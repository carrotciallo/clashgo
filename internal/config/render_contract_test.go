package config

import (
	"gopkg.in/yaml.v3"
	"strings"
	"testing"
)

func renderedMap(t *testing.T, text string, sources []ProviderSource, st Settings) map[string]any {
	t.Helper()
	out, err := RenderChecked(text, sources, st)
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := yaml.Unmarshal([]byte(out), &got); err != nil {
		t.Fatal(err)
	}
	return got
}

func TestRenderFlowYAMLContract(t *testing.T) {
	master := `{ipv6: true, tun: {enable: false, mtu: 9000, stack: gvisor}, dns: {enable: false, listen: ""}, rules: ["DOMAIN,sub.example,REJECT", "MATCH,DIRECT"], proxy-providers: {old: {type: file}}, proxy-groups: [{name: 节点选择, type: select}]}`
	secret := "fixture: #\nnot-a-key: true"
	got := renderedMap(t, master, []ProviderSource{{Name: "local", Local: true, URL: "https://sub.example/feed"}}, Settings{MixedPort: 12345, ControllerPort: 12346, ControllerSecret: secret, TUNEnabled: true})
	if got["secret"] != secret || got["ipv6"] != true || got["mixed-port"] != 12345 {
		t.Fatal("runtime scalar contract failed")
	}
	tun := got["tun"].(map[string]any)
	if tun["mtu"] != 1500 || tun["enable"] != true {
		t.Fatal("TUN contract failed")
	}
	dns := got["dns"].(map[string]any)
	if dns["enable"] != true || dns["listen"] != "127.0.0.1:1053" {
		t.Fatal("DNS contract failed")
	}
	rules := got["rules"].([]any)
	if rules[0] != "DOMAIN,sub.example,DIRECT" || rules[1] != "DOMAIN,sub.example,REJECT" {
		t.Fatal("pin precedence or user rules lost")
	}
	pool := got["proxy-providers"].(map[string]any)
	if len(pool) != 1 || pool["local"].(map[string]any)["type"] != "file" {
		t.Fatal("local provider contract failed")
	}
	if !strings.Contains(master, "mtu: 9000") {
		t.Fatal("master changed")
	}
}

func TestRenderRejectsInvalidStructures(t *testing.T) {
	for _, text := range []string{"a: [", "a: 1\na: 2", "a: 1\n---\nb: 2", "[]", "tun: true", "dns: []", "rules: wrong"} {
		if _, err := RenderChecked(text, []ProviderSource{{Name: "x", URL: "https://sub.example"}}, Settings{TUNEnabled: true}); err == nil {
			t.Errorf("accepted %q", text)
		}
	}
}

func TestRenderCreatesMissingPinsAndPreservesExplicitDNS(t *testing.T) {
	got := renderedMap(t, "dns: {listen: '127.0.0.1:23456', nameserver-policy: {sub.example: [8.8.8.8]}}", []ProviderSource{{Name: "x", Managed: true, URL: "https://sub.example"}}, Settings{TUNEnabled: true})
	dns := got["dns"].(map[string]any)
	if dns["listen"] != "127.0.0.1:23456" {
		t.Fatal("explicit listen overwritten")
	}
	if got["rules"].([]any)[0] != "DOMAIN,sub.example,DIRECT" {
		t.Fatal("missing rules not created")
	}
	ns := dns["nameserver-policy"].(map[string]any)["sub.example"].([]any)
	if !strings.HasSuffix(ns[0].(string), "#DIRECT") {
		t.Fatal("resolver not pinned direct")
	}
	if _, ok := got["proxy-providers"].(map[string]any)["x"].(map[string]any)["url"]; ok {
		t.Fatal("core can bypass app validation")
	}
}

func TestRenderResolvesAliasesBeforeForcingRuntimeKeys(t *testing.T) {
	got := renderedMap(t, "base: &base {mtu: 9000, enable: true}\ntun: {<<: *base}\nipv6: false", nil, Settings{})
	tun := got["tun"].(map[string]any)
	if tun["mtu"] != 1500 || tun["enable"] != false {
		t.Fatal("merge defeated runtime settings")
	}
	if got["base"].(map[string]any)["mtu"] != 9000 {
		t.Fatal("alias target changed")
	}
}
