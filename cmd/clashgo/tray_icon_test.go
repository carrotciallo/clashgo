package main

import (
	"bytes"
	"image/color"
	"image/png"
	"testing"
)

func TestTrayUsesSameCatInColorAndGray(t *testing.T) {
	active, err := png.Decode(bytes.NewReader(trayIconPNG(true)))
	if err != nil {
		t.Fatal(err)
	}
	inactive, err := png.Decode(bytes.NewReader(trayIconPNG(false)))
	if err != nil {
		t.Fatal(err)
	}
	if active.Bounds() != inactive.Bounds() || active.Bounds().Dx() != 256 {
		t.Fatal("tray image dimensions changed")
	}
	colored := false
	for y := 0; y < 256; y++ {
		for x := 0; x < 256; x++ {
			a := color.NRGBAModel.Convert(active.At(x, y)).(color.NRGBA)
			b := color.NRGBAModel.Convert(inactive.At(x, y)).(color.NRGBA)
			if b.R != b.G || b.G != b.B || b.A != a.A {
				t.Fatalf("gray image changed silhouette or contains color at %d,%d", x, y)
			}
			if a.A > 0 && (a.R != a.G || a.G != a.B) {
				colored = true
			}
		}
	}
	if !colored {
		t.Fatal("active cat has no color")
	}
	first, second := trayIconPNG(false), trayIconPNG(false)
	if &first[0] != &second[0] {
		t.Fatal("grayscale image not cached")
	}
}

func TestCaptureEffectiveRequiresHealthySelectedCapture(t *testing.T) {
	for _, tc := range []struct {
		name                                string
		healthy, tun, tunReady, owned, want bool
	}{
		{"stopped", false, false, false, true, false},
		{"system owned", true, false, false, true, true},
		{"system stolen", true, false, false, false, false},
		{"settings alone", true, true, false, false, false},
		{"tun ready", true, true, true, false, true},
		{"tun missing despite system proxy", true, true, false, true, false},
		{"system mode ignores tun", true, false, true, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := captureEffective(tc.healthy, tc.tun, tc.tunReady, tc.owned); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
