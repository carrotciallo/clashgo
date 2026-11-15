package main

import (
	"bytes"
	"context"
	"fmt"
	"image/color"
	"image/png"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/carrotciallo/clashgo/internal/core"
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

// A usable local proxy must stay colored even when the user disables global
// capture and connects individual applications to the mixed port directly.
func TestProxyServiceReadyIndependentOfSystemCapture(t *testing.T) {
	for _, tc := range []struct {
		name                                        string
		healthy, listening, tun, mismatch, canceled bool
		status                                      int
		want                                        bool
	}{
		{"manual proxy without system capture", true, true, false, false, false, 200, true},
		{"tun enabled", true, true, true, false, false, 200, true},
		{"stopped core", false, true, false, false, false, 200, false},
		{"listener closed", true, false, false, false, false, 200, false},
		{"tun flag alone", true, false, true, false, false, 200, false},
		{"wrong port", true, true, false, true, false, 200, false},
		{"unauthorized", true, true, false, false, false, 401, false},
		{"controller error", true, true, false, false, false, 503, false},
		{"canceled", true, true, false, false, true, 200, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer listener.Close()
			_, portText, _ := net.SplitHostPort(listener.Addr().String())
			port, _ := strconv.Atoi(portText)
			if !tc.listening {
				listener.Close()
			}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tc.status)
				reported := port
				if tc.mismatch {
					reported++
				}
				fmt.Fprintf(w, `{"mixed-port":%d,"tun":{"enable":%t}}`, reported, tc.tun)
			}))
			defer server.Close()
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if tc.canceled {
				cancel()
			}
			if got := proxyServiceReady(ctx, core.NewClient(server.URL, ""), port, tc.healthy); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}
