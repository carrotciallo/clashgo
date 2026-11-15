package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

// Opt-in: no production paths, system proxy, TUN or remote network requests.
func TestIsolatedMihomo(t *testing.T) {
	binary := os.Getenv("CLASHGO_TEST_MIHOMO")
	if binary == "" {
		t.Skip("set CLASHGO_TEST_MIHOMO to run isolated sidecar integration")
	}
	home := t.TempDir()
	for _, key := range []string{"XDG_CONFIG_HOME", "XDG_STATE_HOME", "XDG_CACHE_HOME", "XDG_DATA_HOME"} {
		t.Setenv(key, filepath.Join(home, key))
	}
	port := func() int {
		l, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		p := l.Addr().(*net.TCPAddr).Port
		if err := l.Close(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	mixed, ctrl := port(), port()
	for ctrl == mixed {
		ctrl = port()
	}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodConnect {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		conn, rw, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = rw.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n")
		_ = rw.Flush()
		request, err := http.ReadRequest(rw.Reader)
		if err != nil {
			return
		}
		_ = request.Body.Close()
		_, _ = rw.WriteString("HTTP/1.1 204 No Content\r\nConnection: close\r\n\r\n")
		_ = rw.Flush()
	}))
	defer upstream.Close()
	_, upstreamPort, err := net.SplitHostPort(upstream.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, "providers"), 0700); err != nil {
		t.Fatal(err)
	}
	pool := fmt.Sprintf("proxies:\n  - {name: fixture-node, type: http, server: 127.0.0.1, port: %s}\n", upstreamPort)
	if err := os.WriteFile(filepath.Join(home, "providers", "fixture.yaml"), []byte(pool), 0600); err != nil {
		t.Fatal(err)
	}
	master := `log-level: error
geo-auto-update: false
profile: {store-selected: true}
proxy-groups:
  - {name: 节点选择, type: select, proxies: [DIRECT]}
rules: ["MATCH,DIRECT"]
`
	st := config.Settings{MixedPort: mixed, ControllerPort: ctrl, ControllerSecret: "isolated-fixture"}
	text, err := config.RenderChecked(master, []config.ProviderSource{{Name: "fixture", Managed: true}}, st)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(home, "config.yaml")
	validate := func(path string) error { return ValidateConfig(binary, home, path) }
	if err := config.InstallRuntime(file, text, validate, nil); err != nil {
		t.Fatal(err)
	}
	sup := &Supervisor{BinPath: binary, HomeDir: home, LogPath: filepath.Join(home, "core.log")}
	if err := sup.Start(file); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop()
	client := NewClient("http://127.0.0.1:"+strconv.Itoa(ctrl), st.ControllerSecret)
	if err := client.WaitReady(5 * time.Second); err != nil {
		t.Fatal(err)
	}
	// The controller starts before mihomo finishes applying its listeners.
	readyCtx, readyCancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer readyCancel()
	if err := client.WaitCaptureReady(readyCtx, mixed, false); err != nil {
		t.Fatal(err)
	}
	runtimeCapture, err := client.CaptureConfig(context.Background())
	if err != nil || runtimeCapture.MixedPort != mixed || runtimeCapture.TUN.Enable {
		t.Fatalf("runtime capture state mismatch: %+v, %v", runtimeCapture, err)
	}
	providers, err := client.Providers()
	if err != nil || len(providers["fixture"]) != 1 {
		t.Fatalf("provider load failed: %v", err)
	}
	if _, err := client.ProviderDelay("fixture", "fixture-node", upstream.URL+"/check", 1000); err != nil {
		t.Fatalf("provider healthcheck: %v", err)
	}
	if err := client.Switch("节点选择", "fixture-node"); err != nil {
		t.Fatal(err)
	}
	if _, err := client.ConnectionsSnapshot(); err != nil {
		t.Fatal(err)
	}
	if err := config.InstallRuntime(file, text, validate, client.ReloadConfig); err != nil {
		t.Fatalf("reload: %v", err)
	}
	if err := config.InstallRuntime(file, "rules: [", validate, client.ReloadConfig); err == nil {
		t.Fatal("invalid runtime accepted")
	}
	after, err := os.ReadFile(file)
	if err != nil || string(after) != text {
		t.Fatal("invalid install changed runtime")
	}
	if !sup.Running() {
		t.Fatal("sidecar died")
	}
	t.Log("isolated validation, startup, file provider, healthcheck, selection, snapshot, reload and rejected install passed")
}
