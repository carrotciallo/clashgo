package core

import (
	"context"
	"sync"
	"testing"
)

func TestTasksCancelWaitAndReject(t *testing.T) {
	tasks := NewTasks(context.Background())
	done := make(chan struct{})
	if !tasks.Go(func(ctx context.Context) { <-ctx.Done(); close(done) }) {
		t.Fatal("not admitted")
	}
	tasks.Wait()
	select {
	case <-done:
	default:
		t.Fatal("Wait returned before worker")
	}
	if tasks.Go(func(context.Context) { t.Error("ran after cancellation") }) {
		t.Fatal("admitted after cancellation")
	}
	tasks.Wait()
}

func TestTasksConcurrentAdmissionAndShutdown(t *testing.T) {
	for range 30 {
		tasks := NewTasks(context.Background())
		var submit sync.WaitGroup
		for range 20 {
			submit.Add(1)
			go func() { defer submit.Done(); tasks.Go(func(ctx context.Context) { <-ctx.Done() }) }()
		}
		tasks.Wait()
		submit.Wait()
		tasks.Wait()
	}
}
