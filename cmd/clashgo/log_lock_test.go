package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestEnforceLogPerms(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "core.log")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	enforceLogPerms(dir)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("dir mode = %o, want 700", di.Mode().Perm())
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("file mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestApplyLogLockSealAndRestore(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "events.jsonl")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	applyLogLock(dir, false)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o000 {
		t.Errorf("sealed dir mode = %o, want 000", di.Mode().Perm())
	}
	applyLogLock(dir, true)
	di, err = os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o700 {
		t.Errorf("restored dir mode = %o, want 700", di.Mode().Perm())
	}
	fi, err := os.Stat(f)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o600 {
		t.Errorf("restored file mode = %o, want 600", fi.Mode().Perm())
	}
}

func TestEventLogSealedEmit(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	l := newEventLog(filepath.Join(dir, "events.jsonl"))
	l.emit("before_seal", nil)
	applyLogLock(dir, false)
	l.setSealed(true)
	l.emit("while_sealed", nil)
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o000 {
		t.Fatalf("dir mode after sealed emit = %o, want 000", di.Mode().Perm())
	}
	applyLogLock(dir, true)
	kinds := map[string]bool{}
	for _, rec := range readEvents(dir) {
		if k, _ := rec["kind"].(string); k != "" {
			kinds[k] = true
		}
	}
	if !kinds["before_seal"] || !kinds["while_sealed"] {
		t.Errorf("kinds = %v, want both events recorded", kinds)
	}
}
