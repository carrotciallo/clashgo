package subs

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestScheduleCadenceBackoffAndManualCoalescing(t *testing.T) {
	var s Schedule
	now := time.Now()
	sub := Subscription{Name: "x", URL: "https://example.invalid", UpdatedAt: now}
	if s.Claim(sub, now, false) {
		t.Fatal("fresh pool automatically refreshed")
	}
	if !s.Claim(sub, now, true) || s.Claim(sub, now, true) {
		t.Fatal("manual coalescing")
	}
	s.Finish(sub.Name, now, errors.New("offline"))
	if s.Claim(sub, now.Add(59*time.Second), false) {
		t.Fatal("retry too early")
	}
	if !s.Claim(sub, now.Add(time.Minute), false) {
		t.Fatal("retry suppressed by UpdatedAt")
	}
	s.Finish(sub.Name, now.Add(time.Minute), errors.New("offline"))
	if s.Claim(sub, now.Add(2*time.Minute), false) {
		t.Fatal("backoff not exponential")
	}
	if !s.Claim(sub, now.Add(3*time.Minute), false) {
		t.Fatal("retry not due")
	}
	s.Finish(sub.Name, now.Add(3*time.Minute), nil)
	sub.UpdatedAt = now.Add(3 * time.Minute)
	if s.Claim(sub, now.Add(RefreshInterval), false) {
		t.Fatal("cadence uses wrong baseline")
	}
	if !s.Claim(sub, sub.UpdatedAt.Add(RefreshInterval), false) {
		t.Fatal("due refresh missing")
	}
	s.Forget(sub.Name)
	sub.Local = true
	if s.Claim(sub, now, true) {
		t.Fatal("local fetched")
	}
}
func TestScheduleConcurrentClaimIsSingleFlight(t *testing.T) {
	var s Schedule
	var wins atomic.Int32
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.Claim(Subscription{Name: "x", URL: "https://example.invalid"}, time.Now(), true) {
				wins.Add(1)
			}
		}()
	}
	wg.Wait()
	if wins.Load() != 1 {
		t.Fatal("multiple simultaneous refreshes")
	}
}
