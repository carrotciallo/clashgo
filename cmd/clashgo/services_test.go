package main

import (
	"encoding/json"
	"github.com/carrotciallo/clashgo/internal/config"
	"testing"
)

func TestServiceEmptySelectionPersists(t *testing.T) {
	a := &App{store: &config.Store{ConfigDir: t.TempDir()}}
	if len(a.enabledServices()) != len(serviceDefs) {
		t.Fatal("legacy defaults lost")
	}
	for _, s := range serviceDefs {
		a.setServiceEnabled(s.Name, false)
	}
	if a.set.Services == nil || len(a.enabledServices()) != 0 {
		t.Fatal("disabling last service reenabled catalog")
	}
	b, err := json.Marshal(a.set)
	if err != nil {
		t.Fatal(err)
	}
	var loaded config.Settings
	if err := json.Unmarshal(b, &loaded); err != nil {
		t.Fatal(err)
	}
	a.set = loaded
	if len(a.enabledServices()) != 0 {
		t.Fatal("empty selection lost on reload")
	}
	a.setServiceEnabled(serviceDefs[0].Name, true)
	if len(a.enabledServices()) != 1 {
		t.Fatal("enabling one restored all")
	}
}
