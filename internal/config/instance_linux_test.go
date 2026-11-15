package config

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestInstanceLockProcess(t *testing.T) {
	if dir := os.Getenv("CLASHGO_LOCK_TEST_CHILD"); dir != "" {
		lock, err := AcquireInstance(dir)
		if errors.Is(err, ErrInstanceRunning) {
			os.Exit(23)
		}
		if err != nil {
			os.Exit(24)
		}
		_ = lock // Deliberately exit without Close to test kernel release.
		os.Exit(0)
	}
	dir := t.TempDir()
	lock, err := AcquireInstance(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Close()
	child := func(want int) {
		t.Helper()
		cmd := exec.Command(os.Args[0], "-test.run=^TestInstanceLockProcess$")
		cmd.Env = append(os.Environ(), "CLASHGO_LOCK_TEST_CHILD="+dir)
		err := cmd.Run()
		code := 0
		if err != nil {
			var exit *exec.ExitError
			if !errors.As(err, &exit) {
				t.Fatal(err)
			}
			code = exit.ExitCode()
		}
		if code != want {
			t.Fatalf("child exit=%d want=%d", code, want)
		}
	}
	child(23)
	other, err := AcquireInstance(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	_ = other.Close()
	if err := lock.Close(); err != nil {
		t.Fatal(err)
	}
	child(0)
	again, err := AcquireInstance(dir)
	if err != nil {
		t.Fatal("process exit did not release lock", err)
	}
	_ = again.Close()
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != ".instance.lock" {
		t.Fatal("lock acquisition created application state")
	}
}

func TestInstanceLockRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "untouched")
	if err := os.WriteFile(target, []byte("original"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(dir, ".instance.lock")); err != nil {
		t.Fatal(err)
	}
	if lock, err := AcquireInstance(dir); err == nil {
		_ = lock.Close()
		t.Fatal("followed lock symlink")
	}
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "original" {
		t.Fatal("symlink target modified")
	}
}
