package config

import (
	"strings"
	"testing"
)

func TestRenderProvidersAndUseAll(t *testing.T) {
	st := Settings{MixedPort: 7897, ControllerPort: 9097, ControllerSecret: "s3cr3t"}
	out := Render(DefaultMaster, []ProviderSource{{Name: "airport-a", URL: "https://example.com/sub"}}, st)
	for _, want := range []string{
		"airport-a:",
		"path: ./providers/airport-a.yaml",
		"use: [airport-a]",
		"mixed-port: 7897",
		"external-controller: 127.0.0.1:9097",
		"secret: s3cr3t",
		"allow-lan: false",
		"GEOIP,CN,DIRECT", // user content passes through
	} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q", want)
		}
	}
	if strings.Count(out, "use: [airport-a]") != 1 {
		t.Errorf("expected the single use-all group rewritten, got:\n%s", out)
	}
	// The plumbing groups were preprocessed out entirely (user
	// directive 2026-11-12): 节点选择 is the only group the
	// template may define.
	for _, gone := range []string{"自动选择", "故障转移"} {
		if strings.Contains(out, gone) {
			t.Errorf("rendered config still carries plumbing group %q", gone)
		}
	}
}

func TestTemplateCarriesLiveFindings(t *testing.T) {
	// Pinned from the 2026-11-11 live debugging: without
	// proxy-server-nameserver the proxy-bound nameserver deadlocks all
	// node sessions; without exclude-type, Direct-type info entries in
	// airport lists capture url-test groups.
	if !strings.Contains(DefaultMaster, "proxy-server-nameserver:") {
		t.Error("template lost proxy-server-nameserver")
	}
	out := Render(DefaultMaster, []ProviderSource{{Name: "a", URL: "https://x"}}, Settings{})
	if !strings.Contains(out, "exclude-type:") {
		t.Error("rendered providers lost exclude-type")
	}
}

func TestRenderNoProviders(t *testing.T) {
	out := Render(DefaultMaster, nil, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "proxy-providers: {}") {
		t.Error("empty providers should render as {}")
	}
	if !strings.Contains(out, "use: []") {
		t.Error("use-all with no providers should render use: []")
	}
}

// Regression (2026-11-12): a split subscription whose plumbing is
// empty must not splice a bare "[]" under proxies: — that is
// invalid YAML and killed a fresh install's first render outright.
func TestRenderEmptyPlumbingStaysValid(t *testing.T) {
	srcs := []ProviderSource{
		{Name: "flat", URL: "https://example.com/x", Split: true, InlineYAML: "[]\n"},
	}
	out := Render(DefaultMaster, srcs, Settings{MixedPort: 7897, ControllerPort: 9097})
	for _, ln := range strings.Split(out, "\n") {
		if strings.TrimSpace(ln) == "[]" && strings.HasPrefix(ln, " ") {
			t.Fatalf("bare [] spliced into output:\n%s", out)
		}
	}
}

func TestRenderKeepsUserRulesUntouched(t *testing.T) {
	master := strings.Replace(DefaultMaster, "GEOIP,CN,DIRECT", "GEOIP,CN,DIRECT\n  - DOMAIN-SUFFIX,example.org,DIRECT", 1)
	out := Render(master, nil, Settings{})
	if !strings.Contains(out, "DOMAIN-SUFFIX,example.org,DIRECT") {
		t.Error("user rule lost in render")
	}
}

func TestRulesRoundTrip(t *testing.T) {
	rules := ExtractRules(DefaultMaster)
	// The default ruleset is the user's Shadowrocket design ported
	// (2026-11-12): local customs plus RULE-SET entries over the
	// rule-providers in the template. Pin its shape, not every line.
	if len(rules) != 124 {
		t.Fatalf("expected 124 default rules, got %d: %v", len(rules), rules)
	}
	for _, want := range []string{
		"RULE-SET,advertising-domain,REJECT",
		"RULE-SET,china-domain,DIRECT",
		"DOMAIN-SUFFIX,music.apple.com,DIRECT",
		"DOMAIN-SUFFIX,tv.apple.com,节点选择",
	} {
		found := false
		for _, r := range rules {
			if r == want {
				found = true
			}
		}
		if !found {
			t.Errorf("default rules missing %q", want)
		}
	}
	if rules[len(rules)-1] != "MATCH,节点选择" {
		t.Errorf("last default rule should be the final match, got %q", rules[len(rules)-1])
	}
	edited := append(rules, "DOMAIN-SUFFIX,openai.com,节点选择")
	out := ReplaceRules(DefaultMaster, edited)
	got := ExtractRules(out)
	if len(got) != 125 || got[124] != "DOMAIN-SUFFIX,openai.com,节点选择" {
		t.Fatalf("round trip failed: %v", got)
	}
	if !strings.Contains(out, "proxy-groups:") {
		t.Error("replace rules damaged the rest of the file")
	}
}

func TestProviderFiltersExcludeNotices(t *testing.T) {
	out := Render(DefaultMaster, []ProviderSource{{Name: "ky", URL: "https://example.com/sub"}}, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "exclude-type: \"Direct|Reject\"") {
		t.Error("provider block missing exclude-type")
	}
	if !strings.Contains(out, "exclude-filter:") || !strings.Contains(out, "订阅链接") {
		t.Error("provider block missing notice-name exclude-filter")
	}
}

func TestSplitProviderRendersAsProvider(t *testing.T) {
	src := ProviderSource{
		Name: "iplc", URL: "https://example.com/iplc", Split: true,
		InlineYAML: "- name: __p1\n  type: anytls\n",
	}
	out := Render(DefaultMaster, []ProviderSource{src}, Settings{MixedPort: 7897, ControllerPort: 9097})
	// A flattened subscription renders as a file provider: the
	// app maintains the flattened file, and a core-side http pull
	// would overwrite it with the raw full-config payload.
	if !strings.Contains(out, "type: file") {
		t.Error("split subscription must render as a file provider")
	}
	if strings.Contains(out, "url: \"https://example.com/iplc\"") {
		t.Error("split subscription must not carry a pull url")
	}
	if !strings.Contains(out, "use: [iplc]") {
		t.Error("split subscription missing from the use-all groups")
	}
	// Its dialer plumbing is spliced into the main proxies section,
	// where the flattened nodes' chains can resolve it.
	if !strings.Contains(out, "\nproxies:\n  - name: __p1") {
		t.Error("split plumbing not inlined into the main proxies section")
	}
	// A plain provider's payload never leaks into the main section.
	plain := Render(DefaultMaster, []ProviderSource{{Name: "p", URL: "https://example.com/p"}}, Settings{MixedPort: 7897, ControllerPort: 9097})
	if strings.Contains(plain, "__p1") {
		t.Error("plain render must not contain split plumbing")
	}
}

func TestTunDisabledByDefault(t *testing.T) {
	in := "mixed-port: 7890\ntun:\n  enable: true\n  stack: system\nrules:\n  - MATCH,DIRECT\n"
	out := forceRuntimeKeys(in, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "enable: false") || strings.Contains(out, "enable: true") {
		t.Errorf("tun not disabled:\n%s", out)
	}
	if !strings.Contains(out, "stack: system") {
		t.Error("tun sibling keys must survive")
	}
}

func TestTunEnabledInjectsBlock(t *testing.T) {
	in := "mixed-port: 7890\ndns:\n  enable: true\nrules:\n  - MATCH,DIRECT\n"
	out := forceRuntimeKeys(in, Settings{MixedPort: 7897, ControllerPort: 9097, TUNEnabled: true})
	for _, want := range []string{"tun:", "enable: true", "stack: gvisor", "auto-route: true", "mtu: 1500", "dns-hijack:", "any:53", "listen: 0.0.0.0:1053"} {
		if !strings.Contains(out, want) {
			t.Errorf("rendered config missing %q:\n%s", want, out)
		}
	}
}

func TestTunEnabledKeepsMasterBlock(t *testing.T) {
	in := "mixed-port: 7890\ntun:\n  enable: false\n  stack: system\n  mtu: 1400\nrules:\n  - MATCH,DIRECT\n"
	out := forceRuntimeKeys(in, Settings{MixedPort: 7897, ControllerPort: 9097, TUNEnabled: true})
	if !strings.Contains(out, "enable: true") || !strings.Contains(out, "stack: system") || !strings.Contains(out, "mtu: 1400") {
		t.Errorf("master tun block not preserved/enabled:\n%s", out)
	}
	if !strings.Contains(out, "dns-hijack:") {
		t.Errorf("missing keys not filled in:\n%s", out)
	}
}

func TestIPv6DefaultsOffWhenMasterSilent(t *testing.T) {
	in := "mixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"
	out := forceRuntimeKeys(in, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "ipv6: false") {
		t.Errorf("silent master must render ipv6 off:\n%s", out)
	}
}

func TestIPv6MasterKeyRespected(t *testing.T) {
	in := "ipv6: true\nmixed-port: 7890\nrules:\n  - MATCH,DIRECT\n"
	out := forceRuntimeKeys(in, Settings{MixedPort: 7897, ControllerPort: 9097})
	if !strings.Contains(out, "ipv6: true") || strings.Contains(out, "ipv6: false") {
		t.Errorf("explicit master ipv6 key must survive untouched:\n%s", out)
	}
}

func TestRenderPinsSubscriptionHosts(t *testing.T) {
	out := Render(DefaultMaster, []ProviderSource{{Name: "ky", URL: "https://sub.example.com/feed?token=x"}}, Settings{MixedPort: 7897, ControllerPort: 9097})
	// The pin rule sits at the head of the rule table.
	ri := strings.Index(out, "\nrules:")
	pi := strings.Index(out, "- DOMAIN,sub.example.com,DIRECT")
	if ri < 0 || pi < 0 || pi < ri {
		t.Errorf("subscription host not pinned DIRECT at rules head:\n%s", out)
	}
	// And its DNS resolves through the domestic DoH pair.
	if !strings.Contains(out, "\"sub.example.com\":") {
		t.Error("subscription host missing from nameserver-policy")
	}
}
