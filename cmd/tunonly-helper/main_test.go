package main

import (
	"strings"
	"testing"

	"github.com/carrotciallo/clashgo/internal/config"
)

// The table must exempt the core's marked sockets and the node, keep LAN
// open, and end in a drop on the uplink — and the mark must be the one
// the renderer stamps on the core.
func TestRulesetContract(t *testing.T) {
	if got := "0x4347"; coreMark != got {
		t.Fatalf("coreMark = %s, want %s", coreMark, got)
	}
	if config.CoreRoutingMark != 0x4347 {
		t.Fatalf("config.CoreRoutingMark = %#x, want 0x4347", config.CoreRoutingMark)
	}
	for _, want := range []string{
		"meta mark " + coreMark + " accept",
		"ip daddr 45.32.51.34 udp dport { 443, 8443 } accept",
		"ip daddr 45.32.51.34 tcp dport 443 accept",
		"ip daddr 192.168.2.0/24 accept",
		`oifname "enp5s0" drop`,
	} {
		if !strings.Contains(ruleset, want) {
			t.Errorf("ruleset missing %q", want)
		}
	}
}
