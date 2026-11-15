package subs

import (
	"strings"
	"testing"
)

const fullConfigSample = `port: 7890
mode: rule
proxies:
  - {name: __d1, type: anytls, server: a.example, port: 443, password: x, dialer-proxy: __d2, udp: false}
  - {name: __d2, type: anytls, server: b.example, port: 443, password: y, udp: false}
  - {name: __d3, type: anytls, server: c.example, port: 443, password: z}
proxy-groups:
  - name: 国外流量
    type: select
    proxies: [H01, DIRECT]
  - name: H01
    type: fallback
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies: [__d1]
  - name: "剩余流量: 9TB"
    type: fallback
    proxies: [__d3]
rules:
  - MATCH,国外流量
`

func TestSplitFullConfigFlattens(t *testing.T) {
	providerYAML, plumbingYAML, names, ok := SplitFullConfig([]byte(fullConfigSample))
	if !ok {
		t.Fatal("full config not detected")
	}
	// Only the leaf group becomes a node.
	if len(names) != 1 || names[0] != "H01" {
		t.Fatalf("node names: %v", names)
	}
	s := string(providerYAML)
	// The leaf is a renamed copy of its first member proxy.
	if !strings.Contains(s, "H01") || !strings.Contains(s, "a.example") {
		t.Errorf("flattened node missing:\n%s", s)
	}
	// No plumbing travels inside the provider document (the leaf's
	// dialer-proxy *reference* to it is of course still there)...
	if strings.Contains(s, "name: __d2") {
		t.Errorf("plumbing leaked into the provider document:\n%s", s)
	}
	// ...it is returned separately, under its own name.
	if p := string(plumbingYAML); !strings.Contains(p, "__d2") || !strings.Contains(p, "b.example") {
		t.Errorf("dialer plumbing missing from the sidecar:\n%s", p)
	}
	// The source proxy's own name is gone (renamed), the notice
	// leaf and its chain are dropped, and no group survives.
	for _, gone := range []string{"name: __d1", "c.example", "国外流量", "proxy-groups", "剩余流量"} {
		if strings.Contains(s, gone) {
			t.Errorf("provider document should not contain %q:\n%s", gone, s)
		}
	}
	// The payload's udp: false is chain bookkeeping, not a
	// capability: split output asserts UDP on both the flattened
	// node and its dialer plumbing (measured live 2026-11-12 —
	// with the false inherited, all UDP dies at the local gate).
	if !strings.Contains(s, "udp: true") || strings.Contains(s, "udp: false") {
		t.Errorf("flattened node must assert udp:\n%s", s)
	}
	if p := string(plumbingYAML); !strings.Contains(p, "udp: true") || strings.Contains(p, "udp: false") {
		t.Errorf("plumbing must assert udp:\n%s", p)
	}
}

func TestSplitPlainProvider(t *testing.T) {
	plain := "proxies:\n  - {name: n1, type: ss, server: s.example, port: 8388}\n"
	if _, _, _, ok := SplitFullConfig([]byte(plain)); ok {
		t.Error("plain provider payload must not be treated as full config")
	}
}

// Pool-family payload (one provider's new panel, measured live
// 2026-11-12): the proxies are the node pool, every service
// group lists all of them, and the one all-proxy group is a
// url-test over the whole pool whose first member is a notice
// entry pointing at 8.8.8.8. Flattening that group once reduced
// 72 nodes to a single clone of the notice; the pool must pass
// through as the nodes instead.
const poolConfigSample = `port: 7890
mode: rule
proxies:
  - {name: '📊 剩余: 41.77GB / 总计: 50GB', type: direct}
  - {name: '拉取于->2026-11-12 22:49', type: ss, server: 8.8.8.8, port: 12345, cipher: aes-128-gcm, password: x, udp: true}
  - {name: n1, type: anytls, server: a.example, port: 443, password: x, udp: false}
  - {name: n2, type: anytls, server: b.example, port: 443, password: y}
proxy-groups:
  - name: 节点选择
    type: select
    proxies: [自动选择, n1, n2, DIRECT]
  - name: 自动选择
    type: url-test
    url: http://www.gstatic.com/generate_204
    interval: 300
    proxies: ['拉取于->2026-11-12 22:49', n1, n2]
  - name: 国外媒体
    type: select
    proxies: [节点选择, n1, n2]
rules:
  - MATCH,节点选择
`

func TestSplitPoolConfigKeepsProxies(t *testing.T) {
	providerYAML, _, names, ok := SplitFullConfig([]byte(poolConfigSample))
	if !ok {
		t.Fatal("pool config not detected as full config")
	}
	if len(names) != 2 || names[0] != "n1" || names[1] != "n2" {
		t.Fatalf("node names: %v", names)
	}
	s := string(providerYAML)
	for _, want := range []string{"a.example", "b.example"} {
		if !strings.Contains(s, want) {
			t.Errorf("pool node missing %q:\n%s", want, s)
		}
	}
	for _, gone := range []string{"8.8.8.8", "自动选择", "剩余", "proxy-groups"} {
		if strings.Contains(s, gone) {
			t.Errorf("provider document should not contain %q:\n%s", gone, s)
		}
	}
	// Pool nodes assert UDP too, whatever the payload claimed.
	if !strings.Contains(s, "udp: true") || strings.Contains(s, "udp: false") {
		t.Errorf("pool nodes must assert udp:\n%s", s)
	}
}
