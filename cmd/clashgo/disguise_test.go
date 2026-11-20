package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDisguiseCoreLink(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "mihomo")
	if err := os.WriteFile(bin, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	runtime := filepath.Join(dir, "runtime")
	got := disguiseCoreLink(runtime, bin)
	if filepath.Base(got) != "cored" {
		t.Fatalf("link = %s, want basename cored", got)
	}
	target, err := os.Readlink(got)
	if err != nil || target != bin {
		t.Fatalf("readlink = %q, %v; want %q", target, err, bin)
	}
	if again := disguiseCoreLink(runtime, bin); again != got {
		t.Fatalf("second call = %s, want %s (idempotent)", again, got)
	}
}
