package clientgrpc_test

import (
	"context"
	"net"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	clientgrpc "github.com/omcrgnt/client-grpc"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	"google.golang.org/grpc/health/grpc_health_v1"
)

// testAddrHostPort splits a net.Listener address into the (Host, Port)
// shape clientgrpc.Config expects — the one place in this package's tests
// that does this, shared by every test needing a real listener address.
func testAddrHostPort(t *testing.T, addr string) (host string, port uint32) {
	t.Helper()
	h, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("bad addr %q: %v", addr, err)
	}
	p, err := strconv.ParseUint(portStr, 10, 32)
	if err != nil {
		t.Fatalf("bad port in addr %q: %v", addr, err)
	}
	return h, uint32(p)
}

func testGRPCMetrics(t *testing.T) (*clientgrpc.GRPCMetrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m := &clientgrpc.GRPCMetrics{}
	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	return m, reg
}

func startHealthServer(t *testing.T) (addr string, stop func()) {
	t.Helper()
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := grpc.NewServer()
	hs := health.NewServer()
	hs.SetServingStatus("", grpc_health_v1.HealthCheckResponse_SERVING)
	grpc_health_v1.RegisterHealthServer(s, hs)
	go func() { _ = s.Serve(lis) }()
	return lis.Addr().String(), func() {
		s.GracefulStop()
		_ = lis.Close()
	}
}

func TestConfig_Build_StandBy_integration(t *testing.T) {
	spanExporter := tracetest.NewInMemoryExporter()
	tp := trace.NewTracerProvider(trace.WithSyncer(spanExporter))
	otel.SetTracerProvider(tp)

	addr, stop := startHealthServer(t)
	t.Cleanup(stop)

	host, port := testAddrHostPort(t, addr)

	metrics, reg := testGRPCMetrics(t)

	cfg := clientgrpc.Config{
		Label: common.Label{Value: "test_client"},
		Host:  common.Host{Value: host},
		Port:  common.Port{Value: port},
	}
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	c := built.(*clientgrpc.Client)
	if c.Target() != addr {
		t.Fatalf("target = %q, want %q", c.Target(), addr)
	}
	if c.Label() != "test_client" {
		t.Fatalf("label = %q", c.Label())
	}

	deps := c.Deps()
	if got, want := reflect.TypeOf(deps[0]), reflect.TypeOf((*clientgrpc.GRPCMetrics)(nil)); got != want {
		t.Fatalf("Deps()[0] type = %v, want %v", got, want)
	}

	c.Inject([]any{metrics})
	if err := c.StandBy(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	if err := c.Ready(ctx); err != nil {
		t.Fatal(err)
	}

	hc := grpc_health_v1.NewHealthClient(c.Conn())
	resp, err := hc.Check(t.Context(), &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v", resp.GetStatus())
	}

	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	metricsStr := formatMetricFamilies(mfs)
	if !strings.Contains(metricsStr, "grpc_client_handled_total") {
		t.Errorf("metrics: missing grpc_client_handled_total\n%s", metricsStr)
	}

	if err := tp.ForceFlush(t.Context()); err != nil {
		t.Fatal(err)
	}
	spans := spanExporter.GetSpans()
	if len(spans) == 0 {
		t.Fatal("expected otel client spans")
	}
}

func TestConfig_Build_requiresHostPort(t *testing.T) {
	if _, err := (&clientgrpc.Config{
		Host: common.Host{Value: ""},
		Port: common.Port{Value: 9084},
	}).Build(); err == nil {
		t.Fatal("expected host error")
	}
	if _, err := (&clientgrpc.Config{
		Host: common.Host{Value: "127.0.0.1"},
		Port: common.Port{Value: 0},
	}).Build(); err == nil {
		t.Fatal("expected port error")
	}
}

func TestProbeReady_notStarted(t *testing.T) {
	c := &clientgrpc.Client{}
	if err := c.ProbeReady(t.Context()); err == nil {
		t.Fatal("expected error")
	}
}

func TestClient_Close_beforeStandBy(t *testing.T) {
	c := &clientgrpc.Client{}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClient_Close_nilReceiver(t *testing.T) {
	var c *clientgrpc.Client
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func TestClient_Close_doubleClose(t *testing.T) {
	// A different state than TestClient_Close_beforeStandBy: StandBy did
	// succeed here, so the first Close has real work to do; the second
	// hits the same nil-conn path but via c.conn being reset by the first
	// Close, not via StandBy never having run.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	metrics, _ := testGRPCMetrics(t)
	cfg := clientgrpc.Config{
		Label: common.Label{Value: "test_client"},
		Host:  common.Host{Value: "127.0.0.1"},
		Port:  common.Port{Value: uint32(lis.Addr().(*net.TCPAddr).Port)},
	}
	built, err := cfg.Build()
	if err != nil {
		t.Fatal(err)
	}
	c := built.(*clientgrpc.Client)
	c.Inject([]any{metrics})
	if err := c.StandBy(); err != nil {
		t.Fatal(err)
	}

	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}

func formatMetricFamilies(mfs []*dto.MetricFamily) string {
	var b strings.Builder
	for _, mf := range mfs {
		b.WriteString(mf.GetName())
		b.WriteByte('\n')
	}
	return b.String()
}
