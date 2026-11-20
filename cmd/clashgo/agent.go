package main

// Agent-facing surface: a stable, machine-readable view of the app
// (schema v1) shared by the status file, the control socket and the
// CLI. Contract: see AGENTS.md at the repository root. The status
// never carries secrets — no controller secret, no subscription URLs.

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/subs"
)

const agentSchema = 1

type agentSubStatus struct {
	Name      string    `json:"name"`
	Nodes     int       `json:"nodes"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	LastError string    `json:"lastError,omitempty"`
	Local     bool      `json:"local,omitempty"`
	Split     bool      `json:"split,omitempty"`
}

type agentStatus struct {
	Schema  int       `json:"schema"`
	Version string    `json:"version"`
	Commit  string    `json:"commit"`
	Time    time.Time `json:"time"`
	// Stale marks a status read from disk whose heartbeat aged out or
	// whose control socket did not answer: the fields then describe
	// the last known state, not a live one.
	Stale bool `json:"stale,omitempty"`

	Instance struct {
		Running   bool      `json:"running"`
		PID       int       `json:"pid"`
		StartedAt time.Time `json:"startedAt,omitempty"`
		Heartbeat time.Time `json:"heartbeat"`
	} `json:"instance"`
	Core struct {
		Running bool      `json:"running"`
		PID     int       `json:"pid,omitempty"`
		Version string    `json:"version,omitempty"`
		Healthy bool      `json:"healthy"`
		Since   time.Time `json:"since,omitempty"`
		Error   string    `json:"error,omitempty"`
	} `json:"core"`
	Capture struct {
		Mode             string `json:"mode"` // tun | system | off
		TUNEnabled       bool   `json:"tunEnabled"`
		SystemProxyArmed bool   `json:"systemProxyArmed"`
		Ready            bool   `json:"ready"`
		MixedPort        int    `json:"mixedPort"`
		ControllerPort   int    `json:"controllerPort"`
	} `json:"capture"`
	Routing struct {
		Mode string `json:"mode"` // rule | global | direct
	} `json:"routing"`
	Selection struct {
		Group string   `json:"group"`
		Node  string   `json:"node"`
		Chain []string `json:"chain"`
	} `json:"selection"`
	Egress struct {
		IP  string `json:"ip,omitempty"`
		Org string `json:"org,omitempty"`
	} `json:"egress"`
	Traffic struct {
		UpRate    int64 `json:"upRate"`
		DownRate  int64 `json:"downRate"`
		UpTotal   int64 `json:"upTotal"`
		DownTotal int64 `json:"downTotal"`
	} `json:"traffic"`
	Busy          bool             `json:"busy"`
	Message       string           `json:"message,omitempty"`
	Subscriptions []agentSubStatus `json:"subscriptions"`
}

func buildVersionInfo() (version, commit string) {
	version, commit = "dev", ""
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return version, commit
	}
	if bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		version = bi.Main.Version
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.revision" && len(s.Value) >= 7 {
			commit = s.Value[:7]
		}
	}
	return version, commit
}

func sanitizeSubs(items []subs.Subscription) []agentSubStatus {
	out := make([]agentSubStatus, 0, len(items))
	for _, s := range items {
		out = append(out, agentSubStatus{
			Name: s.Name, Nodes: s.NodeCount, UpdatedAt: s.UpdatedAt,
			LastError: s.LastError, Local: s.Local, Split: s.Split,
		})
	}
	return out
}

// buildStatus snapshots the live app. Called from the poll loop and
// the control handlers; it takes only short locks and one cached
// controller read (the core version, refetched when the PID changes).
func (a *App) buildStatus() agentStatus {
	var st agentStatus
	st.Schema = agentSchema
	st.Version, st.Commit = buildVersionInfo()
	now := time.Now()
	st.Time = now

	set := a.settings()
	st.Instance.Running = !a.quitting.Load()
	st.Instance.PID = os.Getpid()
	st.Instance.StartedAt = a.startupStarted
	st.Instance.Heartbeat = now

	a.mu.Lock()
	st.Core.Healthy = a.coreOK
	st.Core.Error = a.coreErr
	st.Core.Since = a.coreSince
	st.Routing.Mode = a.mode
	st.Egress.IP = a.egress
	st.Egress.Org = a.egressOrg
	st.Traffic.UpRate = a.upRate
	st.Traffic.DownRate = a.downRate
	st.Traffic.UpTotal = a.up
	st.Traffic.DownTotal = a.down
	st.Busy = a.busy
	msg := a.msg
	capture := a.capture
	chain := a.egressChainLocked()
	coreVersion, coreVersionPID := a.coreVersion, a.coreVersionPID
	a.mu.Unlock()
	st.Message = msg

	st.Core.Running = a.sup.Running()
	st.Core.PID = a.sup.PID()
	if st.Core.Running && st.Core.PID != 0 {
		if coreVersionPID != st.Core.PID {
			ctx, cancel := context.WithTimeout(a.appContext(), time.Second)
			if v, err := a.client().Version(ctx); err == nil {
				coreVersion = v
			}
			cancel()
			a.mu.Lock()
			a.coreVersion, a.coreVersionPID = coreVersion, st.Core.PID
			a.mu.Unlock()
		}
		st.Core.Version = coreVersion
	}

	st.Capture.TUNEnabled = set.TUNEnabled
	st.Capture.SystemProxyArmed = a.proxyManager().Armed()
	st.Capture.Ready = capture.Active
	st.Capture.MixedPort = set.MixedPort
	st.Capture.ControllerPort = set.ControllerPort
	switch {
	case !st.Core.Running:
		st.Capture.Mode = "off"
	case set.TUNEnabled:
		st.Capture.Mode = "tun"
	default:
		st.Capture.Mode = "system"
	}

	st.Selection.Group = "节点选择"
	if len(chain) > 0 {
		st.Selection.Chain = chain
		st.Selection.Node = chain[len(chain)-1]
	}
	st.Subscriptions = sanitizeSubs(a.reg.Snapshot())
	return st
}

func (a *App) statusFilePath() string {
	return filepath.Join(a.store.StateDir, "status.json")
}

// writeStatusFile persists the live status for out-of-process readers
// (the CLI's offline path, external monitors). Best effort: a failed
// write must never disturb the app.
func (a *App) writeStatusFile() {
	if !a.settings().AgentSurface {
		return
	}
	data, err := json.Marshal(a.buildStatus())
	if err != nil {
		return
	}
	_ = config.WriteFileAtomic(a.statusFilePath(), data, 0o644)
}

func readStatusFile(store *config.Store) (agentStatus, bool) {
	var st agentStatus
	data, err := os.ReadFile(filepath.Join(store.StateDir, "status.json"))
	if err != nil {
		return st, false
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return st, false
	}
	return st, true
}

// offlineStatus builds the best possible status without a live
// instance: persisted settings and the subscription registry, both
// read from disk. The instance is reported stopped and stale.
func offlineStatus(store *config.Store) agentStatus {
	var st agentStatus
	st.Schema = agentSchema
	st.Version, st.Commit = buildVersionInfo()
	st.Time = time.Now()
	st.Stale = true
	st.Subscriptions = []agentSubStatus{}
	if set, err := store.LoadSettings(); err == nil {
		st.Capture.TUNEnabled = set.TUNEnabled
		st.Capture.MixedPort = set.MixedPort
		st.Capture.ControllerPort = set.ControllerPort
	}
	st.Capture.Mode = "off"
	if reg, err := subs.Load(store.SubsPath()); err == nil {
		st.Subscriptions = sanitizeSubs(reg.Snapshot())
	}
	if disk, ok := readStatusFile(store); ok {
		st.Instance.PID = disk.Instance.PID
		st.Instance.StartedAt = disk.Instance.StartedAt
		st.Instance.Heartbeat = disk.Instance.Heartbeat
	}
	return st
}
