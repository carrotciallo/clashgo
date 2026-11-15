package subs

import (
	"sync"
	"time"
)

const RefreshInterval = 24 * time.Hour

// Schedule coalesces manual/automatic work. Retry state is session-local;
// UpdatedAt persists successful refresh cadence across application restarts.
type Schedule struct {
	mu      sync.Mutex
	entries map[string]refreshState
}
type refreshState struct {
	busy     bool
	failures int
	retry    time.Time
	url      string
}

func (s *Schedule) Claim(sub Subscription, now time.Time, manual bool) bool {
	if sub.Local || sub.URL == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.entries == nil {
		s.entries = map[string]refreshState{}
	}
	e := s.entries[sub.Name]
	if e.busy {
		return false
	}
	if e.url != sub.URL {
		e = refreshState{url: sub.URL}
	}
	if !manual {
		if !e.retry.IsZero() {
			if now.Before(e.retry) {
				return false
			}
		} else if !sub.UpdatedAt.IsZero() && now.Before(sub.UpdatedAt.Add(RefreshInterval)) {
			return false
		}
	}
	e.busy = true
	s.entries[sub.Name] = e
	return true
}
func (s *Schedule) Finish(name string, now time.Time, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.entries[name]
	if !ok {
		return
	}
	e.busy = false
	if err == nil {
		e.failures = 0
		e.retry = time.Time{}
	} else {
		if e.failures < 7 {
			e.failures++
		}
		delay := time.Minute * time.Duration(1<<uint(e.failures-1))
		if delay > time.Hour {
			delay = time.Hour
		}
		e.retry = now.Add(delay)
	}
	s.entries[name] = e
}
func (s *Schedule) Forget(name string) { s.mu.Lock(); defer s.mu.Unlock(); delete(s.entries, name) }
