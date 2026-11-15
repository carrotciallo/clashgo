package config

import (
	"fmt"
	"net/url"
	"strings"
)

// Rendering turns the master config into the effective config the core
// runs. Two regions of the master are managed by ClashGo, delimited by
// marker comments; everything else passes through untouched:
//
//	# >>> clashgo:providers ... # <<< clashgo:providers
//	a group line "# clashgo:use-all" makes the next "use:" line list
//	every subscription as a provider source
//
// Runtime-only keys (ports, controller, secret) are forced into the
// rendered copy; the master file itself is never rewritten by rendering.
const (
	providersBegin = "# >>> clashgo:providers (managed — edit subscriptions in ClashGo, not here)"
	providersEnd   = "# <<< clashgo:providers"
	useAllMarker   = "# clashgo:use-all"
)

// Render produces the effective config text.
func Render(master string, providers []ProviderSource, st Settings) string {
	out := substituteProviders(master, providers)
	out = substituteUseAll(out, providers)
	out = inlineProxies(out, providers)
	out = forceRuntimeKeys(out, st)
	out = pinSubscriptionHosts(out, providers)
	return out
}

// pinSubscriptionHosts makes subscription endpoints independent
// of the currently selected node: a rule at the head of the rule
// table sends each subscription host DIRECT, and a nameserver
// policy entry resolves it through the domestic DoH pair instead
// of the proxy-riding main nameserver. Without this, fetching a
// subscription rides whatever node is selected — a degraded node
// then blocks the very refresh that could replace it (measured
// live 2026-11-12: TLS handshakes to the subscription host died
// inside a congested node's session setup while the same host
// answered in ~2s direct). The pins exist only in the rendered
// copy; the master file keeps the user's own rules untouched.
func pinSubscriptionHosts(text string, providers []ProviderSource) string {
	seen := map[string]bool{}
	var hosts []string
	for _, p := range providers {
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		if h := u.Hostname(); h != "" && !seen[h] {
			seen[h] = true
			hosts = append(hosts, h)
		}
	}
	if len(hosts) == 0 {
		return text
	}
	// Rules: insert right under the top-level "rules:" line.
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if ln != "rules:" {
			continue
		}
		var add []string
		for _, h := range hosts {
			if !strings.Contains(text, "DOMAIN,"+h+",") {
				add = append(add, "  - DOMAIN,"+h+",DIRECT")
			}
		}
		if len(add) > 0 {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, add...)
			out = append(out, lines[i+1:]...)
			lines = out
			text = strings.Join(lines, "\n")
		}
		break
	}
	// DNS: add per-host entries under nameserver-policy.
	for i, ln := range lines {
		t := strings.TrimSpace(ln)
		if !strings.HasPrefix(t, "nameserver-policy:") {
			continue
		}
		indent := ln[:len(ln)-len(strings.TrimLeft(ln, " 	"))]
		var add []string
		for _, h := range hosts {
			if strings.Contains(text, "\""+h+"\":") || strings.Contains(text, "'"+h+"':") {
				continue
			}
			add = append(add,
				indent+"  \""+h+"\":",
				indent+"    - https://dns.alidns.com/dns-query",
				indent+"    - https://doh.pub/dns-query")
		}
		if len(add) > 0 {
			out := append([]string{}, lines[:i+1]...)
			out = append(out, add...)
			out = append(out, lines[i+1:]...)
			text = strings.Join(out, "\n")
		}
		break
	}
	return text
}

// inlineProxies splices split subscriptions' dialer plumbing into
// the main proxies: section. This is what makes the flattened
// nodes' chains resolve at all: mihomo registers main-config
// proxies globally, so a provider node's dialer-proxy reference
// finds them — while group membership, which is list-driven, never
// includes them. The plumbing cannot travel inside the provider
// file instead: a provider exclude-filter unregisters what it
// matches, chains included (measured live 2026-11-11).
func inlineProxies(text string, providers []ProviderSource) string {
	var items []string
	for _, p := range providers {
		if !p.Split || strings.TrimSpace(p.InlineYAML) == "" {
			continue
		}
		for _, ln := range strings.Split(strings.TrimSuffix(p.InlineYAML, "\n"), "\n") {
			if strings.TrimSpace(ln) == "" || strings.TrimSpace(ln) == "[]" {
				continue
			}
			items = append(items, "  "+ln)
		}
	}
	if len(items) == 0 {
		return text
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if ln == "proxies:" {
			start = i
			break
		}
	}
	if start < 0 {
		// No proxies section yet: create one just before the
		// proxy-groups section (or at the end of the file).
		at := len(lines)
		for i, ln := range lines {
			if strings.HasPrefix(ln, "proxy-groups:") {
				at = i
				break
			}
		}
		block := append([]string{"proxies:"}, items...)
		out := append([]string{}, lines[:at]...)
		out = append(out, block...)
		out = append(out, lines[at:]...)
		return strings.Join(out, "\n")
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			end = i
			break
		}
	}
	out := append([]string{}, lines[:end]...)
	out = append(out, items...)
	out = append(out, lines[end:]...)
	return strings.Join(out, "\n")
}

func providersBlock(providers []ProviderSource) string {
	var b strings.Builder
	b.WriteString(providersBegin + "\n")
	if len(providers) == 0 {
		b.WriteString("proxy-providers: {}\n")
	} else {
		b.WriteString("proxy-providers:\n")
		for _, p := range providers {
			fmt.Fprintf(&b, "  %s:\n", p.Name)
			// Airports pad their lists with Direct-type info entries
			// (traffic/expiry notices). As url-test candidates they win
			// every delay race and silently route traffic direct —
			// measured live 2026-11-11. Subscriptions contribute nodes,
			// never pseudo outbounds.
			b.WriteString("    exclude-type: \"Direct|Reject\"\n")
			// The same notices also come disguised as real protocols
			// (one provider ships ss entries named like "订阅链接可能已泄露"
			// pointing at 8.8.8.8:12345). Only their names give them
			// away; an url-test group that lands on one blackholes
			// every connection through it — measured live 2026-11-11
			// when it took down the first TUN trial.
			fmt.Fprintf(&b, "    exclude-filter: %q\n", noticeFilter())
			if p.Split {
				// The app fetches and flattens split payloads
				// itself; the provider is the maintained file.
				// A core-side http pull would overwrite the
				// flattened nodes with the raw full-config
				// payload, whose nodes chain through groups
				// that do not exist here.
				b.WriteString("    type: file\n")
			} else {
				b.WriteString("    type: http\n")
				fmt.Fprintf(&b, "    url: %q\n", p.URL)
				b.WriteString("    interval: 86400\n")
			}
			fmt.Fprintf(&b, "    path: ./providers/%s.yaml\n", p.Name)
			b.WriteString("    health-check:\n")
			b.WriteString("      enable: true\n")
			b.WriteString("      url: https://www.gstatic.com/generate_204\n")
			b.WriteString("      interval: 300\n")
		}
	}
	b.WriteString(providersEnd + "\n")
	return b.String()
}

// substituteProviders replaces the marked block, or inserts it before the
// proxy-groups section (appending at the end if there is none).
func substituteProviders(master string, providers []ProviderSource) string {
	block := providersBlock(providers)
	lines := strings.Split(master, "\n")
	start, end := -1, -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "# >>> clashgo:providers") {
			start = i
		}
		if start >= 0 && strings.HasPrefix(ln, "# <<< clashgo:providers") {
			end = i
			break
		}
	}
	blockLines := strings.Split(strings.TrimSuffix(block, "\n"), "\n")
	if start >= 0 && end >= start {
		out := append([]string{}, lines[:start]...)
		out = append(out, blockLines...)
		out = append(out, lines[end+1:]...)
		return strings.Join(out, "\n")
	}
	// Insert before "proxy-groups:" if present, else append.
	for i, ln := range lines {
		if strings.HasPrefix(ln, "proxy-groups:") {
			out := append([]string{}, lines[:i]...)
			out = append(out, blockLines...)
			out = append(out, lines[i:]...)
			return strings.Join(out, "\n")
		}
	}
	return strings.TrimSuffix(master, "\n") + "\n" + block
}

// substituteUseAll rewrites the "use:" line following each use-all
// marker so the group draws from every subscription.
func substituteUseAll(master string, providers []ProviderSource) string {
	names := make([]string, 0, len(providers))
	for _, p := range providers {
		names = append(names, p.Name)
	}
	useLine := "use: [" + strings.Join(names, ", ") + "]"
	lines := strings.Split(master, "\n")
	for i, ln := range lines {
		if !strings.Contains(ln, useAllMarker) {
			continue
		}
		indent := ln[:len(ln)-len(strings.TrimLeft(ln, " \t"))]
		if i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "use:") {
			lines[i+1] = indent + useLine
		} else {
			lines = append(lines, "")
			copy(lines[i+2:], lines[i+1:])
			lines[i+1] = indent + useLine
		}
	}
	return strings.Join(lines, "\n")
}

// forceRuntimeKeys sets the scalar top-level keys the app controls in
// the rendered copy, and renders the tun: block per settings.
func forceRuntimeKeys(text string, st Settings) string {
	keys := []struct{ k, v string }{
		{"mixed-port", fmt.Sprint(st.MixedPort)},
		{"external-controller", fmt.Sprintf("127.0.0.1:%d", st.ControllerPort)},
		{"secret", st.ControllerSecret},
		{"allow-lan", "false"},
		{"mode", "rule"},
	}
	for _, kv := range keys {
		text = setTopScalar(text, kv.k, kv.v)
	}
	text = ensureIPv6Default(text)
	return applyTun(text, st.TUNEnabled)
}

// ensureIPv6Default renders "ipv6: false" when the master never
// mentions the key. Left alone, mihomo brings the TUN up with an IPv6
// address (fdfe:dcba:9876::/126) and accepts IPv6 flows even on hosts
// with no IPv6 egress at all; each such dial then fails slowly inside
// the core ("network is unreachable") and programs with lazy v6-to-v4
// fallback — UU's accelerator node handshake is one, 2026-11-13 —
// time out instead of falling back. An explicit ipv6: key in the
// master is the user's word and stays untouched.
func ensureIPv6Default(text string) string {
	for _, ln := range strings.Split(text, "\n") {
		if strings.HasPrefix(ln, "ipv6:") {
			return text
		}
	}
	return "ipv6: false\n" + text
}

// setTopScalar replaces a "^key:" line or prepends it.
func setTopScalar(text, key, value string) string {
	lines := strings.Split(text, "\n")
	for i, ln := range lines {
		if strings.HasPrefix(ln, key+":") {
			lines[i] = key + ": " + value
			return strings.Join(lines, "\n")
		}
	}
	return key + ": " + value + "\n" + text
}

// applyTun renders the top-level tun: block according to the setting.
// The master's own block keeps its extra keys; enable and the keys TUN
// needs to work are forced. With no block present, enabling injects a
// standard one and disabling is a no-op. Enabling also ensures the DNS
// server listens somewhere: dns-hijack redirects port 53 into it, and
// with no listen address the hijack has nowhere to go.
func applyTun(text string, enabled bool) string {
	if enabled {
		text = ensureDNSListen(text)
	}
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "tun:") {
			start = i
			break
		}
	}
	if start < 0 {
		if !enabled {
			return text
		}
		// gvisor, not mixed/system: the system-stack paths blackholed
		// every tunneled TCP connection on the target machine while
		// gvisor works end to end — bisected live 2026-11-11.
		// mtu 1500, not mihomo's 9000 default: the physical link is
		// 1500, and the jumbo TUN MTU blackholed bulk flows of other
		// TUN-sensitive software (UU accelerator, 2026-11-13).
		block := "tun:\n  enable: true\n  stack: gvisor\n  auto-route: true\n" +
			"  auto-detect-interface: true\n  mtu: 1500\n  dns-hijack:\n    - any:53\n"
		return block + text
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if ln := lines[i]; ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			end = i
			break
		}
	}
	enableVal := "false"
	if enabled {
		enableVal = "true"
	}
	have := map[string]bool{}
	for i := start + 1; i < end; i++ {
		t := strings.TrimSpace(lines[i])
		for _, k := range []string{"enable", "stack", "auto-route", "auto-detect-interface", "mtu", "dns-hijack"} {
			if strings.HasPrefix(t, k+":") {
				have[k] = true
			}
		}
		if strings.HasPrefix(t, "enable:") {
			indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
			lines[i] = indent + "enable: " + enableVal
		}
	}
	if enabled {
		var inject []string
		if !have["enable"] {
			inject = append(inject, "  enable: true")
		}
		if !have["stack"] {
			inject = append(inject, "  stack: gvisor")
		}
		if !have["auto-route"] {
			inject = append(inject, "  auto-route: true")
		}
		if !have["auto-detect-interface"] {
			inject = append(inject, "  auto-detect-interface: true")
		}
		if !have["mtu"] {
			inject = append(inject, "  mtu: 1500")
		}
		if !have["dns-hijack"] {
			inject = append(inject, "  dns-hijack:", "    - any:53")
		}
		if len(inject) > 0 {
			out := append([]string{}, lines[:start+1]...)
			out = append(out, inject...)
			out = append(out, lines[start+1:]...)
			lines = out
		}
	}
	return strings.Join(lines, "\n")
}

// ensureDNSListen adds "listen: 0.0.0.0:1053" to the top-level dns:
// block when it has none.
func ensureDNSListen(text string) string {
	lines := strings.Split(text, "\n")
	start := -1
	for i, ln := range lines {
		if strings.HasPrefix(ln, "dns:") {
			start = i
			break
		}
	}
	if start < 0 {
		return "dns:\n  enable: true\n  listen: 0.0.0.0:1053\n" + text
	}
	for i := start + 1; i < len(lines); i++ {
		ln := lines[i]
		if ln != "" && !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			break
		}
		if strings.HasPrefix(strings.TrimSpace(ln), "listen:") {
			return text
		}
	}
	out := append([]string{}, lines[:start+1]...)
	out = append(out, "  listen: 0.0.0.0:1053")
	out = append(out, lines[start+1:]...)
	return strings.Join(out, "\n")
}
