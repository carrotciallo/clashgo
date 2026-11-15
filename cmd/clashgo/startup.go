package main

import (
	"log"
	"time"
)

// Fixed stage names and elapsed duration only: never log settings, URLs,
// subscription payloads or controller credentials for performance diagnosis.
func (a *App) startupMark(stage string) {
	if a.startupStarted.IsZero() || a.startupFinished.Load() {
		return
	}
	log.Printf("clashgo: startup stage=%s elapsed_ms=%d", stage, time.Since(a.startupStarted).Milliseconds())
}
