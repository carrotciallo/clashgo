package subs

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const validPool = `proxies: [{name: node, type: ss, server: 127.0.0.1, port: 1234, cipher: aes-128-gcm, password: fixture}]`

func TestIngestRejectsWithoutChangingPool(t *testing.T) {
	for _, bad := range []string{
		"proxies: [", "proxies: []", "proxies: [3]", "proxies: [{name: x}]",
		"proxies: [{name: x, type: direct}]", "proxies: [{name: 剩余流量, type: ss}]",
		"proxies: []\nproxies: []", validPool + "\n---\nproxies: []",
		"proxies: [{name: x, type: ss}, {name: x, type: ss}]",
		"proxies: [{name: x, type: ss, dialer-proxy: missing}]",
		"proxies: [{name: x, type: ss, dialer-proxy: x}]",
		strings.Repeat(" ", maxPayloadSize+1),
	} {
		dir := t.TempDir()
		sub := Subscription{Name: "test", Local: true}
		if err := IngestLocal(&sub, []byte(validPool), dir); err != nil {
			t.Fatal(err)
		}
		before := sub
		path := filepath.Join(dir, "test.yaml")
		old, _ := os.ReadFile(path)
		if err := IngestLocal(&sub, []byte(bad), dir); err == nil {
			t.Fatal("accepted invalid payload")
		}
		got, _ := os.ReadFile(path)
		if string(got) != string(old) || !reflect.DeepEqual(before, sub) {
			t.Fatal("failed import changed pool or metadata")
		}
	}
}

func TestIngestDropsAllForeignSections(t *testing.T) {
	dir := t.TempDir()
	sub := Subscription{Name: "test"}
	body := validPool + "\nproxy-groups: [{name: selector, type: select, proxies: [node]}]\ndns: {enable: false}\nrules: [MATCH,REJECT]\n"
	if err := IngestLocal(&sub, []byte(body), dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "test.yaml"))
	var doc map[string]any
	if err := yaml.Unmarshal(data, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc) != 1 || doc["proxies"] == nil || sub.NodeCount != 1 {
		t.Fatalf("unexpected pool: %v", doc)
	}
	if CountNodes(data) != 1 {
		t.Fatal("incorrect count")
	}
}

func TestFailedFetchDoesNotChangeQuota(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Subscription-Userinfo", "upload=999; total=1234")
		_, _ = w.Write([]byte("proxies: []"))
	}))
	defer srv.Close()
	sub := Subscription{Name: "test", URL: srv.URL, UploadBytes: 3}
	before := sub
	if err := Fetch(context.Background(), &sub, t.TempDir()); err == nil {
		t.Fatal("accepted empty pool")
	}
	if !reflect.DeepEqual(before, sub) {
		t.Fatal("failed fetch changed metadata")
	}
}

func TestIngestStagesPlumbingBeforeReplacingPool(t *testing.T) {
	dir := t.TempDir()
	sub := Subscription{Name: "test"}
	path := filepath.Join(dir, "test.yaml")
	if err := os.WriteFile(path, []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "test.plumbing.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := IngestLocal(&sub, []byte(validPool), dir); err == nil {
		t.Fatal("ignored plumbing failure")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "old" || sub.NodeCount != 0 {
		t.Fatal("partially installed pool")
	}
}

func TestIngestPreservesPoolDialers(t *testing.T) {
	dir := t.TempDir()
	sub := Subscription{Name: "test"}
	body := `proxies:
- {name: node, type: ss, dialer-proxy: __relay}
- {name: __relay, type: ss}
proxy-groups:
- {name: a, proxies: [node]}
- {name: b, proxies: [node]}
`
	if err := IngestLocal(&sub, []byte(body), dir); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(dir, "test.plumbing.yaml"))
	if !sub.Split || !strings.Contains(string(data), "__relay") || sub.NodeCount != 1 {
		t.Fatal("lost required dialer")
	}
}
