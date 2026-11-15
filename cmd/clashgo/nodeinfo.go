package main

import (
	"strings"
	"sync"

	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/egoist/mygo/ui"
	"github.com/xiyaowong/flagicons"
)

// Node display helpers for the Shadowrocket-style node rows: which
// flag a node wears, the name without its baked-in flag, and the
// protocol sub-line. Pure functions, pinned by nodeinfo_test.go.

func isRegionalIndicator(r rune) bool {
	return r >= 0x1F1E6 && r <= 0x1F1FF
}

// flagCodeOf extracts the two-letter code of a flag baked into a
// node name (a leading regional-indicator pair).
func flagCodeOf(name string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) >= 2 && isRegionalIndicator(r[0]) && isRegionalIndicator(r[1]) {
		return string([]rune{'A' + (r[0] - 0x1F1E6), 'A' + (r[1] - 0x1F1E6)})
	}
	return ""
}

type regionRule struct {
	code     string
	keywords []string
}

// Ordered: the first keyword hit wins. Both Chinese spellings and
// English names are covered; bare two-letter codes are not matched
// (too many false positives inside unrelated names).
var regionRules = []regionRule{
	{"HK", []string{"香港", "Hong Kong", "HongKong"}},
	{"TW", []string{"台湾", "臺灣", "Taiwan"}},
	{"JP", []string{"日本", "Japan"}},
	{"KR", []string{"韩国", "韓國", "Korea"}},
	{"SG", []string{"新加坡", "Singapore"}},
	{"US", []string{"美国", "美國", "United States", "USA"}},
	{"GB", []string{"英国", "英國", "United Kingdom", "Britain"}},
	{"TH", []string{"泰国", "泰國", "Thailand"}},
	{"VN", []string{"越南", "Vietnam"}},
	{"RU", []string{"俄罗斯", "俄羅斯", "Russia"}},
	{"DE", []string{"德国", "德國", "Germany"}},
	{"FR", []string{"法国", "法國", "France"}},
	{"CA", []string{"加拿大", "Canada"}},
	{"AU", []string{"澳大利亚", "澳洲", "Australia"}},
	{"IN", []string{"印度", "India"}},
	{"ID", []string{"印尼", "印度尼西亚", "Indonesia"}},
	{"MY", []string{"马来西亚", "馬來西亞", "Malaysia"}},
	{"PH", []string{"菲律宾", "菲律賓", "Philippines"}},
	{"TR", []string{"土耳其", "Turkey", "Türkiye"}},
	{"AR", []string{"阿根廷", "Argentina"}},
	{"BR", []string{"巴西", "Brazil"}},
	{"NL", []string{"荷兰", "荷蘭", "Netherlands"}},
	{"CN", []string{"中国", "中國", "China"}},
}

// regionCode resolves a node's ISO country code. A flag baked into
// the name wins (the plain one provider pool: "🇰🇷 E-韩国1"); otherwise the
// region is read from keywords in the name — the split iplc pool's
// nodes are flattened group names ("H01香港 主力") and carry no
// emoji of their own, which is why they showed no flag before.
// "" means the region is unknown.
func regionCode(name string) string {
	if c := flagCodeOf(name); c != "" {
		return c
	}
	for _, r := range regionRules {
		for _, kw := range r.keywords {
			if strings.Contains(name, kw) {
				return r.code
			}
		}
	}
	return ""
}

// Parsed circle flags, cached: ui.ParseSVG must not run per frame.
var flagCache sync.Map // lowercase code -> *ui.SVG

// flagImage returns the circle-cut flag for an ISO code, from
// xiyaowong/flagicons (lipis/flag-icons). Unknown codes return nil
// and the row falls back to a globe badge.
func flagImage(code string) *ui.SVG {
	if code == "" {
		return nil
	}
	key := strings.ToLower(code)
	if v, ok := flagCache.Load(key); ok {
		return v.(*ui.SVG)
	}
	f, err := flagicons.Circle(key)
	if err != nil {
		return nil
	}
	flagCache.Store(key, f)
	return f
}

// displayName is the row's title: the name with a leading baked-in
// flag removed (the flag already sits in the badge beside it).
func displayName(name string) string {
	r := []rune(strings.TrimSpace(name))
	if len(r) >= 2 && isRegionalIndicator(r[0]) && isRegionalIndicator(r[1]) {
		return strings.TrimSpace(string(r[2:]))
	}
	return name
}

// protoLine is the row's sub-line, Shadowrocket-style:
// "ANYTLS / UDP" — upper-cased type, UDP support when present.
func protoLine(p core.Proxy) string {
	t := strings.ToUpper(p.Type)
	if t == "" {
		return ""
	}
	if p.UDP {
		return t + " / UDP"
	}
	return t
}
