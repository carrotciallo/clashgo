package main

import (
	"testing"
	"time"
)

func TestMessageExpiresAndReplacementKeepsOwnDeadline(t *testing.T) {
	a := &App{}
	a.setMsg("updated %d nodes", 68)
	first := a.msgUntil
	if msg, _, _, _, _, _, _ := a.snapshotAt(first.Add(-time.Nanosecond)); msg != "updated 68 nodes" {
		t.Fatalf("message missing before expiry: %q", msg)
	}
	if msg, _, _, _, _, _, _ := a.snapshotAt(first); msg != "" {
		t.Fatalf("message retained at expiry: %q", msg)
	}
	a.setMsg("new result")
	second := a.msgUntil
	if !second.After(first) {
		t.Fatal("replacement did not receive a fresh deadline")
	}
	if msg, _, _, _, _, _, _ := a.snapshotAt(first); msg != "new result" {
		t.Fatalf("old expiry removed replacement: %q", msg)
	}
	if msg, _, _, _, _, _, _ := a.snapshotAt(second.Add(time.Hour)); msg != "" {
		t.Fatalf("expired result returned after reopening: %q", msg)
	}
}

func TestBusyProgressDoesNotExpireAndClearsOnCompletion(t *testing.T) {
	a := &App{}
	a.setBusy(true, "fetching")
	if msg, busy, _, _, _, _, _ := a.snapshotAt(time.Now().Add(time.Hour)); msg != "fetching" || !busy {
		t.Fatalf("progress expired: %q, %v", msg, busy)
	}
	a.setBusy(false, "")
	if msg, busy, _, _, _, _, _ := a.snapshot(); msg != "" || busy {
		t.Fatalf("stale progress: %q, %v", msg, busy)
	}
}

func TestResultSurvivesDeferredBusyResetThenExpires(t *testing.T) {
	a := &App{}
	a.setBusy(true, "fetching")
	a.setMsg("updated")
	if msg, _, _, _, _, _, _ := a.snapshotAt(a.msgUntil.Add(time.Hour)); msg != "updated" {
		t.Fatalf("result lost while task running: %q", msg)
	}
	a.setBusy(false, "")
	deadline := a.msgUntil
	if msg, _, _, _, _, _, _ := a.snapshotAt(deadline.Add(-time.Second)); msg != "updated" {
		t.Fatalf("result lost on busy reset: %q", msg)
	}
	if msg, _, _, _, _, _, _ := a.snapshotAt(deadline); msg != "" {
		t.Fatalf("completed result persisted: %q", msg)
	}
	a.setBusy(false, "done")
	if a.msgUntil.IsZero() {
		t.Fatal("idle message has no expiry")
	}
	a.setMsg("")
	if a.msg != "" || !a.msgUntil.IsZero() {
		t.Fatal("explicit clear did not reset deadline")
	}
}
