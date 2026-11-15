package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCommitFilesRejectsBeforePublishing(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "source")
	b := filepath.Join(dir, "runtime")
	_ = os.WriteFile(a, []byte("old"), 0644)
	err := CommitFiles([]FileChange{{a, []byte("new")}, {b, []byte("runtime")}}, func(staged map[string]string) error {
		got, _ := os.ReadFile(a)
		if string(got) != "old" {
			t.Fatal("published before validation")
		}
		for _, p := range staged {
			fi, e := os.Stat(p)
			if e != nil || fi.Mode().Perm() != 0600 {
				t.Fatal("candidate permissions")
			}
		}
		return errors.New("invalid")
	}, func() error { t.Fatal("activated invalid candidate"); return nil }, nil)
	if err == nil {
		t.Fatal("expected failure")
	}
	got, _ := os.ReadFile(a)
	if string(got) != "old" {
		t.Fatal("source changed")
	}
	if _, err := os.Stat(b); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("created runtime")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Fatal("staging leaked")
	}
}
func TestCommitFilesRestoresAllFilesBeforeRollback(t *testing.T) {
	dir := t.TempDir()
	a := filepath.Join(dir, "source")
	b := filepath.Join(dir, "settings")
	newPath := filepath.Join(dir, "new")
	_ = os.WriteFile(a, []byte("old-source"), 0644)
	_ = os.WriteFile(b, []byte("old-settings"), 0600)
	rolled := false
	err := CommitFiles([]FileChange{{a, []byte("new-source")}, {b, []byte("new-settings")}, {newPath, []byte("new")}}, nil, func() error { return errors.New("activation") }, func() error {
		rolled = true
		x, _ := os.ReadFile(a)
		y, _ := os.ReadFile(b)
		if string(x) != "old-source" || string(y) != "old-settings" {
			t.Fatal("rollback before disk restore")
		}
		if _, e := os.Stat(newPath); !errors.Is(e, os.ErrNotExist) {
			t.Fatal("new file survived")
		}
		return errors.New("rollback failed")
	})
	if err == nil || !rolled || !strings.Contains(err.Error(), "rollback failed") {
		t.Fatal("lost failures", err)
	}
	fi, _ := os.Stat(a)
	if fi.Mode().Perm() != 0644 {
		t.Fatal("rollback permissions changed")
	}
}
func TestCommitFilesSuccessAndRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	if err := CommitFiles([]FileChange{{path, []byte("new")}}, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	_ = os.Symlink(path, link)
	if err := CommitFiles([]FileChange{{link, []byte("bad")}}, nil, nil, nil); err == nil {
		t.Fatal("symlink accepted")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "new" {
		t.Fatal("symlink modified target")
	}
}
