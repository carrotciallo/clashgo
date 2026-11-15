package main

import (
	"os"
	"regexp"
	"strings"
)

// Read-only inspection of a profile file's text for the config
// page's Shadowrocket-style detail view: section counts, the
// rule-provider list, the nameserver-policy (Hosts) entries and
// the plain general settings. The master format is line-oriented
// YAML that ClashGo itself generates and the user edits as text,
// so small line parsers beat a full YAML round-trip here (which
// would also eat the comments on any write-back).

type kvPair struct{ Key, Val string }

type ruleProviderInfo struct {
	Name, URL string
}

// profileContent reads any profile's raw text (the active profile
// is the master file itself; others sit beside it).
func (a *App) profileContent(name string) (string, error) {
	b, err := os.ReadFile(a.store.ProfilePath(name))
	return string(b), err
}

// saveProfileContent writes a profile back; the active one goes
// through SaveMaster, others are plain file writes.
func (a *App) saveProfileContent(name, content string) error {
	return os.WriteFile(a.store.ProfilePath(name), []byte(content), 0o644)
}

func indentOf(line string) int {
	n := 0
	for n < len(line) && line[n] == ' ' {
		n++
	}
	return n
}

// blockLines returns the lines belonging to the mapping block
// whose header line is at lines[start] (the first line after it
// whose indent is <= the header's ends the block).
func blockLines(lines []string, start int) []string {
	base := indentOf(lines[start])
	var out []string
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if strings.TrimSpace(ln) == "" {
			continue
		}
		if indentOf(ln) <= base {
			break
		}
		out = append(out, ln)
	}
	return out
}

func findBlock(lines []string, header string) (int, bool) {
	for i, ln := range lines {
		if strings.TrimRight(ln, " ") == header {
			return i, true
		}
	}
	return -1, false
}

// ruleProvidersOf lists the master's rule-providers (name + URL),
// in file order.
func ruleProvidersOf(master string) []ruleProviderInfo {
	lines := strings.Split(master, "\n")
	i, ok := findBlock(lines, "rule-providers:")
	if !ok {
		return nil
	}
	var out []ruleProviderInfo
	for _, ln := range blockLines(lines, i) {
		t := strings.TrimSpace(ln)
		if indentOf(ln) == 2 && strings.HasSuffix(t, ":") && !strings.Contains(t, " ") {
			out = append(out, ruleProviderInfo{Name: strings.TrimSuffix(t, ":")})
			continue
		}
		if strings.HasPrefix(t, "url:") && len(out) > 0 {
			u := strings.TrimSpace(strings.TrimPrefix(t, "url:"))
			out[len(out)-1].URL = strings.Trim(u, `"`)
		}
	}
	return out
}

// hostsOf lists the dns nameserver-policy entries — the Hosts
// section of the master (Shadowrocket's [Host] equivalent).
func hostsOf(master string) []kvPair {
	lines := strings.Split(master, "\n")
	i, ok := findBlock(lines, "  nameserver-policy:")
	if !ok {
		return nil
	}
	block := blockLines(lines, i)
	var out []kvPair
	for j := 0; j < len(block); j++ {
		ln := block[j]
		t := strings.TrimSpace(ln)
		var kv kvPair
		// Keys may be quoted and may themselves contain colons
		// ("geosite:cn": …), so split quote-aware.
		if strings.HasPrefix(t, `"`) {
			end := strings.Index(t[1:], `"`)
			if end < 0 {
				continue
			}
			rest := strings.TrimSpace(t[end+2:])
			kv = kvPair{Key: t[1 : end+1], Val: strings.TrimSpace(strings.TrimPrefix(rest, ":"))}
		} else {
			k, v, found := strings.Cut(t, ":")
			if !found {
				continue
			}
			kv = kvPair{Key: strings.TrimSpace(k), Val: strings.TrimSpace(v)}
		}
		// A value can also be a nested list of servers on the
		// following, deeper-indented lines.
		if kv.Val == "" {
			var servers []string
			for j+1 < len(block) && indentOf(block[j+1]) > indentOf(ln) {
				j++
				if s := strings.TrimSpace(block[j]); strings.HasPrefix(s, "- ") {
					servers = append(servers, strings.TrimSpace(strings.TrimPrefix(s, "- ")))
				}
			}
			kv.Val = strings.Join(servers, ", ")
		}
		out = append(out, kv)
	}
	return out
}

// generalOf lists the plain top-level settings a detail view can
// show as the 通用 section: scalar keys plus a one-line tun
// summary. Order follows the candidate list, not the file.
func generalOf(master string) []kvPair {
	lines := strings.Split(master, "\n")
	vals := map[string]string{}
	for _, ln := range lines {
		if indentOf(ln) != 0 {
			continue
		}
		k, v, found := strings.Cut(ln, ":")
		if !found {
			continue
		}
		vals[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	var tun string
	if i, ok := findBlock(lines, "tun:"); ok {
		var parts []string
		for _, ln := range blockLines(lines, i) {
			t := strings.TrimSpace(ln)
			if strings.HasPrefix(t, "enable:") || strings.HasPrefix(t, "stack:") {
				parts = append(parts, t)
			}
		}
		tun = strings.Join(parts, " · ")
	}
	candidates := []string{
		"mixed-port", "allow-lan", "log-level", "ipv6",
		"external-controller", "unified-delay", "tcp-concurrent",
		"geodata-mode", "find-process-mode", "global-client-fingerprint",
	}
	var out []kvPair
	for _, k := range candidates {
		if v, ok := vals[k]; ok && v != "" {
			out = append(out, kvPair{k, v})
		}
	}
	if tun != "" {
		out = append(out, kvPair{"tun", tun})
	}
	return out
}

var wildcardReplacer = strings.NewReplacer(
	`.`, `\.`, `+`, `\+`, `(`, `\(`, `)`, `\)`, `|`, `\|`,
	`^`, `\^`, `$`, `\$`, `[`, `\[`, `]`, `\]`, `{`, `\{`, `}`, `\}`,
	`*`, `.*`, `?`, `.`,
)

// evalRules walks the rule list in order and reports the first
// rule a host hits, Shadowrocket's 测试规则 in offline form. Only
// the domain-shaped types are decidable without the core; GEOSITE
// / RULE-SET / IP rules are skipped, MATCH always hits.
func evalRules(rules []string, host string) (int, string, bool) {
	host = strings.ToLower(strings.TrimSpace(host))
	for i, r := range rules {
		typ, value, _ := ruleParts(r)
		switch typ {
		case "DOMAIN":
			if host == strings.ToLower(value) {
				return i, r, true
			}
		case "DOMAIN-SUFFIX":
			v := strings.ToLower(value)
			if host == v || strings.HasSuffix(host, "."+v) {
				return i, r, true
			}
		case "DOMAIN-KEYWORD":
			if strings.Contains(host, strings.ToLower(value)) {
				return i, r, true
			}
		case "DOMAIN-WILDCARD":
			re, err := regexp.Compile("^" + wildcardReplacer.Replace(strings.ToLower(value)) + "$")
			if err == nil && re.MatchString(host) {
				return i, r, true
			}
		case "MATCH":
			return i, r, true
		}
	}
	return -1, "", false
}

// rulePolicy splits a rule line into (head, policy) where head is
// everything before the policy token, honouring a trailing
// no-resolve flag.
func rulePolicy(r string) (head, policy string) {
	parts := strings.Split(r, ",")
	if len(parts) < 2 {
		return r, ""
	}
	last := parts[len(parts)-1]
	if last == "no-resolve" && len(parts) >= 3 {
		return strings.Join(parts[:len(parts)-2], ","), parts[len(parts)-2]
	}
	return strings.Join(parts[:len(parts)-1], ","), last
}

// hostFromInput normalises a 测试规则 input: bare host, or a URL
// the user pasted — scheme, path, port and userinfo are stripped.
func hostFromInput(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, ":"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}
