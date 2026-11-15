package core

import (
	"context"
	"sync"
)

// Tasks admits work until cancellation. Admission and shutdown share one mutex,
// so Wait cannot race with an Add after the counter reaches zero.
type Tasks struct {
	mu     sync.Mutex
	ctx    context.Context
	cancel context.CancelFunc
	closed bool
	wg     sync.WaitGroup
}

func NewTasks(parent context.Context) *Tasks {
	ctx, cancel := context.WithCancel(parent)
	return &Tasks{ctx: ctx, cancel: cancel}
}
func (t *Tasks) Context() context.Context { return t.ctx }
func (t *Tasks) Go(fn func(context.Context)) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.wg.Add(1)
	go func() { defer t.wg.Done(); fn(t.ctx) }()
	return true
}
func (t *Tasks) Cancel() {
	t.mu.Lock()
	t.closed = true
	t.cancel()
	t.mu.Unlock()
}

// Wait must be called outside managed tasks. Cancel first to close admission.
func (t *Tasks) Wait() { t.Cancel(); t.wg.Wait() }
