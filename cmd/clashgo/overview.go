package main

import "github.com/carrotciallo/clashgo/internal/config"

// Sampled by the worker, never by a frame. In particular Armed may execute
// gsettings and must not turn an animated dashboard into a process launcher.
type overviewFacts struct {
	AppMemory, CoreMemory int64
	RuleCount             int
	CaptureOwned          bool
}

func (a *App) refreshOverviewFacts() {
	var facts overviewFacts
	facts.AppMemory, facts.CoreMemory = a.memUsage()
	facts.CaptureOwned = a.proxyManager().Armed()
	if master, err := a.store.LoadMaster(); err == nil {
		facts.RuleCount = len(config.ExtractRules(master))
	}
	a.mu.Lock()
	a.overview = facts
	a.mu.Unlock()
}
