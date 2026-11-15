package core

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTUNLaunchRejectsSuppressedFileCapabilities(t *testing.T) {
	for _, tc := range []struct {
		status string
		fail   bool
	}{
		{"NoNewPrivs:\t1\nCapPrm:\t0000000000000000\n", true},
		{"NoNewPrivs:\t0\nCapPrm:\t0000000000000000\n", false},
		{"NoNewPrivs:\t1\nCapPrm:\t0000000000003000\n", false},
		{"NoNewPrivs:\t1\nCapPrm:\t0000000000002000\n", true},
	} {
		err := checkTUNLaunchStatus(tc.status)
		if (err != nil) != tc.fail {
			t.Fatalf("status %q: %v", tc.status, err)
		}
		if err != nil && !strings.Contains(err.Error(), "NoNewPrivs") {
			t.Fatal(err)
		}
	}
}

func TestTUNReadinessRequiresActualInterface(t *testing.T) {
	for _, tc := range []struct {
		name          string
		enabled       bool
		device        string
		flags         net.Flags
		mtu           int
		missing, fail bool
	}{
		{"ready", true, "Meta", net.FlagUp, 1500, false, false},
		{"disabled", false, "Meta", net.FlagUp, 1500, false, true},
		{"empty device", true, "", net.FlagUp, 1500, false, true},
		{"failed initialization", true, "Meta", net.FlagUp, 1500, true, true},
		{"down", true, "Meta", 0, 1500, false, true},
		{"wrong mtu", true, "Meta", net.FlagUp, 1400, false, true},
		{"loopback is not tun", true, "lo", net.FlagUp | net.FlagLoopback, 1500, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var config CaptureConfig
			config.TUN.Enable, config.TUN.Device = tc.enabled, tc.device
			err := checkTUNDevice(config, func(name string) (*net.Interface, error) {
				if name != tc.device {
					t.Fatal("wrong device lookup")
				}
				if tc.missing {
					return nil, errors.New("no such network interface")
				}
				return &net.Interface{Name: name, Flags: tc.flags, MTU: tc.mtu}, nil
			})
			if (err != nil) != tc.fail {
				t.Fatalf("error=%v", err)
			}
		})
	}
}

func TestVersionHealthyDoesNotProveTUNReady(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			w.Write([]byte(`{"version":"test"}`))
			return
		}
		w.Write([]byte(`{"tun":{"enable":true,"device":"clashgo-no-such-tun-test"}}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "")
	if err := client.WaitReady(time.Second); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 75*time.Millisecond)
	defer cancel()
	err := client.WaitTUNReady(ctx)
	if err == nil || !strings.Contains(err.Error(), "TUN") || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected TUN readiness failure, got %v", err)
	}
}
