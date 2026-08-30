package clientgrpc

import (
	"context"
	"fmt"

	"github.com/omcrgnt/app"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
	"github.com/omcrgnt/runner"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// Config is the gRPC client spec (Label, Host, Port); ecfg fills before Build.
// Target address is host:port (plaintext; TLS is out of scope for v1).
type Config struct {
	Label common.Label
	Host  common.Host
	Port  common.Port
}

func (cfg *Config) Build() (any, error) {
	host := cfg.Host.GetValue()
	port := cfg.Port.GetValue()
	if host == "" {
		return nil, fmt.Errorf("clientgrpc: host is required")
	}
	if port == 0 {
		return nil, fmt.Errorf("clientgrpc: port is required")
	}
	return &Client{
		label:  cfg.Label.GetValue(),
		target: fmt.Sprintf("%s:%d", host, port),
	}, nil
}

// Client is an outbound gRPC connection resource.
// Catalog field: *Client (Configurable); dial happens in StandBy after Inject resolves GRPCMetrics.
type Client struct {
	conn    *grpc.ClientConn
	metrics *GRPCMetrics
	label   string
	target  string
}

var _ app.Configurable = (*Client)(nil)
var _ runner.StandBy = (*Client)(nil)

// BuildConfig returns the config spec for materialize.
func (*Client) BuildConfig() (app.Materializer, error) {
	return &Config{}, nil
}

// Deps declares the shared GRPCMetrics singleton.
func (*Client) Deps() []any {
	return []any{(*GRPCMetrics)(nil)}
}

// Inject receives GRPCMetrics from SDI.
func (c *Client) Inject(args []any) {
	for _, arg := range args {
		if m, ok := arg.(*GRPCMetrics); ok {
			c.metrics = m
		}
	}
}

// Label returns the configured resource label.
func (c *Client) Label() string {
	if c == nil {
		return ""
	}
	return c.label
}

// Target returns host:port.
func (c *Client) Target() string {
	if c == nil {
		return ""
	}
	return c.target
}

// Conn returns the underlying gRPC connection (nil before StandBy).
func (c *Client) Conn() *grpc.ClientConn {
	if c == nil {
		return nil
	}
	return c.conn
}

// StandBy dials the target with otel + prometheus client instrumentation.
// grpc.NewClient performs no I/O — it builds a ClientConn that connects
// lazily on first RPC — so this runs in runner.Runner's sequential StandBy
// phase rather than as a runner.Starter.
//
// On success it returns a cleanup that closes conn — runner.Runner retains
// this closure and calls it during Stop (or immediately, to unwind, if a
// later sibling's own StandBy or Start fails). There is no started()-guard
// here: Runner only ever calls a cleanup it received from a StandBy call
// that itself succeeded, so a nil c.conn can't happen when this runs.
func (c *Client) StandBy() (func(context.Context) error, error) {
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithMetricAttributes(attribute.String("client", c.label)),
		)),
		grpc.WithChainUnaryInterceptor(c.metrics.UnaryClientInterceptor()),
		grpc.WithChainStreamInterceptor(c.metrics.StreamClientInterceptor()),
	}

	conn, err := grpc.NewClient(c.target, opts...)
	if err != nil {
		return nil, fmt.Errorf("clientgrpc: dial %s: %w", c.target, err)
	}
	c.conn = conn
	return func(context.Context) error {
		err := conn.Close()
		c.conn = nil
		return err
	}, nil
}

// newTestClient constructs a Client around an existing connection (tests only).
func newTestClient(conn *grpc.ClientConn, label string) *Client {
	return &Client{conn: conn, label: label, target: conn.Target()}
}
