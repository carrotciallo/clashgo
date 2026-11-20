package main

import (
	"github.com/carrotciallo/clashgo/internal/config"
	"os"
	"path/filepath"
)

// enforceLogPerms locks the logs directory down to the owner: the core
// and event logs carry connection history and must not be world
// readable. Runs at startup; the writers also create files 0600.
func enforceLogPerms(dir string) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	_ = os.Chmod(dir, 0o700)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Type().IsRegular() {
			_ = os.Chmod(filepath.Join(dir, e.Name()), 0o600)
		}
	}
}

// sealLogs makes the logs directory and its files fully inaccessible
// (mode 0000). Files go first: once the directory is sealed its
// contents can no longer be chmodded. Writers reopen through a brief
// unseal, so logging continues while nothing can read.
func sealLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err == nil {
		for _, e := range entries {
			if e.Type().IsRegular() {
				_ = os.Chmod(filepath.Join(dir, e.Name()), 0o000)
			}
		}
	}
	_ = os.Chmod(dir, 0o000)
}

// applyLogLock enforces the resting permission state for the setting:
// on = owner-only (0700/0600), off = sealed (0000).
func applyLogLock(dir string, on bool) {
	if on {
		enforceLogPerms(dir)
	} else {
		sealLogs(dir)
	}
}

// setLogLock persists the log-lock setting and applies it: the writers
// learn the new mode first, then the resting permissions are enforced.
func (a *App) setLogLock(on bool) error {
	if err := a.changeSettings(func(s *config.Settings) { s.LogLock = on }); err != nil {
		return err
	}
	if a.events != nil {
		a.events.setSealed(!on)
	}
	a.sup.LogSealed = !on
	applyLogLock(a.store.LogsDir(), on)
	a.emitEvent("log_lock_changed", map[string]any{"on": on})
	return nil
}
