package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/subs"
)

func TestSourceTransactionRejectedCandidatePreservesFilesAndChoice(t *testing.T) {
	dir := t.TempDir()
	st := &config.Store{ConfigDir: filepath.Join(dir, "config"), StateDir: filepath.Join(dir, "state")}
	if err := st.EnsureLayout(); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "reject-core")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 42\n"), 0700); err != nil {
		t.Fatal(err)
	}
	reg, err := subs.Load(st.SubsPath())
	if err != nil {
		t.Fatal(err)
	}
	settings := config.Settings{CorePath: bin, MixedPort: 12345, ControllerPort: 12346}
	a := &App{store: st, reg: reg, set: settings, coreBin: bin, sup: &core.Supervisor{BinPath: bin}}
	sourceBefore, _ := os.ReadFile(st.MasterPath())
	settingsBefore, _ := os.ReadFile(st.SettingsPath())
	if err := a.commitProfile("", "rules: [MATCH,DIRECT]"); err == nil {
		t.Fatal("accepted failed core validation")
	}
	sourceAfter, _ := os.ReadFile(st.MasterPath())
	settingsAfter, _ := os.ReadFile(st.SettingsPath())
	if string(sourceBefore) != string(sourceAfter) || string(settingsBefore) != string(settingsAfter) {
		t.Fatal("rejected candidate changed source or settings")
	}
	if a.settingsBusy || a.settings().MixedPort != 12345 || st.ActiveProfile() != "" {
		t.Fatal("state not restored")
	}
	if err := a.commitProfile("new", "rules: []"); err == nil {
		t.Fatal("accepted new invalid profile")
	}
	if st.ProfileExists("new") {
		t.Fatal("invalid profile created")
	}
}
