package clientgrpc_test

import (
	"testing"

	clientgrpc "github.com/omcrgnt/client-grpc"
	"github.com/prometheus/client_golang/prometheus"
)

func TestGRPCMetrics_RegisterMetrics(t *testing.T) {
	reg := prometheus.NewRegistry()
	m := &clientgrpc.GRPCMetrics{}
	if err := m.RegisterMetrics(reg); err != nil {
		t.Fatal(err)
	}
	if m.UnaryClientInterceptor() == nil {
		t.Fatal("expected unary interceptor")
	}
	if m.StreamClientInterceptor() == nil {
		t.Fatal("expected stream interceptor")
	}
	// Collectors may expose zero series until the first RPC; registration must not fail.
	if _, err := reg.Gather(); err != nil {
		t.Fatal(err)
	}
}
