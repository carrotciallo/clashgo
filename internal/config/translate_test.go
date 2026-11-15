package config

import (
	"os"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// sampleConf exercises every translation the converter claims:
// logic rules, USER-AGENT drop, REJECT-NO-DROP downgrade, set
// providers in both behaviors, unknown policy mapping, [Host] in
// both forms, [General] notes and a FINAL.
const sampleConf = `# test conf
[General]
dns-server = https://doh.pub/dns-query
ipv6 = true

[Rule]
AND,((PROTOCOL,UDP),(DEST-PORT,443)),REJECT-NO-DROP
USER-AGENT,AppleTV*,PROXY
DOMAIN-SUFFIX,tv.apple.com,PROXY
DOMAIN-SUFFIX,music.apple.com,DIRECT
DOMAIN-KEYWORD,siri,PROXY
IP-CIDR,17.0.0.0/8,DIRECT,no-resolve
RULE-SET,https://example.com/rule/Shadowrocket/AppleTV/AppleTV.list,PROXY
DOMAIN-SET,https://example.com/rule/Shadowrocket/China/China_Domain.list,DIRECT
RULE-SET,https://example.com/rule/Shadowrocket/China/China_Domain.list,DIRECT
DOMAIN-SUFFIX,example.org,SomeCustomGroup
BOGUS-TYPE,whatever,PROXY
FINAL,PROXY

[Host]
localhost = 127.0.0.1
*.apple.com = server:system
*.example.net = server:1.1.1.1

[Script]
whatever = 1
`

func TestTranslateSample(t *testing.T) {
	master, rep, err := TranslateShadowrocket(sampleConf)
	if err != nil {
		t.Fatal(err)
	}
	rules := ExtractRules(master)
	// 6 private head + 10 translated (incl. MATCH from FINAL). The
	// sample points a RULE-SET and a DOMAIN-SET at the same URL:
	// different behavior means different providers.
	if len(rules) != 16 {
		t.Fatalf("expected 16 rules, got %d: %v", len(rules), rules)
	}
	for _, want := range []string{
		"AND,((NETWORK,UDP),(DST-PORT,443)),REJECT",
		"DOMAIN-SUFFIX,tv.apple.com,节点选择",
		"DOMAIN-SUFFIX,music.apple.com,DIRECT",
		"DOMAIN-KEYWORD,siri,节点选择",
		"IP-CIDR,17.0.0.0/8,DIRECT,no-resolve",
		"RULE-SET,appletv,节点选择",
		"RULE-SET,china-domain,DIRECT",
		"DOMAIN-SUFFIX,example.org,节点选择",
		"MATCH,节点选择",
	} {
		found := false
		for _, r := range rules {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Errorf("translated rules missing %q", want)
		}
	}
	if rep.Rules != 16 || rep.Providers != 3 {
		t.Errorf("report counts off: %+v", rep)
	}
	// USER-AGENT and the bogus type are dropped and listed.
	if len(rep.Dropped) != 2 {
		t.Errorf("expected 2 dropped lines, got %v", rep.Dropped)
	}
	// The two sets became providers at their original URLs.
	for _, want := range []string{
		"appletv:",
		"behavior: classical",
		"china-domain:",
		"behavior: domain",
		`url: "https://example.com/rule/Shadowrocket/AppleTV/AppleTV.list"`,
	} {
		if !strings.Contains(master, want) {
			t.Errorf("master missing %q", want)
		}
	}
	// [Host]: localhost becomes a hosts entry; server:system and
	// server:<ip> become nameserver-policy entries.
	var hostDoc map[string]any
	if err := yaml.Unmarshal([]byte(master), &hostDoc); err != nil {
		t.Fatal(err)
	}
	if hosts, ok := hostDoc["hosts"].(map[string]any); !ok || hosts["localhost"] != "127.0.0.1" {
		t.Error("hosts entry lost")
	}
	if !strings.Contains(master, `"+.apple.com":`) || !strings.Contains(master, `"+.example.net":`) {
		t.Error("nameserver-policy entries lost")
	}
	// Exactly one rule-providers block: the skeleton's own sets
	// are replaced, not duplicated (a duplicate key fails the
	// core's YAML parse at switch time).
	if n := strings.Count(master, "\nrule-providers:"); n != 1 {
		t.Errorf("expected exactly 1 rule-providers block, got %d", n)
	}
	// The result must survive a strict YAML parse and the normal render path.
	var decoded map[string]any
	if err := yaml.Unmarshal([]byte(master), &decoded); err != nil {
		t.Fatalf("translated YAML is invalid: %v", err)
	}
	out := Render(master, nil, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "RULE-SET,appletv,节点选择") {
		t.Error("rendered import lost its rules")
	}
}

func TestTranslateFragment(t *testing.T) {
	// A bare rule fragment (no sections) imports too, and a
	// missing FINAL gets a synthesized MATCH plus a note.
	master, rep, err := TranslateShadowrocket("DOMAIN-SUFFIX,example.com,DIRECT\nDOMAIN,foo.bar,REJECT\n")
	if err != nil {
		t.Fatal(err)
	}
	rules := ExtractRules(master)
	if rules[len(rules)-1] != "MATCH,节点选择" {
		t.Errorf("fragment import should end in MATCH, got %v", rules)
	}
	joined := strings.Join(rep.Notes, " ")
	if !strings.Contains(joined, "FINAL") {
		t.Errorf("missing-FINAL note absent: %v", rep.Notes)
	}
}

func TestTranslateMalformedKnownRule(t *testing.T) {
	_, rep, err := TranslateShadowrocket("[Rule]\nDOMAIN\nIP-CIDR,x\nDOMAIN,x,no-resolve\nFINAL,DIRECT\n")
	if err != nil || len(rep.Dropped) != 3 {
		t.Fatalf("malformed rules not reported: %+v, %v", rep, err)
	}
}

func TestTranslateEmpty(t *testing.T) {
	if _, _, err := TranslateShadowrocket("[General]\nipv6 = true\n"); err == nil {
		t.Error("a config with no rules must be refused")
	}
}

// TestTranslateGoldenV10 runs only when CLASHGO_GOLDEN_CONF points
// at a real Shadowrocket config (kept out of the repo: it is the
// user's personal config). It asserts the converter reproduces
// the hand-synced rules of 2026-11-12 exactly.
func TestTranslateGoldenV10(t *testing.T) {
	path := os.Getenv("CLASHGO_GOLDEN_CONF")
	if path == "" {
		t.Skip("CLASHGO_GOLDEN_CONF not set")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	master, rep, err := TranslateShadowrocket(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	rules := ExtractRules(master)
	t.Logf("rules=%d providers=%d dropped=%v notes=%v", len(rules), rep.Providers, rep.Dropped, rep.Notes)
	for _, r := range rules {
		t.Logf("RULE %s", r)
	}
}
