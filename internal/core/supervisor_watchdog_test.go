package core

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

// The watchdog contract: an exit nobody asked for fires
// OnUnexpectedExit; an exit through Stop does not.
func TestSupervisorUnexpectedExitCallback(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fakecore.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	fired := make(chan struct{}, 2)
	sup := &Supervisor{
		BinPath:          script,
		HomeDir:          dir,
		LogPath:          filepath.Join(dir, "core.log"),
		OnUnexpectedExit: func() { fired <- struct{}{} },
	}
	if err := sup.Start("ignored"); err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(sup.PID(), syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	select {
	case <-fired:
	case <-time.After(3 * time.Second):
		t.Fatal("unexpected exit did not fire the callback")
	}

	if err := sup.Start("ignored"); err != nil {
		t.Fatal(err)
	}
	sup.Stop()
	select {
	case <-fired:
		t.Fatal("Stop() exit must not fire the callback")
	case <-time.After(300 * time.Millisecond):
	}
}
