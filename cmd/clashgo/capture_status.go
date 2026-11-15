package main

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/carrotciallo/clashgo/internal/core"
)

// captureSample is sampled by the serialized worker, including while hidden.
// It describes local proxy availability, not global capture or remote reachability.
type captureSample struct {
	Active bool
	PID    int
}

func proxyServiceReady(ctx context.Context, client *core.Client, mixedPort int, healthy bool) bool {
	if !healthy {
		return false
	}
	runtime, err := client.CaptureConfig(ctx)
	if err != nil || runtime.MixedPort != mixedPort {
		return false
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

func (a *App) refreshCaptureStatus() {
	settings := a.settings()
	sample := captureSample{PID: a.sup.PID()}
	if a.coreHealthy() && a.sup.Running() {
		ctx, cancel := context.WithTimeout(a.appContext(), 1500*time.Millisecond)
		sample.Active = proxyServiceReady(ctx, a.client(), settings.MixedPort, true)
		cancel()
	}
	a.mu.Lock()
	a.capture = sample
	a.mu.Unlock()
	a.refreshTray()
}
