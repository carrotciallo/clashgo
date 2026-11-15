package main

import (
	"context"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
)

func TestSettingsSnapshotAndConcurrentChanges(t *testing.T) {
	a := &App{store: &config.Store{ConfigDir: t.TempDir()}, set: config.Settings{Services: []string{"a"}}}
	snapshot := a.settings()
	snapshot.Services[0] = "changed"
	if a.settings().Services[0] != "a" {
		t.Fatal("snapshot aliases settings")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 15 {
				if err := a.changeSettings(func(s *config.Settings) { s.MixedPort++ }); err != nil {
					t.Error(err)
				}
				_ = a.settings()
				_ = a.enabledServices()
			}
		}()
	}
	wg.Wait()
	if a.settings().MixedPort != 120 {
		t.Fatal("lost updates")
	}
	old := a.settings()
	a.store.ConfigDir = filepath.Join(t.TempDir(), "missing")
	if err := a.changeSettings(func(s *config.Settings) { s.MixedPort = 1 }); err == nil {
		t.Fatal("expected write failure")
	}
	if a.settings().MixedPort != old.MixedPort {
		t.Fatal("published failed write")
	}
}

func TestOperationsSerializeAndShutdownRejectsQueuedWork(t *testing.T) {
	a := &App{work: core.NewTasks(context.Background())}
	entered, release := make(chan struct{}), make(chan struct{})
	var active, ran atomic.Int32
	var wg sync.WaitGroup
	a.operate(func() { close(entered); <-release })
	<-entered
	for range 16 {
		wg.Add(1)
		a.operate(func() {
			defer wg.Done()
			if active.Add(1) != 1 {
				t.Error("operations overlap")
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			ran.Add(1)
		})
	}
	close(release)
	wg.Wait()
	if ran.Load() != 16 {
		t.Fatal("lost operation")
	}
	a.work.Wait()
	a.operate(func() { t.Error("admitted after shutdown") })
}

func TestQueuedOperationsCancelledBeforeRunning(t *testing.T) {
	a := &App{work: core.NewTasks(context.Background())}
	entered, release := make(chan struct{}), make(chan struct{})
	a.operate(func() { close(entered); <-release })
	<-entered
	a.operate(func() { t.Error("cancelled operation ran") })
	a.work.Cancel()
	close(release)
	a.work.Wait()
}
