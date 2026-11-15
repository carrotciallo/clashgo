package main

import (
	"github.com/carrotciallo/clashgo/internal/core"
	"testing"
)

func TestFilterNodes(t *testing.T) {
	nodes := []core.Proxy{{Name: "__dialer", Type: "HTTP"}, {Name: "Hong Kong 香港", Type: "Shadowsocks"}, {Name: "Tokyo", Type: "Trojan"}}
	for query, want := range map[string]int{"": 2, " hong KONG ": 1, "香港": 1, "trojan": 1, "missing": 0, "__": 0} {
		if got := len(filterNodes(nodes, query)); got != want {
			t.Errorf("query %q: %d != %d", query, got, want)
		}
	}
	if len(nodes) != 3 || nodes[0].Name != "__dialer" {
		t.Fatal("filter mutated snapshot")
	}
}
