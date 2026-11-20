package config

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"net/url"
	"strings"

	"gopkg.in/yaml.v3"
)

const (
	providersBegin = "# >>> clashgo:providers (managed — edit subscriptions in ClashGo, not here)"
	providersEnd   = "# <<< clashgo:providers"
	useAllMarker   = "# clashgo:use-all"
)

// Render is the compatibility entry point. Invalid input remains invalid rather
// than falling back to a potentially unsafe, unmodified master.
func Render(master string, providers []ProviderSource, st Settings) string {
	out, err := RenderChecked(master, providers, st)
	if err != nil {
		return "clashgo-render-error: [\n"
	}
	return out
}

// RenderChecked changes only the runtime copy. Decoding into values resolves
// aliases/merge keys and rejects duplicates before applying mandatory settings.
func RenderChecked(master string, providers []ProviderSource, st Settings) (string, error) {
	root, err := parseMaster(master)
	if err != nil {
		return "", err
	}
	pool := map[string]any{}
	uses := []string{}
	for _, p := range providers {
		if p.Name == "" || strings.ContainsAny(p.Name, "/\\\x00") {
			return "", fmt.Errorf("invalid provider name")
		}
		if _, exists := pool[p.Name]; exists {
			return "", fmt.Errorf("duplicate provider name")
		}
		item := map[string]any{
			"type": "http", "url": p.URL, "interval": 86400,
			"path":         "./providers/" + p.Name + ".yaml",
			"exclude-type": "direct|reject", "exclude-filter": noticeFilter(),
			"health-check": map[string]any{"enable": true, "url": "https://www.gstatic.com/generate_204", "interval": 300},
		}
		if p.Split || p.Local || p.Managed {
			item["type"] = "file"
			delete(item, "url")
			delete(item, "interval")
		}
		pool[p.Name] = item
		uses = append(uses, p.Name)
		if p.Split && strings.TrimSpace(p.InlineYAML) != "" {
			var nodes []any
			if err := yaml.Unmarshal([]byte(p.InlineYAML), &nodes); err != nil {
				return "", fmt.Errorf("invalid provider plumbing")
			}
			if len(nodes) > 0 {
				existing, ok := root["proxies"].([]any)
				if root["proxies"] != nil && !ok {
					return "", fmt.Errorf("proxies must be a sequence")
				}
				root["proxies"] = append(existing, nodes...)
			}
		}
	}
	root["proxy-providers"] = pool
	// Subscription strategy groups never enter this pipeline. Only the app's
	// own selector gets the managed pool; user-owned master data stays on disk.
	if groups, ok := root["proxy-groups"].([]any); ok {
		for _, value := range groups {
			if group, ok := value.(map[string]any); ok && group["name"] == "节点选择" {
				group["use"] = uses
				delete(group, "use-all-providers")
			}
		}
	}
	if err := runtimeKeys(root, st); err != nil {
		return "", err
	}
	if err := pinHosts(root, providers); err != nil {
		return "", err
	}
	return encodeMaster(root)
}

func parseMaster(text string) (map[string]any, error) {
	var root map[string]any
	dec := yaml.NewDecoder(strings.NewReader(text))
	if err := dec.Decode(&root); err != nil || root == nil {
		return nil, fmt.Errorf("master must be a valid YAML mapping")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("master must contain one YAML document")
	}
	return root, nil
}

func mapping(root map[string]any, key string) (map[string]any, error) {
	if root[key] == nil {
		root[key] = map[string]any{}
	}
	m, ok := root[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a mapping", key)
	}
	return m, nil
}

// CoreRoutingMark is stamped on every socket the core dials (proxy and
// DIRECT alike) so the tun-only nft table can exempt the core's own egress.
// cmd/tunonly-helper carries the same value; keep them in sync.
const CoreRoutingMark = 0x4347

// TunDeviceName replaces the core's default TUN device name ("Meta")
// with one that reads as a second wired NIC, so interface listings
// carry neither a product label nor an obvious tunnel name.
const TunDeviceName = "enp7s0"

func runtimeKeys(root map[string]any, st Settings) error {
	root["mixed-port"] = st.MixedPort
	root["external-controller"] = fmt.Sprintf("127.0.0.1:%d", st.ControllerPort)
	root["secret"] = st.ControllerSecret
	root["allow-lan"] = false
	root["mode"] = "rule"
	root["routing-mark"] = CoreRoutingMark
	if _, exists := root["ipv6"]; !exists {
		root["ipv6"] = false
	}
	profile, err := mapping(root, "profile")
	if err != nil {
		return err
	}
	if _, exists := profile["store-selected"]; !exists {
		profile["store-selected"] = true
	}
	if _, exists := root["tun"]; exists || st.TUNEnabled {
		tun, err := mapping(root, "tun")
		if err != nil {
			return err
		}
		tun["enable"], tun["mtu"], tun["device"] = st.TUNEnabled, 1500, TunDeviceName
		if st.TUNEnabled {
			for key, value := range map[string]any{"stack": "gvisor", "auto-route": true, "auto-detect-interface": true, "dns-hijack": []string{"any:53"}} {
				if _, exists := tun[key]; !exists {
					tun[key] = value
				}
			}
		}
	}
	if st.TUNEnabled {
		dns, err := mapping(root, "dns")
		if err != nil {
			return err
		}
		dns["enable"] = true
		if v, _ := dns["listen"].(string); strings.TrimSpace(v) == "" {
			dns["listen"] = "127.0.0.1:1053"
		}
		if _, exists := dns["nameserver"]; !exists {
			dns["nameserver"] = []string{"223.5.5.5", "119.29.29.29"}
		}
	}
	return nil
}

func pinHosts(root map[string]any, providers []ProviderSource) error {
	seen := map[string]bool{}
	var pins []any
	for _, p := range providers {
		u, err := url.Parse(p.URL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		host := strings.ToLower(u.Hostname())
		if host == "" || seen[host] {
			continue
		}
		seen[host] = true
		if ip := net.ParseIP(host); ip != nil {
			kind, mask := "IP-CIDR", "/32"
			if ip.To4() == nil {
				kind, mask = "IP-CIDR6", "/128"
			}
			pins = append(pins, kind+","+host+mask+",DIRECT,no-resolve")
			continue
		}
		pins = append(pins, "DOMAIN,"+host+",DIRECT")
		dns, err := mapping(root, "dns")
		if err != nil {
			return err
		}
		policy, err := mapping(dns, "nameserver-policy")
		if err != nil {
			return err
		}
		policy[host] = []string{"https://dns.alidns.com/dns-query#DIRECT", "https://doh.pub/dns-query#DIRECT"}
	}
	if len(pins) > 0 {
		rules, ok := root["rules"].([]any)
		if root["rules"] != nil && !ok {
			return fmt.Errorf("rules must be a sequence")
		}
		root["rules"] = append(pins, rules...)
	}
	return nil
}

func encodeMaster(root map[string]any) (string, error) {
	var doc yaml.Node
	if err := doc.Encode(root); err != nil {
		return "", err
	}
	var style func(*yaml.Node)
	style = func(n *yaml.Node) {
		if n.Kind == yaml.MappingNode {
			for i := 0; i+1 < len(n.Content); i += 2 {
				key, value := n.Content[i], n.Content[i+1]
				if key.Value == "use" {
					value.Style = yaml.FlowStyle
				}
				if key.Value == "exclude-type" {
					value.Style = yaml.DoubleQuotedStyle
				}
				if key.Value == "nameserver-policy" && value.Kind == yaml.MappingNode {
					for j := 0; j < len(value.Content); j += 2 {
						value.Content[j].Style = yaml.DoubleQuotedStyle
					}
				}
			}
		}
		for _, child := range n.Content {
			style(child)
		}
	}
	style(&doc)
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return "", err
	}
	return buf.String(), enc.Close()
}

// Kept for runtime-key contract tests and callers that need no providers.
func forceRuntimeKeys(text string, st Settings) string {
	root, err := parseMaster(text)
	if err != nil {
		return "clashgo-render-error: [\n"
	}
	if err := runtimeKeys(root, st); err != nil {
		return "clashgo-render-error: [\n"
	}
	out, _ := encodeMaster(root)
	return out
}
