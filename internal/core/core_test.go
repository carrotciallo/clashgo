package core

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestSupervisorLifecycle(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "child")
	if err := os.WriteFile(script, []byte("#!/bin/sh\ntrap '' TERM\nwhile :; do :; done\n"), 0700); err != nil {
		t.Fatal(err)
	}
	s := &Supervisor{BinPath: script, HomeDir: dir, LogPath: filepath.Join(dir, "log"), stopTimeout: 20 * time.Millisecond}
	for i := 0; i < 3; i++ {
		if err := s.Start("unused"); err != nil {
			t.Fatal(err)
		}
		if !s.Running() || s.PID() == 0 {
			t.Fatal("child not tracked")
		}
		if err := s.Start("unused"); err == nil {
			t.Fatal("double start accepted")
		}
		var wg sync.WaitGroup
		for n := 0; n < 8; n++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := 0; j < 100; j++ {
					s.Running()
					s.PID()
				}
			}()
		}
		start := time.Now()
		s.Stop()
		wg.Wait()
		if s.Running() || s.PID() != 0 || time.Since(start) > time.Second {
			t.Fatal("child not reaped promptly")
		}
		s.Stop()
	}
}

func TestSupervisorStartFailureAndExplicitPath(t *testing.T) {
	dir := t.TempDir()
	s := &Supervisor{BinPath: filepath.Join(dir, "missing"), HomeDir: dir, LogPath: filepath.Join(dir, "log")}
	if s.Start("unused") == nil || s.Running() || s.PID() != 0 {
		t.Fatal("failed spawn marked alive")
	}
	if FindCore(s.BinPath) != s.BinPath {
		t.Fatal("explicit invalid path silently replaced")
	}
}
