package clientgrpc

import (
	"context"
	"fmt"

	"google.golang.org/grpc/connectivity"
)

// Ready waits until the connection is Ready (or ctx ends).
func (c *Client) Ready(ctx context.Context) error {
	if c.conn == nil {
		return fmt.Errorf("clientgrpc: client not started")
	}
	c.conn.Connect()
	for {
		state := c.conn.GetState()
		if state == connectivity.Ready {
			return nil
		}
		if state == connectivity.Shutdown {
			return fmt.Errorf("clientgrpc: connection shutdown")
		}
		if !c.conn.WaitForStateChange(ctx, state) {
			return fmt.Errorf("clientgrpc: wait ready: %w", ctx.Err())
		}
	}
}

// ProbeReady reports outbound gRPC readiness (ops duck typing; no ops import).
func (c *Client) ProbeReady(ctx context.Context) error {
	return c.Ready(ctx)
}
