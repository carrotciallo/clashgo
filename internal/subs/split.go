package subs

import (
	"bytes"
	"strings"

	"github.com/carrotciallo/clashgo/internal/config"
	"gopkg.in/yaml.v3"
)

// SplitFullConfig detects a subscription payload that is actually a
// whole client config (top-level proxy-groups next to rules/dns/…)
// and flattens it into ClashGo's model at import time:
//
//   - The payload's proxy-groups are discarded wholesale. Routing
//     structure belongs to the user's master config, never to a
//     subscription; nothing group-shaped crosses the import border.
//   - A "leaf" group — one whose members are all proxies — is what
//     the source config presents as a selectable node (typically a
//     fallback over several entry relays into one exit). It becomes
//     a plain proxy: a copy of its first (preferred) member, renamed
//     to the group's name. Entry-relay failover inside the group is
//     the one behavior flattening drops; the exit itself is
//     unchanged.
//   - Plumbing proxies (the __dialer_* chains) survive only where a
//     flattened node still reaches them through dialer-proxy links;
//     the rest is pruned.
//
// The split produces two artifacts: the provider document (the
// flattened nodes only) and a plumbing document with the dialer
// proxies the nodes chain through. The plumbing cannot live in the
// provider file: a provider's exclude-filter would unregister it
// from the core entirely and the nodes' dialer-proxy references
// would resolve nowhere (measured live 2026-11-11). Rendering
// splices the plumbing into the main config's proxies: section
// instead, where it registers globally without joining any group.
//
// Two full-config families exist in the wild, told apart by group
// membership (measured 2026-11-12 against both live payloads):
//
//   - Relay-family configs (iplc): the proxies are entry relays,
//     each referenced by exactly one leaf group — its exit. Only
//     the leaf groups are user-facing, so only they flatten.
//   - Pool-family configs (one provider's new panel): the proxies ARE the
//     node pool and every service group lists all of them; the one
//     all-proxy group (a url-test over the whole pool) is a pool
//     organizer, not an exit. Flattening it once collapsed 72 real
//     nodes into a single clone of its first member — which was
//     the 8.8.8.8 notice entry. Pool-family payloads therefore
//     skip flattening entirely: their proxies pass through as the
//     nodes (notices and pseudo outbounds filtered), and all of
//     their groups drop like any other's.
//
// Notice entries disguised as groups are dropped by name, like
// everywhere else. ok=false means the payload is not a full config
// (an ordinary provider file) and passes through untouched.
func SplitFullConfig(body []byte) (providerYAML, plumbingYAML []byte, nodeNames []string, ok bool) {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil || len(doc.Content) == 0 {
		return nil, nil, nil, false
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, nil, false
	}
	get := func(key string) *yaml.Node {
		for i := 0; i+1 < len(root.Content); i += 2 {
			if root.Content[i].Value == key {
				return root.Content[i+1]
			}
		}
		return nil
	}
	groups := get("proxy-groups")
	proxies := get("proxies")
	if groups == nil || proxies == nil || proxies.Kind != yaml.SequenceNode {
		return nil, nil, nil, false
	}

	byName := map[string]*yaml.Node{}
	var order []string
	for _, item := range proxies.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		if name := nodeName(item); name != "" {
			byName[name] = item
			order = append(order, name)
		}
	}

	// Decode the groups once; membership counts across all of
	// them tell the two full-config families apart (see the doc
	// comment): an exit relay is referenced by exactly one group,
	// a pool node by every service group in the file.
	type groupDef struct {
		Name    string   `yaml:"name"`
		Proxies []string `yaml:"proxies"`
	}
	var groupDefs []groupDef
	membership := map[string]int{}
	if groups.Kind == yaml.SequenceNode {
		for _, item := range groups.Content {
			var g groupDef
			if err := item.Decode(&g); err != nil || g.Name == "" {
				continue
			}
			groupDefs = append(groupDefs, g)
			for _, m := range g.Proxies {
				membership[m]++
			}
		}
	}
	var leafDefs []groupDef
	for _, g := range groupDefs {
		if len(g.Proxies) == 0 || config.IsNoticeName(g.Name) {
			continue
		}
		leaf := true
		for _, m := range g.Proxies {
			if _, isProxy := byName[m]; !isProxy {
				leaf = false
				break
			}
		}
		if leaf {
			leafDefs = append(leafDefs, g)
		}
	}
	// Leaf groups flatten only in the relay family: every member
	// of every leaf group referenced by that group alone. One
	// pool-shared member flips the payload to pool family.
	leafMode := len(leafDefs) > 0
	for _, g := range leafDefs {
		for _, m := range g.Proxies {
			if membership[m] != 1 {
				leafMode = false
			}
		}
	}

	// Leaf groups become nodes.
	var leaves []*yaml.Node
	if leafMode {
		for _, g := range leafDefs {
			clone := cloneNode(byName[g.Proxies[0]])
			setNodeName(clone, g.Name)
			forceUDP(clone)
			leaves = append(leaves, clone)
			nodeNames = append(nodeNames, g.Name)
		}
	}

	encode := func(items []*yaml.Node) []byte {
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		seq.Content = append(seq.Content, items...)
		out := &yaml.Node{Kind: yaml.MappingNode}
		out.Content = append(out.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: "proxies"}, seq)
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(out); err != nil {
			return nil
		}
		_ = enc.Close()
		return buf.Bytes()
	}

	if len(leaves) > 0 {
		// Keep only the plumbing the flattened nodes still reach.
		keep := map[string]bool{}
		var walk func(n *yaml.Node)
		walk = func(n *yaml.Node) {
			dp := nodeScalar(n, "dialer-proxy")
			if dp == "" || keep[dp] {
				return
			}
			if target, ok := byName[dp]; ok {
				keep[dp] = true
				walk(target)
			}
		}
		for _, leaf := range leaves {
			walk(leaf)
		}
		var plumbing []*yaml.Node
		for _, name := range order {
			if keep[name] {
				forceUDP(byName[name])
				plumbing = append(plumbing, byName[name])
			}
		}
		providerYAML = encode(leaves)
		if providerYAML == nil {
			return nil, nil, nil, false
		}
		return providerYAML, encode(plumbing), nodeNames, true
	}

	// Pool family (or no leaf groups at all): the proxies are
	// the nodes. Plumbing (__-prefixed), notice entries and
	// pseudo outbounds (direct/reject info entries) never count
	// as user nodes.
	var kept []*yaml.Node
	for _, item := range proxies.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		name := nodeName(item)
		if name == "" || strings.HasPrefix(name, "__") || config.IsNoticeName(name) {
			continue
		}
		if t := nodeScalar(item, "type"); strings.EqualFold(t, "direct") || strings.EqualFold(t, "reject") {
			continue
		}
		forceUDP(item)
		kept = append(kept, item)
		nodeNames = append(nodeNames, name)
	}
	providerYAML = encode(kept)
	if providerYAML == nil {
		return nil, nil, nil, false
	}
	return providerYAML, nil, nodeNames, true
}

// ProviderItems extracts the proxies list items of a provider
// document as YAML text (items at indent 0), for inlining into the
// main config's proxies: section. Returns "" when the document has
// no proxies sequence.
func ProviderItems(body []byte) string {
	var doc yaml.Node
	if err := yaml.Unmarshal(body, &doc); err != nil || len(doc.Content) == 0 {
		return ""
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return ""
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value != "proxies" {
			continue
		}
		seq := root.Content[i+1]
		if seq.Kind != yaml.SequenceNode || len(seq.Content) == 0 {
			// An empty sequence would encode as a bare "[]",
			// which spliced under proxies: is invalid YAML and
			// once killed a fresh install's first render
			// (2026-11-12, split sub with no dialer plumbing).
			return ""
		}
		var buf bytes.Buffer
		enc := yaml.NewEncoder(&buf)
		enc.SetIndent(2)
		if err := enc.Encode(seq); err != nil {
			return ""
		}
		_ = enc.Close()
		return buf.String()
	}
	return ""
}

// nodeName reads a proxy mapping's name field.
func nodeName(m *yaml.Node) string { return nodeScalar(m, "name") }

// nodeScalar reads a string field of a mapping node ("" when absent).
func nodeScalar(m *yaml.Node, key string) string {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == key {
			return m.Content[i+1].Value
		}
	}
	return ""
}

// setNodeName rewrites a proxy mapping's name field in place.
func setNodeName(m *yaml.Node, name string) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "name" {
			m.Content[i+1].Value = name
			return
		}
	}
}

// forceUDP marks a proxy mapping as UDP-capable. Full-config
// payloads ship their chained entries with udp: false — that is
// bookkeeping for the source core's group wiring, not a
// capability statement: measured live (2026-11-12) the same
// chains relay UDP cleanly (STUN 5/5) once the flag allows it,
// while inheriting the false disables UDP entirely for every
// flattened node — QUIC, WebRTC and game traffic all die at the
// local gate. Split output therefore always asserts UDP.
func forceUDP(m *yaml.Node) {
	if m.Kind != yaml.MappingNode {
		return
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		if m.Content[i].Value == "udp" {
			m.Content[i+1].Tag = "!!bool"
			m.Content[i+1].Value = "true"
			return
		}
	}
	m.Content = append(m.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "udp"},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: "true"})
}

// cloneNode deep-copies a YAML node tree, so a flattened node's
// rename never mutates the source proxy it was copied from.
func cloneNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	c := *n
	if n.Content != nil {
		c.Content = make([]*yaml.Node, len(n.Content))
		for i, ch := range n.Content {
			c.Content[i] = cloneNode(ch)
		}
	}
	return &c
}
