package core

import (
	"context"
	"fmt"
	"net"
	"strconv"
	"time"
)

// CheckCaptureReady verifies applied settings plus the actual local listener.
// /version may answer before mihomo has applied its initial configuration.
func (c *Client) CheckCaptureReady(ctx context.Context, port int, tun bool) error {
	runtime, err := c.CaptureConfig(ctx)
	if err != nil {
		return err
	}
	if port <= 0 || runtime.MixedPort != port {
		return fmt.Errorf("mixed listener configuration is not ready")
	}
	if tun {
		if err := CheckTUNDevice(runtime); err != nil {
			return err
		}
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}

func (c *Client) WaitCaptureReady(ctx context.Context, port int, tun bool) error {
	interval := 20 * time.Millisecond
	for {
		err := c.CheckCaptureReady(ctx, port, tun)
		if err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("capture did not become ready: %v: %w", err, ctx.Err())
		case <-time.After(interval):
		}
		interval = min(interval*2, 250*time.Millisecond)
	}
}
