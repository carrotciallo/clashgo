package main

import (
	"context"
	"net"
	"time"
)

// captureSample is sampled by the serialized worker, including while hidden.
// It describes local traffic capture, not reachability of every remote node.
type captureSample struct {
	Active bool
	TUN    bool
	PID    int
}

func captureEffective(healthy, tunWanted, tunReady, proxyOwned bool) bool {
	if !healthy {
		return false
	}
	if tunWanted {
		return tunReady
	}
	return proxyOwned
}

func (a *App) refreshCaptureStatus() {
	settings := a.settings()
	sample := captureSample{TUN: settings.TUNEnabled, PID: a.sup.PID()}
	if a.coreHealthy() && a.sup.Running() {
		ctx, cancel := context.WithTimeout(a.appContext(), 1500*time.Millisecond)
		runtime, err := a.client().CaptureConfig(ctx)
		cancel()
		if err == nil && runtime.MixedPort == settings.MixedPort {
			tunReady, owned := false, false
			if settings.TUNEnabled && runtime.TUN.Enable {
				device, err := net.InterfaceByName(runtime.TUN.Device)
				tunReady = err == nil && device.Flags&net.FlagUp != 0
			} else if !settings.TUNEnabled {
				owned = a.proxyManager().Armed()
			}
			sample.Active = captureEffective(true, settings.TUNEnabled, tunReady, owned)
		}
	}
	a.mu.Lock()
	a.capture = sample
	a.mu.Unlock()
	a.refreshTray()
}
