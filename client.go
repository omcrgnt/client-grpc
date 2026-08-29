package clientgrpc

import (
	"context"
	"fmt"

	"github.com/omcrgnt/app"
	common "github.com/omcrgnt/proto/gen/go/common/v1"
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
var _ app.StandBy = (*Client)(nil)

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
// lazily on first RPC — so this runs in app.Bootstrap's sequential StandBy
// phase rather than as a runner.Starter.
func (c *Client) StandBy() error {
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
		return fmt.Errorf("clientgrpc: dial %s: %w", c.target, err)
	}
	c.conn = conn
	return nil
}

// Close closes the gRPC connection. A no-op if StandBy never ran (e.g.
// StandBy itself failed, or this Client was never wired through
// app.Bootstrap) — c.conn is nil in that case, and grpc.ClientConn.Close
// would otherwise panic on a nil receiver.
func (c *Client) Close(_ context.Context) error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}

// newTestClient constructs a Client around an existing connection (tests only).
func newTestClient(conn *grpc.ClientConn, label string) *Client {
	return &Client{conn: conn, label: label, target: conn.Target()}
}
