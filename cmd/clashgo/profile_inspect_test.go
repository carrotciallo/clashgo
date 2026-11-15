package main

import (
	"testing"

	"github.com/carrotciallo/clashgo/internal/config"
)

func TestInspectDefaultMaster(t *testing.T) {
	providers := ruleProvidersOf(config.DefaultMaster)
	if len(providers) != 24 {
		t.Errorf("expected 24 rule providers in the template, got %d", len(providers))
	}
	for _, rp := range providers {
		if rp.Name == "" || rp.URL == "" {
			t.Errorf("provider parsed without name or URL: %+v", rp)
		}
	}
	hosts := hostsOf(config.DefaultMaster)
	if len(hosts) == 0 {
		t.Error("template nameserver-policy parsed as empty")
	}
	for _, kv := range hosts {
		if kv.Key == "" || kv.Val == "" {
			t.Errorf("host entry parsed wrong: %+v", kv)
		}
		if kv.Key == `geosite:cn` && kv.Val == "" {
			t.Error("quoted colon key mis-split")
		}
	}
	if len(generalOf(config.DefaultMaster)) == 0 {
		t.Error("general section parsed as empty")
	}
}

func TestEvalRules(t *testing.T) {
	rules := []string{
		"DOMAIN,api.example.com,DIRECT",
		"DOMAIN-SUFFIX,google.com,节点选择",
		"DOMAIN-KEYWORD,tube,节点选择",
		"DOMAIN-WILDCARD,*.cdn-?.net,REJECT",
		"GEOSITE,cn,DIRECT",
		"MATCH,节点选择",
	}
	cases := []struct {
		host string
		want int
	}{
		{"api.example.com", 0},
		{"www.google.com", 1},
		{"google.com", 1},
		{"mytube.org", 2},
		{"a.cdn-1.net", 3},
		{"anything.else", 5}, // GEOSITE is not offline-decidable; MATCH catches
	}
	for _, tc := range cases {
		idx, _, ok := evalRules(rules, tc.host)
		if !ok || idx != tc.want {
			t.Errorf("evalRules(%q) = %d, %v; want %d", tc.host, idx, ok, tc.want)
		}
	}
	if got := hostFromInput("https://User@WWW.Google.com:8443/path?q=1"); got != "www.google.com" {
		t.Errorf("hostFromInput = %q", got)
	}
}

func TestRulePolicy(t *testing.T) {
	head, policy := rulePolicy("DOMAIN-SUFFIX,google.com,PROXY,no-resolve")
	if head != "DOMAIN-SUFFIX,google.com" || policy != "PROXY" {
		t.Errorf("no-resolve rule split wrong: %q / %q", head, policy)
	}
	head, policy = rulePolicy("MATCH,节点选择")
	if head != "MATCH" || policy != "节点选择" {
		t.Errorf("MATCH split wrong: %q / %q", head, policy)
	}
}
