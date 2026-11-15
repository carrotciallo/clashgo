package main

import (
	"context"
	"time"
)

// The loop stays alive with the window hidden. TryLock avoids an unbounded
// backlog behind interactive operations; each pool has a single in-flight job.
func (a *App) subscriptionLoop(ctx context.Context) {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for {
		if ctx.Err() != nil {
			return
		}
		if a.opMu.TryLock() {
			a.refreshDueSubscriptions(ctx, time.Now())
			a.opMu.Unlock()
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}
func (a *App) refreshDueSubscriptions(ctx context.Context, now time.Time) {
	for _, sub := range a.reg.Snapshot() {
		if ctx.Err() != nil {
			return
		}
		if !a.refreshSchedule.Claim(sub, now, false) {
			continue
		}
		err := a.fetchSubscription(sub.Name, false)
		a.refreshSchedule.Finish(sub.Name, time.Now(), err)
	}
}
