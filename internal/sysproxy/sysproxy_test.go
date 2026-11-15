package sysproxy

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

type fakeDesktop struct {
	value             snapshot
	writes            int
	readErr, writeErr error
}

func (f *fakeDesktop) Read() (snapshot, error) { return f.value, f.readErr }
func (f *fakeDesktop) Write(s snapshot) error {
	if f.writeErr != nil {
		return f.writeErr
	}
	f.value = s
	f.writes++
	return nil
}
func fakeManager(t *testing.T) (*Manager, *fakeDesktop) {
	t.Helper()
	f := &fakeDesktop{value: snapshot{Mode: "auto", HTTPHost: "old", HTTPPort: 8123}}
	m := New(t.TempDir(), 7897)
	m.backend = f
	return m, f
}
func TestArmParkRestoreOriginal(t *testing.T) {
	m, f := fakeManager(t)
	before := f.value
	if err := m.Arm(); err != nil {
		t.Fatal(err)
	}
	if !m.Armed() {
		t.Fatal("not owned")
	}
	if err := m.Neutralize(); err != nil {
		t.Fatal(err)
	}
	if f.value.Mode != "none" || m.Armed() {
		t.Fatal("park state")
	}
	if err := m.Arm(); err != nil {
		t.Fatal(err)
	}
	if err := m.Disarm(); err != nil {
		t.Fatal(err)
	}
	if f.value != before {
		t.Fatal("lost original snapshot")
	}
	if err := m.EndSession(); err != nil {
		t.Fatal(err)
	}
}
func TestThirdPartyOwnershipNeverRestoredOrReasserted(t *testing.T) {
	for _, park := range []bool{false, true} {
		m, f := fakeManager(t)
		if err := m.acquire(park); err != nil {
			t.Fatal(err)
		}
		// Same port is insufficient: another host is a different owner.
		f.value.HTTPHost = "third-party"
		third := f.value
		writes := f.writes
		if m.Armed() {
			t.Fatal("claimed third party")
		}
		if err := m.Reassert(); err != nil {
			t.Fatal(err)
		}
		if err := m.Disarm(); err != nil {
			t.Fatal(err)
		}
		if f.value != third || f.writes != writes {
			t.Fatal("overwrote third party")
		}
	}
}
func TestHealingRequiresFullFingerprint(t *testing.T) {
	m, f := fakeManager(t)
	before := f.value
	if err := m.Arm(); err != nil {
		t.Fatal(err)
	}
	next := New(filepath.Dir(m.StatePath), m.Port)
	next.backend = f
	if !next.HealIfNeeded() || f.value != before {
		t.Fatal("did not heal owned crash")
	}
	if err := m.Neutralize(); err != nil {
		t.Fatal(err)
	}
	f.value.HTTPSPort = 9999
	third := f.value
	if next.HealIfNeeded() || f.value != third {
		t.Fatal("healed a different owner")
	}
}
func TestProxyPersistenceAndBackendFailures(t *testing.T) {
	m, f := fakeManager(t)
	m.StatePath = filepath.Join(t.TempDir(), "missing", "state.json")
	if err := m.Arm(); err == nil || f.writes != 0 {
		t.Fatal("changed desktop before journal persisted")
	}
	m, f = fakeManager(t)
	f.readErr = errors.New("unavailable")
	if err := m.Arm(); err == nil || f.writes != 0 {
		t.Fatal("armed without snapshot")
	}
	f.readErr = nil
	f.writeErr = errors.New("denied")
	if err := m.Arm(); err == nil {
		t.Fatal("ignored write failure")
	}
	if err := m.EndSession(); err == nil {
		t.Fatal("discarded pending recovery")
	}
	f.writeErr = nil
	if err := os.WriteFile(m.StatePath, []byte("broken"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := m.Disarm(); err == nil {
		t.Fatal("ignored corrupt journal")
	}
}
func TestProxyManagerConcurrentQueries(t *testing.T) {
	m, _ := fakeManager(t)
	var wg sync.WaitGroup
	for range 6 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 10 {
				_ = m.Arm()
				_ = m.Armed()
				_ = m.Reassert()
				_ = m.Disarm()
			}
		}()
	}
	wg.Wait()
}
