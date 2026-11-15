// Package sysproxy owns the GNOME system proxy settings. It implements
// the arm/restore/self-heal state machine proven in the Phase 0 spike:
//
//   - Arm records the previous settings, then points http/https/socks
//     at our core. Only ever called after the core is verified healthy.
//   - A graceful quit restores the recorded settings.
//   - If the process dies uncleanly (kill -9, crash), the next start
//     finds the state file with clean=false and the live settings still
//     pointing at our (dead) port, and heals them back.
package sysproxy

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	Armed bool     `json:"armed"`
	Clean bool     `json:"clean"`
	Prev  snapshot `json:"prev"`
	// Parked: TUN mode owns the settings — GNOME is set to "none" so
	// proxy-aware apps go direct and TUN captures them. Prev still
	// holds the pre-ClashGo snapshot for exit/heal restore. Armed and
	// Parked are mutually exclusive by construction.
	Parked bool `json:"parked,omitempty"`
}

type Manager struct {
	StatePath string
	Port      int
}

func New(stateDir string, port int) *Manager {
	return &Manager{StatePath: filepath.Join(stateDir, "proxy-state.json"), Port: port}
}

func gget(schema, key string) string {
	out, err := exec.Command("gsettings", "get", schema, key).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func gset(schema, key, val string) {
	_ = exec.Command("gsettings", "set", schema, key, val).Run()
}

func unq(s string) string { return strings.Trim(s, "'") }

func current() snapshot {
	var s snapshot
	s.Mode = unq(gget("org.gnome.system.proxy", "mode"))
	s.HTTPHost = unq(gget("org.gnome.system.proxy.http", "host"))
	fmt.Sscan(gget("org.gnome.system.proxy.http", "port"), &s.HTTPPort)
	s.HTTPSHost = unq(gget("org.gnome.system.proxy.https", "host"))
	fmt.Sscan(gget("org.gnome.system.proxy.https", "port"), &s.HTTPSPort)
	s.SocksHost = unq(gget("org.gnome.system.proxy.socks", "host"))
	fmt.Sscan(gget("org.gnome.system.proxy.socks", "port"), &s.SocksPort)
	return s
}

func restore(s snapshot) {
	gset("org.gnome.system.proxy.http", "host", s.HTTPHost)
	gset("org.gnome.system.proxy.http", "port", fmt.Sprint(s.HTTPPort))
	gset("org.gnome.system.proxy.https", "host", s.HTTPSHost)
	gset("org.gnome.system.proxy.https", "port", fmt.Sprint(s.HTTPSPort))
	gset("org.gnome.system.proxy.socks", "host", s.SocksHost)
	gset("org.gnome.system.proxy.socks", "port", fmt.Sprint(s.SocksPort))
	gset("org.gnome.system.proxy", "mode", s.Mode)
}

func (m *Manager) load() (state, bool) {
	b, err := os.ReadFile(m.StatePath)
	if err != nil {
		return state{}, false
	}
	var st state
	if json.Unmarshal(b, &st) != nil {
		return state{}, false
	}
	return st, true
}

func (m *Manager) save(st state) {
	b, _ := json.Marshal(st)
	_ = os.WriteFile(m.StatePath, b, 0o600)
}

// HealIfNeeded repairs a stale arm left by an unclean exit. Call before
// anything else touches the proxy. Reports whether it healed.
func (m *Manager) HealIfNeeded() bool {
	st, ok := m.load()
	if !ok || st.Clean {
		return false
	}
	cur := current()
	if st.Armed && cur.HTTPPort == m.Port {
		restore(st.Prev)
		st.Armed = false
		st.Clean = true
		m.save(st)
		return true
	}
	if st.Parked && cur.Mode == "none" {
		restore(st.Prev)
		st.Parked = false
		st.Clean = true
		m.save(st)
		return true
	}
	return false
}

// BeginSession marks the state unclean so a hard kill is detectable.
func (m *Manager) BeginSession() {
	if st, ok := m.load(); ok {
		st.Clean = false
		m.save(st)
	}
}

// applyArm writes the live settings that point the system proxy at
// our core. The caller owns the state file.
func (m *Manager) applyArm() {
	gset("org.gnome.system.proxy.http", "host", "127.0.0.1")
	gset("org.gnome.system.proxy.http", "port", fmt.Sprint(m.Port))
	gset("org.gnome.system.proxy.https", "host", "127.0.0.1")
	gset("org.gnome.system.proxy.https", "port", fmt.Sprint(m.Port))
	gset("org.gnome.system.proxy.socks", "host", "127.0.0.1")
	gset("org.gnome.system.proxy.socks", "port", fmt.Sprint(m.Port))
	gset("org.gnome.system.proxy", "mode", "manual")
}

// Arm points the system proxy at our core, recording previous values.
func (m *Manager) Arm() {
	if st, ok := m.load(); ok && st.Armed {
		if current().HTTPPort == m.Port {
			return // already armed by us; keep the original prev
		}
		// Stale flag (2026-11-11, found live): a previous session died
		// mid-shutdown after the live settings had been restored, so
		// the record claims we own a proxy we do not. Re-assert the
		// arm against the recorded prev instead of stranding the
		// desktop on whatever the live values happen to be.
		m.applyArm()
		return
	}
	prev := current()
	if st, ok := m.load(); ok && st.Parked {
		prev = st.Prev // TUN owned the settings; keep the original prev
	}
	st := state{Armed: true, Clean: false, Prev: prev}
	m.save(st)
	m.applyArm()
}

// Neutralize puts GNOME into "none" mode for TUN capture: apps that
// read the system proxy go direct, and TUN catches their traffic. The
// recorded prev is kept untouched so Disarm/EndSession and the healer
// can still restore the pre-ClashGo settings. Mutually exclusive with
// Arm — the caller picks exactly one capture mode.
func (m *Manager) Neutralize() {
	if st, ok := m.load(); ok && st.Parked {
		if current().Mode == "none" {
			return // already parked by us
		}
		// Stale flag, same 2026-11-11 incident as in Arm: the record
		// says parked but the live settings were restored underneath
		// us (they pointed at a dead port while TUN owned traffic).
		// Re-assert the park; the recorded prev stays authoritative.
		gset("org.gnome.system.proxy", "mode", "none")
		return
	}
	prev := current()
	if st, ok := m.load(); ok && st.Armed {
		prev = st.Prev // keep the original prev, not our own armed values
	}
	m.save(state{Parked: true, Clean: false, Prev: prev})
	gset("org.gnome.system.proxy", "mode", "none")
}

// Reassert is the watchdog half of the launch-time grab: while
// ClashGo owns the capture, another proxy app starting up (the
// FlClash behavior that prompted this, 2026-11-11) — or anything
// else — can rewrite the GNOME settings under us. Reassert checks
// the live settings against the recorded ownership and writes ours
// back when they no longer match. The recorded prev is never
// touched: the thief's values are not the desktop's original
// settings, ours still are. A no-op when we own nothing.
func (m *Manager) Reassert() {
	st, ok := m.load()
	if !ok {
		return
	}
	if st.Armed {
		cur := current()
		if cur.Mode != "manual" || cur.HTTPPort != m.Port || cur.HTTPSPort != m.Port || cur.SocksPort != m.Port {
			m.applyArm()
		}
	} else if st.Parked {
		if current().Mode != "none" {
			gset("org.gnome.system.proxy", "mode", "none")
		}
	}
}

// Disarm restores the recorded settings if we own them (armed or
// parked for TUN).
func (m *Manager) Disarm() {
	st, ok := m.load()
	if !ok || (!st.Armed && !st.Parked) {
		return
	}
	restore(st.Prev)
	st.Armed = false
	st.Parked = false
	st.Clean = true
	m.save(st)
}

// Armed reports whether the live settings currently point at our port.
func (m *Manager) Armed() bool {
	return current().HTTPPort == m.Port
}

// EndSession records a clean exit (call on graceful quit, after Disarm).
func (m *Manager) EndSession() {
	st, _ := m.load()
	st.Clean = true
	m.save(st)
}
