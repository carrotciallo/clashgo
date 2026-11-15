package main

import (
	"context"
	"net"
	"strconv"
	"time"

	"github.com/carrotciallo/clashgo/internal/core"
)

// captureSample is sampled by the serialized worker, including while hidden.
// It verifies the selected local service path, not remote node reachability.
type captureSample struct {
	Active bool
	TUN    bool
	PID    int
}

func proxyServiceReady(ctx context.Context, client *core.Client, mixedPort int, healthy, tunWanted bool) bool {
	if !healthy {
		return false
	}
	runtime, err := client.CaptureConfig(ctx)
	if err != nil || runtime.MixedPort != mixedPort {
		return false
	}
	if tunWanted && core.CheckTUNDevice(runtime) != nil {
		return false
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(mixedPort)))
	if err != nil {
		return false
	}
	conn.Close()
	return true
}

// waitTUNReady is shared by startup, activation and rollback before they claim
// successful capture or neutralize the existing system proxy.
func (a *App) waitTUNReady(client *core.Client, enabled bool) error {
	if !enabled {
		return nil
	}
	ctx, cancel := context.WithTimeout(a.appContext(), 3*time.Second)
	defer cancel()
	return client.WaitTUNReady(ctx)
}

func (a *App) refreshCaptureStatus() {
	settings := a.settings()
	sample := captureSample{TUN: settings.TUNEnabled, PID: a.sup.PID()}
	if a.coreHealthy() && a.sup.Running() {
		ctx, cancel := context.WithTimeout(a.appContext(), 1500*time.Millisecond)
		sample.Active = proxyServiceReady(ctx, a.client(), settings.MixedPort, true, settings.TUNEnabled)
		cancel()
	}
	a.mu.Lock()
	a.capture = sample
	a.mu.Unlock()
	a.refreshTray()
}
