package clientgrpc_test

import (
	"errors"
	"net"
	"testing"

	"github.com/omcrgnt/app"
	clientgrpc "github.com/omcrgnt/client-grpc"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
	"github.com/omcrgnt/res/unique"
	"github.com/omcrgnt/runner"
)

// fakeAppGate satisfies runner.Runner's gateOpener dependency (Open(), no
// args) — required since Deps() there is a mandatory single dep (see the
// runner.Gate discussion: real deployments always have one via runner's
// own init, only hand-built registries like this one need a stand-in).
type fakeAppGate struct{}

func (*fakeAppGate) Open() {}

// standByFailsAfter fails StandBy unconditionally — registered after the
// real client-grpc.Client below so Bootstrap's cleanup path is exercised
// against an already-succeeded, real *grpc.ClientConn, not a mock.
type standByFailsAfter struct{}

func (*standByFailsAfter) StandBy() error { return errStandByBoom }

var errStandByBoom = errors.New("sibling standby: boom")

type emptyAppResources struct{}

// TestClient_ClosedWhenSiblingStandByFails is the empirical check the
// review round asked for: does app.Bootstrap's cleanup path actually close
// a real dialed *grpc.ClientConn, not just a fake mock Closer? Client.Conn
// is nil after a real Close (Close sets c.conn = nil) — checking that is a
// direct, real signal, not an inference from mock state.
func TestClient_ClosedWhenSiblingStandByFails(t *testing.T) {
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = lis.Close() })

	host, portStr, ok := splitHostPort(lis.Addr().String())
	if !ok {
		t.Fatalf("bad addr %q", lis.Addr().String())
	}

	metrics, _ := testGRPCMetrics(t)

	built, err := (&clientgrpc.Config{
		Label: common.Label{Value: "sibling_fail_test"},
		Host:  common.Host{Value: host},
		Port:  common.Port{Value: portStr},
	}).Build()
	if err != nil {
		t.Fatal(err)
	}
	c := built.(*clientgrpc.Client)

	reg := unique.New()
	reg.MustAddReplaceable(app.DefaultApp())
	reg.MustAddFixed(&runner.Runner{})
	reg.MustAddFixed(&fakeAppGate{})
	reg.MustAddFixed(metrics)
	reg.MustAddFixed(c)                    // registered before the failure: must end up closed
	reg.MustAddFixed(&standByFailsAfter{}) // registered after: fails, triggers cleanup

	_, err = app.Bootstrap(&emptyAppResources{}, app.Pipeline{
		Registry:  reg,
		EnvPrefix: "SIBLINGFAIL",
	})
	if !errors.Is(err, errStandByBoom) {
		t.Fatalf("Bootstrap err = %v, want to wrap %v", err, errStandByBoom)
	}

	if c.Conn() != nil {
		t.Fatal("client-grpc.Client's real *grpc.ClientConn was not closed by app.Bootstrap's cleanup path")
	}
}

func splitHostPort(addr string) (host string, port uint32, ok bool) {
	for i := len(addr) - 1; i >= 0; i-- {
		if addr[i] == ':' {
			host = addr[:i]
			var p uint32
			for _, ch := range addr[i+1:] {
				if ch < '0' || ch > '9' {
					return "", 0, false
				}
				p = p*10 + uint32(ch-'0')
			}
			return host, p, true
		}
	}
	return "", 0, false
}
