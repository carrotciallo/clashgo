package main

import (
	"github.com/carrotciallo/clashgo/internal/config"
	"os"
	"path/filepath"
	"testing"
)

func TestBuiltinProfileIndependentOfActive(t *testing.T) {
	dir := t.TempDir()
	store := &config.Store{ConfigDir: dir}
	if err := os.MkdirAll(store.ProfilesDir(), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "master.yaml"), []byte("builtin"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(store.ProfilePath("work"), []byte("active"), 0600); err != nil {
		t.Fatal(err)
	}
	store.SetActiveProfile("work")
	a := &App{store: store, set: config.Settings{ActiveProfile: "work"}}
	got, err := a.profileContent("")
	if err != nil || got != "builtin" {
		t.Fatalf("builtin resolves to %q: %v", got, err)
	}
	if err := a.saveProfileContent("", "changed"); err != nil {
		t.Fatal(err)
	}
	got, err = store.LoadMaster()
	if err != nil || got != "active" {
		t.Fatal("editing builtin overwrote active profile")
	}
}
