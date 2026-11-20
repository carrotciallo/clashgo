package main

import (
	"os"
	"path/filepath"
)

// disguiseCoreLink returns a neutral-named symlink to the core binary
// inside the runtime dir, so process listings show neither the core's
// product name nor a proxy label. The link resolves to the real binary,
// so its file capabilities are preserved. Any failure falls back to the
// plain binary path: the disguise is cosmetic and must never block a
// core start.
func disguiseCoreLink(runtimeDir, coreBin string) string {
	abs, err := filepath.Abs(coreBin)
	if err != nil {
		return coreBin
	}
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		return coreBin
	}
	link := filepath.Join(runtimeDir, "cored")
	if target, err := os.Readlink(link); err == nil && target == abs {
		return link
	}
	_ = os.Remove(link)
	if err := os.Symlink(abs, link); err != nil {
		return coreBin
	}
	return link
}
