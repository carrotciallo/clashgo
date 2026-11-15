package config

import (
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Format adaptation: import a Shadowrocket .conf (or a bare Surge
// rule fragment) and get a working ClashGo master config. The
// converter is deliberately mechanical — every line it cannot carry
// is listed in the report, never silently dropped:
//
//   - USER-AGENT rules have no mihomo equivalent: dropped, listed.
//   - REJECT-NO-DROP / REJECT-TINYGIF become REJECT: noted.
//   - Unknown policy names map to 节点选择: noted per name.
//   - [General] is not carried (ClashGo's DNS/tun tuning is its
//     own): noted once. [Host] IS carried: server:system entries
//     become nameserver-policy over the domestic DoH pair, plain
//     host mappings become the hosts: table.
//   - RULE-SET / DOMAIN-SET URLs become rule-providers pointing at
//     the ORIGINAL .list files. Probed live 2026-11-12: mihomo
//     reads Surge text lists natively in both behaviors — domain
//     sets with leading-dot lines load fine (China_Domain 3689
//     entries), and a classical read of AWAvenue's Surge list
//     yields its full 976 rules with exact-match semantics intact,
//     which the YAML domain packaging actually widens. Referencing
//     the originals is both simpler and more faithful than swapping
//     to a repackaged twin.

type ImportReport struct {
	Rules     int
	Providers int
	Dropped   []string
	Notes     []string
}

type providerDef struct {
	name     string
	behavior string // classical | domain
	format   string // text | yaml
	url      string
}

// privateHead is prepended to every imported ruleset: the desktop
// TUN client needs private ranges decided before any remote set
// gets a say (the phone config leans on skip-proxy for this, which
// has no equivalent here). Same six lines as the default template.
var privateHead = []string{
	"DOMAIN-SUFFIX,local,DIRECT",
	"IP-CIDR,127.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,10.0.0.0/8,DIRECT,no-resolve",
	"IP-CIDR,172.16.0.0/12,DIRECT,no-resolve",
	"IP-CIDR,192.168.0.0/16,DIRECT,no-resolve",
	"IP-CIDR,100.64.0.0/10,DIRECT,no-resolve",
}

var directPolicies = map[string]bool{"DIRECT": true, "REJECT": true, "REJECT-DROP": true}

var knownRuleTypes = map[string]bool{
	"DOMAIN": true, "DOMAIN-SUFFIX": true, "DOMAIN-KEYWORD": true,
	"DOMAIN-WILDCARD": true, "IP-CIDR": true, "IP-CIDR6": true,
	"GEOIP": true, "GEOSITE": true, "DST-PORT": true, "SRC-PORT": true,
	"SRC-IP-CIDR": true, "IN-PORT": true, "PROTOCOL": false, // handled by logic translation only
}

// TranslateShadowrocket converts a Shadowrocket config (or bare
// rule list) into a complete ClashGo master config based on the
// default template skeleton, plus a report of everything that was
// dropped or approximated.
func TranslateShadowrocket(conf string) (string, ImportReport, error) {
	var rep ImportReport
	sections := splitSections(conf)

	tr := &translator{
		providers: map[string]providerDef{},
		byURL:     map[string]string{},
		policies:  map[string]bool{},
	}

	var hostPolicy []string // nameserver-policy YAML lines
	var hosts []string      // hosts: entries
	generalKeys := 0
	otherSections := map[string]bool{}

	for _, sec := range sections {
		switch sec.name {
		case "rule":
			for _, ln := range sec.lines {
				tr.rule(ln, &rep)
			}
		case "host":
			for _, ln := range sec.lines {
				k, v, ok := splitKV(ln)
				if !ok {
					continue
				}
				if strings.HasPrefix(v, "server:") {
					srv := strings.TrimPrefix(v, "server:")
					var servers []string
					if srv == "system" {
						servers = []string{"https://dns.alidns.com/dns-query", "https://doh.pub/dns-query"}
					} else {
						servers = []string{srv}
					}
					key := "+." + strings.TrimPrefix(k, "*.")
					hostPolicy = append(hostPolicy, fmt.Sprintf("    %q:", key))
					for _, s := range servers {
						hostPolicy = append(hostPolicy, fmt.Sprintf("      - %q", s))
					}
				} else {
					hosts = append(hosts, fmt.Sprintf("  %q: %q", k, v))
				}
			}
		case "general":
			for _, ln := range sec.lines {
				if k, _, ok := splitKV(ln); ok {
					generalKeys++
					if k == "dns-server" {
						rep.Notes = append(rep.Notes, "[General] 的 dns-server 未搬：ClashGo 用自己的 DNS 调优")
					}
				}
			}
		case "":
			// Preamble before any header: comments only, ignore.
		default:
			otherSections[sec.name] = true
		}
	}
	if generalKeys > 0 {
		rep.Notes = append(rep.Notes, "[General] 段其余设置未搬（ClashGo 有自己的通用设置）")
	}
	for name := range otherSections {
		rep.Notes = append(rep.Notes, fmt.Sprintf("[%s] 段未搬", name))
	}
	if len(tr.rules) == 0 {
		return "", rep, fmt.Errorf("没有解析出任何规则，这不像一份小火箭配置")
	}
	if !tr.hasFinal {
		tr.rules = append(tr.rules, "MATCH,节点选择")
		rep.Notes = append(rep.Notes, "源配置没有 FINAL，已补 MATCH,节点选择 兜底")
	}
	rep.Rules = len(tr.rules) + len(privateHead)
	rep.Providers = len(tr.order)
	rep.Notes = append(rep.Notes, "已在规则最前面补桌面端私有地址直连头（6 条）")

	master := DefaultMaster
	master = strings.Replace(master,
		"# ClashGo master config — this file is yours.",
		"# ClashGo master config — imported from a Shadowrocket config (format adaptation).",
		1)
	// The skeleton's own rule-providers belong to the default
	// ruleset; the import replaces them wholesale with the source
	// config's sets (a second block would be a duplicate YAML key —
	// the first imported profile failed validation on exactly that).
	master = stripRuleProviders(master)
	if len(hostPolicy) > 0 {
		master = insertHostPolicy(master, hostPolicy)
	}
	if len(hosts) > 0 {
		block := "hosts:\n" + strings.Join(hosts, "\n") + "\n\n"
		master = strings.Replace(master, "sniffer:", block+"sniffer:", 1)
	}
	if len(tr.order) > 0 {
		var b strings.Builder
		b.WriteString("# Rule providers generated by the Shadowrocket import:\n")
		b.WriteString("# the original .list URLs, which mihomo reads natively.\n")
		b.WriteString("rule-providers:\n")
		for _, name := range tr.order {
			p := tr.providers[name]
			ext := "list"
			if p.format == "yaml" {
				ext = "yaml"
			}
			fmt.Fprintf(&b, "  %s:\n    type: http\n    behavior: %s\n    format: %s\n    url: %q\n    path: ./rules/%s.%s\n    interval: 86400\n",
				p.name, p.behavior, p.format, p.url, p.name, ext)
		}
		master = strings.Replace(master, "rules:", b.String()+"\nrules:", 1)
	}
	all := append(append([]string{}, privateHead...), tr.rules...)
	master = ReplaceRules(master, all)
	return master, rep, nil
}

type section struct {
	name  string
	lines []string
}

// splitSections groups lines by [Header]; lines before the first
// header (or in a header-less fragment) land in the "rule" section
// when they look like rules — that is what makes a bare Surge rule
// fragment importable.
func splitSections(text string) []section {
	var out []section
	cur := section{name: ""}
	for _, raw := range strings.Split(text, "\n") {
		ln := strings.TrimSpace(raw)
		if strings.HasPrefix(ln, "[") && strings.HasSuffix(ln, "]") {
			name := strings.ToLower(strings.TrimSpace(ln[1 : len(ln)-1]))
			out = append(out, section{name: name})
			cur = out[len(out)-1]
			continue
		}
		if len(out) == 0 {
			out = append(out, section{name: ""})
			cur = out[0]
		}
		if cur.name == "" && looksLikeRule(ln) {
			// Fragment mode: promote the preamble to a rule section.
			out[len(out)-1].name = "rule"
			cur = out[len(out)-1]
		}
		out[len(out)-1].lines = append(out[len(out)-1].lines, ln)
	}
	return out
}

func looksLikeRule(ln string) bool {
	if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "//") {
		return false
	}
	head := ln
	if i := strings.Index(ln, ","); i >= 0 {
		head = ln[:i]
	}
	switch strings.ToUpper(head) {
	case "DOMAIN", "DOMAIN-SUFFIX", "DOMAIN-KEYWORD", "RULE-SET", "DOMAIN-SET",
		"IP-CIDR", "GEOIP", "FINAL", "AND", "OR", "NOT", "USER-AGENT":
		return true
	}
	return false
}

func splitKV(ln string) (string, string, bool) {
	i := strings.Index(ln, "=")
	if i < 0 {
		return "", "", false
	}
	k := strings.TrimSpace(ln[:i])
	v := strings.TrimSpace(ln[i+1:])
	if k == "" || v == "" {
		return "", "", false
	}
	return k, v, true
}

// stripRuleProviders removes the rule-providers: block (the
// default skeleton's sets) up to the rules: key.
func stripRuleProviders(master string) string {
	lines := strings.Split(master, "\n")
	start, end := -1, -1
	for i, ln := range lines {
		if ln == "rule-providers:" {
			start = i
		}
		if start >= 0 && ln == "rules:" {
			end = i
			break
		}
	}
	if start < 0 || end < 0 {
		return master
	}
	out := append([]string{}, lines[:start]...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

// Imported host policies replace matching template keys rather than creating
// duplicate YAML keys. Nodes preserve the template's comments and quote styles.
func insertHostPolicy(master string, entries []string) string {
	var doc, imported yaml.Node
	if yaml.Unmarshal([]byte(master), &doc) != nil || yaml.Unmarshal([]byte(strings.Join(entries, "\n")), &imported) != nil {
		return master
	}
	var merge func(*yaml.Node)
	merge = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				if n.Content[i].Value != "nameserver-policy" {
					continue
				}
				policy := n.Content[i+1]
				for j := 0; j+1 < len(imported.Content[0].Content); j += 2 {
					key, value := imported.Content[0].Content[j], imported.Content[0].Content[j+1]
					found := false
					for k := 0; k+1 < len(policy.Content); k += 2 {
						if policy.Content[k].Value == key.Value {
							policy.Content[k+1] = value
							found = true
							break
						}
					}
					if !found {
						policy.Content = append(policy.Content, key, value)
					}
				}
				return
			}
		}
		for _, child := range n.Content {
			merge(child)
		}
	}
	merge(&doc)
	var out strings.Builder
	enc := yaml.NewEncoder(&out)
	enc.SetIndent(2)
	if enc.Encode(&doc) != nil {
		return master
	}
	_ = enc.Close()
	return out.String()
}

type translator struct {
	rules     []string
	providers map[string]providerDef
	byURL     map[string]string // url+behavior -> name
	order     []string          // provider names in first-use order
	policies  map[string]bool   // unknown policies already noted
	hasFinal  bool
}

func (t *translator) mapPolicy(p string, rep *ImportReport) string {
	switch p {
	case "PROXY":
		return "节点选择"
	case "REJECT-NO-DROP":
		if !t.policies["REJECT-NO-DROP"] {
			t.policies["REJECT-NO-DROP"] = true
			rep.Notes = append(rep.Notes, "REJECT-NO-DROP 在 mihomo 无等价，已降为 REJECT")
		}
		return "REJECT"
	case "REJECT-TINYGIF":
		if !t.policies["REJECT-TINYGIF"] {
			t.policies["REJECT-TINYGIF"] = true
			rep.Notes = append(rep.Notes, "REJECT-TINYGIF 在 mihomo 无等价，已降为 REJECT")
		}
		return "REJECT"
	}
	if directPolicies[p] {
		return p
	}
	if !t.policies[p] {
		t.policies[p] = true
		rep.Notes = append(rep.Notes, fmt.Sprintf("策略 %s 在 ClashGo 没有对应分组，已映射为 节点选择", p))
	}
	return "节点选择"
}

// splitRuleTail splits "HEAD...,policy[,param]" — policy is the
// last field unless the last field is a known parameter.
func splitPolicy(fields []string) (head []string, policy string, params []string) {
	last := fields[len(fields)-1]
	if last == "no-resolve" && len(fields) >= 2 {
		return fields[:len(fields)-2], fields[len(fields)-2], []string{"no-resolve"}
	}
	return fields[:len(fields)-1], last, nil
}

func (t *translator) rule(ln string, rep *ImportReport) {
	if ln == "" || strings.HasPrefix(ln, "#") || strings.HasPrefix(ln, "//") {
		return
	}
	upper := strings.ToUpper(ln)
	// Logic rules: body contains commas inside parens; the policy
	// follows the final closing paren.
	if strings.HasPrefix(upper, "AND,") || strings.HasPrefix(upper, "OR,") || strings.HasPrefix(upper, "NOT,") {
		i := strings.LastIndex(ln, "),")
		if i < 0 {
			rep.Dropped = append(rep.Dropped, ln+"（逻辑规则格式无法解析）")
			return
		}
		body := ln[:i+1]
		policy := strings.TrimSpace(ln[i+2:])
		body = strings.ReplaceAll(body, "PROTOCOL,", "NETWORK,")
		body = strings.ReplaceAll(body, "DEST-PORT", "DST-PORT")
		t.rules = append(t.rules, body+","+t.mapPolicy(policy, rep))
		return
	}
	fields := strings.Split(ln, ",")
	for i := range fields {
		fields[i] = strings.TrimSpace(fields[i])
	}
	typ := strings.ToUpper(fields[0])
	switch typ {
	case "USER-AGENT":
		rep.Dropped = append(rep.Dropped, ln+"（USER-AGENT 在 mihomo 无等价规则）")
		return
	case "FINAL":
		if len(fields) >= 2 {
			t.hasFinal = true
			t.rules = append(t.rules, "MATCH,"+t.mapPolicy(fields[1], rep))
		}
		return
	case "RULE-SET", "DOMAIN-SET":
		if len(fields) < 3 {
			rep.Dropped = append(rep.Dropped, ln+"（规则集缺少地址或策略）")
			return
		}
		behavior := "classical"
		if typ == "DOMAIN-SET" {
			behavior = "domain"
		}
		name := t.provider(fields[1], behavior)
		t.rules = append(t.rules, "RULE-SET,"+name+","+t.mapPolicy(fields[2], rep))
		return
	}
	if typ == "PROTOCOL" && len(fields) >= 3 {
		// Standalone protocol rule: mihomo calls it NETWORK.
		head, policy, params := splitPolicy(fields)
		out := append([]string{"NETWORK"}, head[1:]...)
		out = append(out, t.mapPolicy(policy, rep))
		out = append(out, params...)
		t.rules = append(t.rules, strings.Join(out, ","))
		return
	}
	if !knownRuleTypes[typ] {
		rep.Dropped = append(rep.Dropped, ln+"（规则类型 "+typ+" 不支持）")
		return
	}
	if len(fields) < 3 || fields[1] == "" || fields[2] == "" || (fields[len(fields)-1] == "no-resolve" && len(fields) < 4) {
		rep.Dropped = append(rep.Dropped, ln+"（规则缺少匹配内容或策略）")
		return
	}
	head, policy, params := splitPolicy(fields)
	out := append([]string{typ}, head[1:]...)
	out = append(out, t.mapPolicy(policy, rep))
	out = append(out, params...)
	t.rules = append(t.rules, strings.Join(out, ","))
}

// provider registers (or reuses) a rule-provider for a set URL and
// returns its generated name.
func (t *translator) provider(url, behavior string) string {
	key := url + "|" + behavior
	if name, ok := t.byURL[key]; ok {
		return name
	}
	base := url
	if i := strings.Index(base, "?"); i >= 0 {
		base = base[:i]
	}
	if i := strings.LastIndex(base, "/"); i >= 0 {
		base = base[i+1:]
	}
	format := "text"
	for _, ext := range []string{".list", ".txt", ".yaml", ".yml"} {
		if strings.HasSuffix(strings.ToLower(base), ext) {
			base = base[:len(base)-len(ext)]
			if ext == ".yaml" || ext == ".yml" {
				format = "yaml"
			}
			break
		}
	}
	name := strings.ToLower(base)
	if strings.HasSuffix(name, "_domain") {
		name = strings.TrimSuffix(name, "_domain") + "-domain"
	}
	var sb strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			sb.WriteRune(r)
		} else {
			sb.WriteRune('-')
		}
	}
	name = strings.Trim(sb.String(), "-")
	// Drop filler tokens from long export filenames
	// ("AWAvenue-Ads-Rule-Surge-RULE-SET" -> "awavenue-ads") so
	// generated names stay readable in the rules list.
	var kept []string
	for _, tok := range strings.Split(name, "-") {
		switch tok {
		case "surge", "rule", "set", "rules", "list":
		default:
			kept = append(kept, tok)
		}
	}
	if len(kept) > 0 {
		name = strings.Join(kept, "-")
	}
	if name == "" {
		name = "set"
	}
	candidate := name
	for n := 2; ; n++ {
		if _, taken := t.providers[candidate]; !taken {
			break
		}
		candidate = fmt.Sprintf("%s-%d", name, n)
	}
	t.providers[candidate] = providerDef{name: candidate, behavior: behavior, format: format, url: url}
	t.byURL[key] = candidate
	t.order = append(t.order, candidate)
	return candidate
}

// SortedDropped returns the report's dropped lines in a stable
// order for display (the UI shows them grouped, tests compare
// sets).
func (r ImportReport) SortedDropped() []string {
	out := append([]string{}, r.Dropped...)
	sort.Strings(out)
	return out
}
