package clientgrpc_test

import (
	"context"
	"reflect"
	"testing"
	"time"

	clientgrpc "github.com/omcrgnt/client-grpc"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/health/grpc_health_v1"
)

type tagA struct{}
type tagB struct{}

// TestTagged_distinctTypes pins down the entire point of Tagged: two
// instantiations with different phantom tags must be different Go types,
// so a type-unique DI registry (github.com/omcrgnt/res/unique) can hold
// both without the "entry for type already has TagRegular" collision a
// plain *Client would trip if used twice in the same catalog.
func TestTagged_distinctTypes(t *testing.T) {
	typeA := reflect.TypeOf((*clientgrpc.Tagged[tagA])(nil))
	typeB := reflect.TypeOf((*clientgrpc.Tagged[tagB])(nil))
	if typeA == typeB {
		t.Fatalf("Tagged[tagA] and Tagged[tagB] must be distinct types, got the same: %v", typeA)
	}
}

func TestTagged_BuildConfig_Build_StandBy_integration(t *testing.T) {
	addr, stop := startHealthServer(t)
	t.Cleanup(stop)
	host, port := testAddrHostPort(t, addr)
	metrics, _ := testGRPCMetrics(t)

	var c clientgrpc.Tagged[tagA]
	matz, err := c.BuildConfig()
	if err != nil {
		t.Fatal(err)
	}
	spec, ok := matz.(interface{ Build() (any, error) })
	if !ok {
		t.Fatalf("BuildConfig() result %T has no Build method", matz)
	}

	// ecfg would fill Label/Host/Port by reflection here — set them
	// directly via the same struct-literal shape it targets.
	specVal := reflect.ValueOf(spec).Elem()
	specVal.FieldByName("Label").Set(reflect.ValueOf(common.Label{Value: "test_tagged"}))
	specVal.FieldByName("Host").Set(reflect.ValueOf(common.Host{Value: host}))
	specVal.FieldByName("Port").Set(reflect.ValueOf(common.Port{Value: port}))

	built, err := spec.Build()
	if err != nil {
		t.Fatal(err)
	}
	tagged, ok := built.(*clientgrpc.Tagged[tagA])
	if !ok {
		t.Fatalf("Build() returned %T, want *Tagged[tagA]", built)
	}
	if tagged.Label() != "test_tagged" {
		t.Fatalf("label = %q", tagged.Label())
	}
	if tagged.Target() != addr {
		t.Fatalf("target = %q, want %q", tagged.Target(), addr)
	}

	deps := tagged.Deps()
	if got, want := reflect.TypeOf(deps[0]), reflect.TypeOf((*clientgrpc.GRPCMetrics)(nil)); got != want {
		t.Fatalf("Deps()[0] type = %v, want %v", got, want)
	}
	tagged.Inject([]any{metrics})

	cleanup, err := tagged.StandBy()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	hc := grpc_health_v1.NewHealthClient(tagged.Conn())
	resp, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if resp.GetStatus() != grpc_health_v1.HealthCheckResponse_SERVING {
		t.Fatalf("status = %v", resp.GetStatus())
	}
}

// TestTagged_NewTagged_interceptorCarriesThrough pins down the extraUnary
// path added for npc-dialogue's shopstore client (gctxgrpc propagation to a
// compat-filtering store) — NewTagged's option must survive
// BuildConfig -> taggedConfig.Build -> the real StandBy-dialed Client,
// same as Client's own New/WithUnaryClientInterceptors already does.
func TestTagged_NewTagged_interceptorCarriesThrough(t *testing.T) {
	addr, stop := startHealthServer(t)
	t.Cleanup(stop)
	host, port := testAddrHostPort(t, addr)
	metrics, _ := testGRPCMetrics(t)

	var called bool
	interceptor := func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		called = true
		return invoker(ctx, method, req, reply, cc, opts...)
	}

	c := clientgrpc.NewTagged[tagB](clientgrpc.WithTaggedUnaryClientInterceptors[tagB](interceptor))
	matz, err := c.BuildConfig()
	if err != nil {
		t.Fatal(err)
	}
	spec := matz.(interface{ Build() (any, error) })
	specVal := reflect.ValueOf(spec).Elem()
	specVal.FieldByName("Label").Set(reflect.ValueOf(common.Label{Value: "test_tagged_opt"}))
	specVal.FieldByName("Host").Set(reflect.ValueOf(common.Host{Value: host}))
	specVal.FieldByName("Port").Set(reflect.ValueOf(common.Port{Value: port}))

	built, err := spec.Build()
	if err != nil {
		t.Fatal(err)
	}
	tagged := built.(*clientgrpc.Tagged[tagB])
	tagged.Inject([]any{metrics})

	cleanup, err := tagged.StandBy()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cleanup(context.Background()) })

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	hc := grpc_health_v1.NewHealthClient(tagged.Conn())
	if _, err := hc.Check(ctx, &grpc_health_v1.HealthCheckRequest{}); err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("expected NewTagged's interceptor to run during the real call")
	}
}

func TestTagged_nilReceiver_LabelTarget(t *testing.T) {
	var c *clientgrpc.Tagged[tagA]
	if c.Label() != "" {
		t.Fatalf("Label() on nil = %q, want empty", c.Label())
	}
	if c.Target() != "" {
		t.Fatalf("Target() on nil = %q, want empty", c.Target())
	}
}
