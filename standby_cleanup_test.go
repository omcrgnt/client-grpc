package clientgrpc_test

import (
	"context"
	"errors"
	"net"
	"testing"

	clientgrpc "github.com/omcrgnt/client-grpc"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
	"github.com/omcrgnt/runner"
)

// standByFailsAfter fails StandBy unconditionally — registered after the
// real client-grpc.Client below so runner.Runner's StandBy-phase rollback
// is exercised against an already-succeeded, real *grpc.ClientConn, not a
// mock.
type standByFailsAfter struct{}

func (*standByFailsAfter) StandBy() (func(context.Context) error, error) {
	return nil, errStandByBoom
}

var errStandByBoom = errors.New("sibling standby: boom")

// TestClient_ClosedWhenSiblingStandByFails is the empirical check the
// review round asked for: does runner.Runner's StandBy-phase rollback
// actually close a real dialed *grpc.ClientConn, not just a fake mock
// cleanup? Client.Conn is nil after the returned cleanup runs (the closure
// sets c.conn = nil) — checking that is a direct, real signal, not an
// inference from mock state.
func TestClient_ClosedWhenSiblingStandByFails(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	host, port := testAddrHostPort(t, lis.Addr().String())

	metrics, _ := testGRPCMetrics(t)

	built, err := (&clientgrpc.Config{
		Label: common.Label{Value: "sibling_fail_test"},
		Host:  common.Host{Value: host},
		Port:  common.Port{Value: port},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := built.(*clientgrpc.Client)
	c.Inject([]any{metrics})

	r := &runner.Runner{}
	// c registered before the failure: its StandBy must succeed (a real
	// dial) and then get rolled back; standByFailsAfter registered after:
	// its own StandBy fails and aborts the phase.
	r.Inject([]any{[]runner.StandBy{c, &standByFailsAfter{}}})

	err = r.Run(context.Background())
	if !errors.Is(err, errStandByBoom) {
		t.Fatalf("Run err = %v, want to wrap %v", err, errStandByBoom)
	}

	if c.Conn() != nil {
		t.Fatal("client-grpc.Client's real *grpc.ClientConn was not closed by runner's StandBy rollback")
	}
}
