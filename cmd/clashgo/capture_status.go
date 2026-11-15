package main

import (
	"context"
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
	return client.CheckCaptureReady(ctx, mixedPort, tunWanted) == nil
}

// waitCaptureReady is shared by startup, activation and rollback before they claim
// successful capture or neutralize the existing system proxy.
func (a *App) waitCaptureReady(client *core.Client, port int, enabled bool) error {
	ctx, cancel := context.WithTimeout(a.appContext(), 3*time.Second)
	defer cancel()
	return client.WaitCaptureReady(ctx, port, enabled)
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
