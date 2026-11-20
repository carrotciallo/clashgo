package core

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLogFileSealed(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "core.log")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, 0o000); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(dir, 0o000); err != nil {
		t.Fatal(err)
	}
	f, err := OpenLogFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, true)
	if err != nil {
		t.Fatalf("sealed open: %v", err)
	}
	if _, err := f.WriteString("new\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	di, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != 0o000 {
		t.Errorf("dir mode = %o, want 000 (resealed)", di.Mode().Perm())
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Mode().Perm() != 0o000 {
		t.Errorf("file mode = %o, want 000 (resealed)", fi.Mode().Perm())
	}
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "old\nnew\n" {
		t.Errorf("content = %q, want both lines", data)
	}
}
