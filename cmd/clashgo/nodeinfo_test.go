package main

import (
	"testing"

	"github.com/carrotciallo/clashgo/internal/core"
)

func TestRegionCode(t *testing.T) {
	cases := []struct{ name, code string }{
		// Baked-in flags (plain provider pool) win as-is.
		{"🇬🇧 E-英国1", "GB"},
		{"🇰🇷 S-韩国2", "KR"},
		{"🇹🇼 E-台湾1", "TW"},
		// Keyword-derived (split pool's flattened group names).
		{"H01香港 主力", "HK"},
		{"J04韩国 低速", "KR"},
		{"J05日本 主力", "JP"},
		{"S07新加坡 主力", "SG"},
		{"H09台湾 主力", "TW"},
		{"H11泰国 低速", "TH"},
		{"H12越南 冷门", "VN"},
		{"S13美国 半费", "US"},
		{"S19英国 冷门", "GB"},
		{"J20俄罗斯 低速", "RU"},
		// Unknown region: no code.
		{"mystery node 42", ""},
	}
	for _, c := range cases {
		if got := regionCode(c.name); got != c.code {
			t.Errorf("regionCode(%q) = %q; want %q", c.name, got, c.code)
		}
	}
}

func TestDisplayName(t *testing.T) {
	cases := map[string]string{
		"🇰🇷 E-韩国1": "E-韩国1",
		"🇹🇼 S-台湾3": "S-台湾3",
		"H01香港 主力": "H01香港 主力",
		"J06日本 主力": "J06日本 主力",
	}
	for in, want := range cases {
		if got := displayName(in); got != want {
			t.Errorf("displayName(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestProtoLine(t *testing.T) {
	if got := protoLine(core.Proxy{Type: "AnyTLS", UDP: true}); got != "ANYTLS / UDP" {
		t.Errorf("protoLine(AnyTLS+udp) = %q", got)
	}
	if got := protoLine(core.Proxy{Type: "Shadowsocks"}); got != "SHADOWSOCKS" {
		t.Errorf("protoLine(ss) = %q", got)
	}
	if got := protoLine(core.Proxy{}); got != "" {
		t.Errorf("protoLine(empty) = %q", got)
	}
}

func TestFlagImage(t *testing.T) {
	if img := flagImage("JP"); img == nil {
		t.Error("flagImage(JP) = nil; want a parsed circle flag")
	}
	if img := flagImage(""); img != nil {
		t.Error("flagImage(\"\") != nil; want nil")
	}
	// NB: "xx" is a real placeholder flag in flag-icons; QQ is not
	// a code the set carries.
	if img := flagImage("QQ"); img != nil {
		t.Error("flagImage(QQ) != nil; want nil for an unknown code")
	}
}
