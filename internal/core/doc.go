// Package core supervises the mihomo sidecar process (start, stop,
// crash restart, log capture) and talks to it through the External
// Controller REST API. It also owns the system-proxy switch and its
// crash recovery (a dead proxy target must never strand the desktop).
package core
