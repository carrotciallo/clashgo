package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInstallRuntimeTransaction(t *testing.T) {
	for _, scenario := range []string{"success", "validation", "activation", "rollback"} {
		t.Run(scenario, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "config.yaml")
			if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
				t.Fatal(err)
			}
			calls := 0
			err := InstallRuntime(path, "new", func(candidate string) error {
				if candidate == path {
					t.Fatal("validated after install")
				}
				info, e := os.Stat(candidate)
				if e != nil || info.Mode().Perm() != 0600 {
					t.Fatal("candidate is not private")
				}
				if scenario == "validation" {
					return errors.New("bad config")
				}
				return nil
			}, func(active string) error {
				calls++
				b, _ := os.ReadFile(active)
				if calls == 1 && string(b) != "new" {
					t.Fatal("new config not activated")
				}
				if calls == 2 && string(b) != "old" {
					t.Fatal("old config not restored")
				}
				if scenario == "activation" && calls == 1 || scenario == "rollback" {
					return errors.New("reload failed")
				}
				return nil
			})
			b, _ := os.ReadFile(path)
			if scenario == "success" {
				if err != nil || string(b) != "new" {
					t.Fatal(err)
				}
			} else if err == nil || string(b) != "old" {
				t.Fatalf("failed transaction lost snapshot: %s %v", b, err)
			}
			if scenario == "rollback" && !strings.Contains(err.Error(), "reload restored runtime") {
				t.Fatal("rollback error hidden")
			}
			entries, _ := os.ReadDir(dir)
			if len(entries) != 1 {
				t.Fatal("temporary files left behind")
			}
		})
	}
}
