package core

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCaptureConfigReadsRuntimeState(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/configs" || r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture" {
			t.Error("missing controller auth")
		}
		w.Write([]byte(`{"mixed-port":17897,"tun":{"enable":true,"device":"test-tun"}}`))
	}))
	defer server.Close()
	client := NewClient(server.URL, "fixture")
	config, err := client.CaptureConfig(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if config.MixedPort != 17897 || !config.TUN.Enable || config.TUN.Device != "test-tun" {
		t.Fatalf("wrong runtime capture: %+v", config)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.CaptureConfig(ctx); err == nil {
		t.Fatal("canceled request succeeded")
	}
}

func TestCaptureConfigRejectsFailedOrInvalidResponse(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{"unauthorized", http.StatusUnauthorized, `{}`},
		{"malformed", http.StatusOK, `invalid`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(tc.status); w.Write([]byte(tc.body)) }))
			defer server.Close()
			if _, err := NewClient(server.URL, "").CaptureConfig(context.Background()); err == nil {
				t.Fatal("bad response accepted")
			}
		})
	}
}
