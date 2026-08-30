# client-grpc

Outbound gRPC client for omcrgnt apps: ecfg catalog resource, plaintext dial to
`host:port`, OpenTelemetry traces via `otelgrpc`, Prometheus client metrics via
`go-grpc-middleware/providers/prometheus`.

Pair with [`srv-grpc`](https://github.com/omcrgnt/srv-grpc) on the server side.

## Catalog

```go
import clientgrpc "github.com/omcrgnt/client-grpc"

type catalog struct {
	AssetGRPC *clientgrpc.Client `ecfg:"ASSET_GRPC"`
}
```

`Client` depends on the shared `*GRPCMetrics` singleton (registered in `unique.Global`
on import, like `srv-grpc`). Ops metrics actuator calls `RegisterMetrics` before
`StandBy`.

Typed stubs are created by the app from `Conn()`:

```go
mapv1.NewMapServiceClient(c.AssetGRPC.Conn())
```

## Environment

Prefix comes from the app (`EnvPrefix`); fields below are relative to the slot
(e.g. `ASSET_GRPC`).

| Variable | Description |
|----------|-------------|
| `ASSET_GRPC_LABEL` | Resource label (otel attribute `client`) |
| `ASSET_GRPC_HOST` | Target host / IP (`common.v1.Host`) |
| `ASSET_GRPC_PORT` | Target port 1–65535 (`common.v1.Port`) |

Example:

```bash
BEMVPGAME_ASSET_GRPC_LABEL=asset
BEMVPGAME_ASSET_GRPC_HOST=127.0.0.1
BEMVPGAME_ASSET_GRPC_PORT=9084
```

## Lifecycle

| Hook | Role |
|------|------|
| `Build` / `BuildConfig` | Materialize from ecfg (`Label`, `Host`, `Port`) |
| `Deps` / `Inject` | Wire `*GRPCMetrics` |
| `StandBy` | `grpc.NewClient` with otel stats handler + prometheus interceptors |
| `CleanUp` | Undo `StandBy` (`app.StandByCleaner`) if a sibling resource's own `StandBy` fails and aborts `app.Bootstrap` |
| `Close` | Close `ClientConn` on normal shutdown (`runner.Closer`) |
| `Ready` / `ProbeReady` | Wait until connectivity `Ready` |

`StandBy`, not `runner.Starter` — see `Client.StandBy`'s doc comment for why.
`CleanUp` and `Close` both end up closing the same `ClientConn` but are called from
two independent paths (`app.Bootstrap`'s cleanup vs. `runner.Runner.Stop`) that
never fire for the same instance in the same run — see `Client.CleanUp`'s doc
comment.

v1 transport is **insecure/plaintext** (internal mesh). TLS can be added later.

## Telemetry

On `StandBy`:

- `otelgrpc.NewClientHandler` — client spans/metrics to process `TracerProvider`
- prometheus `ClientMetrics` (with handling-time histogram) — `grpc_client_*` series

Import the package (or `srv-grpc`) so metrics singletons exist; scrape via ops `/metrics`.

## Example

See [`example/app`](example/app).
