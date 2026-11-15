package main

import (
	"context"
	"slices"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/sysproxy"
	"github.com/egoist/mygo"
)

func (a *App) settings() config.Settings {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	s := a.set
	s.Services = slices.Clone(s.Services)
	s.CollapsedSubs = slices.Clone(s.CollapsedSubs)
	return s
}

// Settings are published only after persistence succeeds. Mutations operate on
// a copy under the same lock so unrelated preferences cannot lose updates.
func (a *App) changeSettings(change func(*config.Settings)) error {
	a.stateMu.Lock()
	defer a.stateMu.Unlock()
	next := a.set
	next.Services = slices.Clone(next.Services)
	next.CollapsedSubs = slices.Clone(next.CollapsedSubs)
	change(&next)
	if err := a.store.SaveSettings(next); err != nil {
		return err
	}
	a.set = next
	return nil
}
func (a *App) client() *core.Client { a.stateMu.RLock(); defer a.stateMu.RUnlock(); return a.cli }
func (a *App) proxyManager() *sysproxy.Manager {
	a.stateMu.RLock()
	defer a.stateMu.RUnlock()
	return a.px
}
func (a *App) corePath() string { a.stateMu.RLock(); defer a.stateMu.RUnlock(); return a.coreBin }
func (a *App) appContext() context.Context {
	if a.work == nil {
		return context.Background()
	}
	return a.work.Context()
}

// operate serializes state-changing actions, not rendering. The callback must
// not wait for another operate callback; nested work uses synchronous helpers.
func (a *App) operate(fn func()) {
	a.launch(func() {
		a.opMu.Lock()
		defer a.opMu.Unlock()
		if a.appContext().Err() != nil {
			return
		}
		fn()
	})
}
func (a *App) launch(fn func()) {
	if a.work == nil {
		return
	}
	a.work.Go(func(ctx context.Context) {
		if ctx.Err() == nil {
			fn()
		}
	})
}
func (a *App) onUI(fn func()) {
	if a.quitting.Load() {
		return
	}
	mygo.RunOnMain(func() {
		if !a.quitting.Load() {
			fn()
		}
	})
}
