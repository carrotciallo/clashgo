// Package sysproxy restores desktop settings only while their full fingerprint
// still matches the values this application wrote. It never fights a new owner.
package sysproxy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

type snapshot struct {
	Mode      string `json:"mode"`
	HTTPHost  string `json:"httpHost"`
	HTTPPort  int    `json:"httpPort"`
	HTTPSHost string `json:"httpsHost"`
	HTTPSPort int    `json:"httpsPort"`
	SocksHost string `json:"socksHost"`
	SocksPort int    `json:"socksPort"`
}
type state struct {
	Armed    bool      `json:"armed"`
	Clean    bool      `json:"clean"`
	Prev     snapshot  `json:"prev"`
	Parked   bool      `json:"parked,omitempty"`
	Expected *snapshot `json:"expected,omitempty"`
}
type backend interface {
	Read() (snapshot, error)
	Write(snapshot) error
}
type Manager struct {
	mu        sync.Mutex
	StatePath string
	Port      int
	backend   backend
	lastErr   error
}

func New(stateDir string, port int) *Manager {
	return &Manager{StatePath: filepath.Join(stateDir, "proxy-state.json"), Port: port, backend: desktop{}}
}

type desktop struct{}

func gsettings(args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	b, err := exec.CommandContext(ctx, "gsettings", args...).Output()
	if err != nil {
		return "", fmt.Errorf("gsettings %s failed: %w", args[0], err)
	}
	return strings.TrimSpace(string(b)), nil
}
func (desktop) Read() (snapshot, error) {
	var s snapshot
	fields := []struct {
		schema, key string
		target      *string
	}{
		{"org.gnome.system.proxy", "mode", &s.Mode},
		{"org.gnome.system.proxy.http", "host", &s.HTTPHost},
		{"org.gnome.system.proxy.https", "host", &s.HTTPSHost},
		{"org.gnome.system.proxy.socks", "host", &s.SocksHost},
	}
	for _, f := range fields {
		v, err := gsettings("get", f.schema, f.key)
		if err != nil {
			return s, err
		}
		*f.target = strings.Trim(v, "'\"")
	}
	for _, f := range []struct {
		schema string
		target *int
	}{{"http", &s.HTTPPort}, {"https", &s.HTTPSPort}, {"socks", &s.SocksPort}} {
		v, err := gsettings("get", "org.gnome.system.proxy."+f.schema, "port")
		if err != nil {
			return s, err
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			return s, err
		}
		*f.target = n
	}
	return s, nil
}
func (desktop) Write(s snapshot) error {
	// Mode last: a fresh arm cannot become active until all endpoints exist.
	for _, f := range []struct{ schema, key, val string }{
		{"http", "host", strconv.Quote(s.HTTPHost)}, {"http", "port", strconv.Itoa(s.HTTPPort)},
		{"https", "host", strconv.Quote(s.HTTPSHost)}, {"https", "port", strconv.Itoa(s.HTTPSPort)},
		{"socks", "host", strconv.Quote(s.SocksHost)}, {"socks", "port", strconv.Itoa(s.SocksPort)},
	} {
		if _, err := gsettings("set", "org.gnome.system.proxy."+f.schema, f.key, f.val); err != nil {
			return err
		}
	}
	_, err := gsettings("set", "org.gnome.system.proxy", "mode", strconv.Quote(s.Mode))
	return err
}
func (m *Manager) load() (state, error) {
	b, err := os.ReadFile(m.StatePath)
	if errors.Is(err, os.ErrNotExist) {
		return state{}, nil
	}
	if err != nil {
		return state{}, err
	}
	var st state
	err = json.Unmarshal(b, &st)
	return st, err
}
func (m *Manager) save(st state) error {
	b, err := json.Marshal(st)
	if err != nil {
		return err
	}
	return config.WriteFileAtomic(m.StatePath, b, 0600)
}
func (m *Manager) armSnapshot() snapshot {
	return snapshot{Mode: "manual", HTTPHost: "127.0.0.1", HTTPPort: m.Port, HTTPSHost: "127.0.0.1", HTTPSPort: m.Port, SocksHost: "127.0.0.1", SocksPort: m.Port}
}
func (m *Manager) owns(st state, cur snapshot) bool {
	if !st.Armed && !st.Parked {
		return false
	}
	if st.Expected != nil {
		return cur == *st.Expected
	}
	// Legacy parked records have no full fingerprint, so cannot prove ownership.
	return st.Armed && cur == m.armSnapshot()
}
func relinquish(st state) state {
	st.Armed = false
	st.Parked = false
	st.Clean = true
	st.Expected = nil
	return st
}

func (m *Manager) HealIfNeeded() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		m.lastErr = err
		return false
	}
	if st.Clean {
		return false
	}
	cur, err := m.backend.Read()
	if err != nil {
		m.lastErr = err
		return false
	}
	healed := m.owns(st, cur)
	if healed {
		if err = m.backend.Write(st.Prev); err != nil {
			m.lastErr = err
			return false
		}
	}
	m.lastErr = m.save(relinquish(st))
	return healed && m.lastErr == nil
}
func (m *Manager) BeginSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return err
	}
	st.Clean = false
	return m.save(st)
}
func (m *Manager) Arm() error        { return m.acquire(false) }
func (m *Manager) Neutralize() error { return m.acquire(true) }
func (m *Manager) acquire(park bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return err
	}
	cur, err := m.backend.Read()
	if err != nil {
		return err
	}
	prev := cur
	if m.owns(st, cur) {
		prev = st.Prev
	}
	target := m.armSnapshot()
	if park {
		target = cur
		target.Mode = "none"
	}
	next := state{Armed: !park, Parked: park, Prev: prev, Expected: &target}
	// Write-ahead ownership journal. A mismatching partial write is never healed
	// over another application's settings; callers receive the backend error.
	if err = m.save(next); err != nil {
		return err
	}
	if err = m.backend.Write(target); err != nil {
		return err
	}
	check, err := m.backend.Read()
	if err != nil {
		return err
	}
	if check != target {
		return fmt.Errorf("system proxy changed during acquisition")
	}
	return nil
}

// Reassert now yields on interference instead of stealing the desktop back.
// Kept as a compatibility name for the periodic ownership check.
func (m *Manager) Reassert() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return err
	}
	if !st.Armed && !st.Parked {
		return nil
	}
	cur, err := m.backend.Read()
	if err != nil {
		return err
	}
	if !m.owns(st, cur) {
		return m.save(relinquish(st))
	}
	return nil
}
func (m *Manager) Disarm() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return err
	}
	if !st.Armed && !st.Parked {
		return nil
	}
	cur, err := m.backend.Read()
	if err != nil {
		return err
	}
	if m.owns(st, cur) {
		if err = m.backend.Write(st.Prev); err != nil {
			return err
		}
	}
	return m.save(relinquish(st))
}
func (m *Manager) Armed() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil || !st.Armed {
		return false
	}
	cur, err := m.backend.Read()
	return err == nil && m.owns(st, cur)
}
func (m *Manager) EndSession() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, err := m.load()
	if err != nil {
		return err
	}
	// Do not erase the recovery journal after a failed Disarm.
	if st.Armed || st.Parked {
		return fmt.Errorf("system proxy ownership not released")
	}
	st.Clean = true
	return m.save(st)
}
func (m *Manager) LastError() error { m.mu.Lock(); defer m.mu.Unlock(); return m.lastErr }
