package core

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestProviderDelayRoute(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/providers/proxies/sub%2Fone/node%2Ftwo/healthcheck" || r.Header.Get("Authorization") != "Bearer fixture" || r.URL.Query().Get("timeout") != "5000" {
			t.Errorf("wrong controller request: %s", r.URL.EscapedPath())
		}
		_ = json.NewEncoder(w).Encode(map[string]int{"delay": 37})
	}))
	defer server.Close()
	d, err := NewClient(server.URL, "fixture").ProviderDelay("sub/one", "node/two", "https://example.com", 5000)
	if err != nil || d != 37 {
		t.Fatalf("delay=%d err=%v", d, err)
	}
}

func TestWaitReadyDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }))
	defer server.Close()
	start := time.Now()
	if NewClient(server.URL, "").WaitReady(30*time.Millisecond) == nil || time.Since(start) > time.Second {
		t.Fatal("readiness ignored deadline")
	}
}

func TestWaitReadyRejectsUnrelatedHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"hello":"world"}`)) }))
	defer server.Close()
	if NewClient(server.URL, "").WaitReady(20*time.Millisecond) == nil {
		t.Fatal("unrelated HTTP accepted as mihomo")
	}
}

func TestClientNullProxiesAndIPv6(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/proxies":
			_, _ = w.Write([]byte(`{"proxies":null}`))
		case "/providers/proxies":
			_, _ = w.Write([]byte(`{"providers":{"p":{"proxies":[{"name":"n","type":"Shadowsocks"}]}}}`))
		case "/connections":
			_, _ = w.Write([]byte(`{"connections":[{"id":"1","metadata":{"destinationIP":"2001:db8::1","destinationPort":"443"}}]}`))
		}
	}))
	defer server.Close()
	c := NewClient(server.URL, "")
	p, err := c.Proxies()
	if err != nil || p["n"].Name != "n" {
		t.Fatal("null map not merged")
	}
	connections, err := c.Connections()
	if err != nil || len(connections) != 1 || connections[0].Target != "[2001:db8::1]:443" {
		t.Fatalf("IPv6 target: %+v %v", connections, err)
	}
}

func TestConnectionSnapshotSingleRequest(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"uploadTotal":12,"downloadTotal":34,"connections":[{"id":"one"}]}`))
	}))
	defer server.Close()
	snapshot, err := NewClient(server.URL, "").ConnectionsSnapshot()
	if err != nil || snapshot.UploadTotal != 12 || snapshot.DownloadTotal != 34 || len(snapshot.Connections) != 1 || calls != 1 {
		t.Fatalf("inconsistent snapshot: %+v, calls=%d, err=%v", snapshot, calls, err)
	}
}

func TestClientBoundsAndMarshalFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(strings.Repeat("x", (16<<20)+1))) }))
	defer server.Close()
	c := NewClient(server.URL, "")
	if _, err := c.do("GET", "/", nil); err == nil {
		t.Fatal("unbounded response")
	}
	if _, err := c.do("POST", "/", make(chan int)); err == nil {
		t.Fatal("marshal error ignored")
	}
}
