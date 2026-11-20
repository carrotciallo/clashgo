package main

// Structured event log (JSON Lines) for agents and post-hoc
// diagnosis: one JSON object per line, {"time","kind",...fields}.
// Written to <state>/logs/events.jsonl with a single rotated
// generation (events.jsonl.1) once the file passes ~2 MiB. Emission
// is best effort and never blocks the caller on error.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const eventLogMaxBytes = 2 << 20

type eventLog struct {
	mu     sync.Mutex
	path   string
	sealed bool
}

// setSealed switches the log to (or from) sealed mode: while sealed,
// each emit briefly unseals the directory and file to write, then
// seals them again (mode 0000).
func (l *eventLog) setSealed(sealed bool) {
	l.mu.Lock()
	l.sealed = sealed
	l.mu.Unlock()
}

func newEventLog(path string) *eventLog { return &eventLog{path: path} }

func (l *eventLog) emit(kind string, fields map[string]any) {
	if l == nil {
		return
	}
	rec := make(map[string]any, len(fields)+2)
	rec["time"] = time.Now().Format(time.RFC3339Nano)
	rec["kind"] = kind
	for k, v := range fields {
		rec[k] = v
	}
	data, err := json.Marshal(rec)
	if err != nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.sealed {
		dir := filepath.Dir(l.path)
		_ = os.Chmod(dir, 0o700)
		_ = os.Chmod(l.path, 0o600)
		defer func() {
			_ = os.Chmod(l.path, 0o000)
			_ = os.Chmod(dir, 0o000)
		}()
	}
	if fi, err := os.Stat(l.path); err == nil && fi.Size() > eventLogMaxBytes {
		_ = os.Rename(l.path, l.path+".1")
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.Write(append(data, '\n'))
}

// emitEvent is the App-level shortcut; safe before the log exists.
func (a *App) emitEvent(kind string, fields map[string]any) {
	if a.events != nil {
		a.events.emit(kind, fields)
	}
}

func eventsPath(store interface{ LogsDir() string }) string {
	return filepath.Join(store.LogsDir(), "events.jsonl")
}

// readEvents loads the rotated generation followed by the live file,
// oldest first. Unparseable lines are skipped.
func readEvents(logsDir string) []map[string]any {
	var out []map[string]any
	for _, path := range []string{
		filepath.Join(logsDir, "events.jsonl.1"),
		filepath.Join(logsDir, "events.jsonl"),
	} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range splitLines(string(data)) {
			if line == "" {
				continue
			}
			var rec map[string]any
			if err := json.Unmarshal([]byte(line), &rec); err == nil {
				out = append(out, rec)
			}
		}
	}
	return out
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		lines = append(lines, s[start:])
	}
	return lines
}
