package subs

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := Load(filepath.Join(t.TempDir(), "subscriptions.json"))
	if err != nil {
		t.Fatal(err)
	}
	return r
}
func TestRegistryDetachedSnapshotsAndStaleResults(t *testing.T) {
	r := testRegistry(t)
	if err := r.AddLocal("x"); err != nil {
		t.Fatal(err)
	}
	s := r.Find("x")
	s.ProxyNames = []string{"node"}
	if err := r.Update(s); err != nil {
		t.Fatal(err)
	}
	snapshot := r.Snapshot()
	snapshot[0].ProxyNames[0] = "changed"
	if r.Find("x").ProxyNames[0] != "node" {
		t.Fatal("snapshot aliases registry")
	}
	stale := r.Find("x")
	if err := r.Remove("x"); err != nil {
		t.Fatal(err)
	}
	if err := r.AddLocal("x"); err != nil {
		t.Fatal(err)
	}
	if err := r.Update(stale); !errors.Is(err, ErrStaleSubscription) {
		t.Fatal("accepted removed generation", err)
	}
}
func TestRegistrySaveFailureLeavesMemoryUnchanged(t *testing.T) {
	r := testRegistry(t)
	if err := r.AddLocal("x"); err != nil {
		t.Fatal(err)
	}
	r.path = filepath.Join(t.TempDir(), "missing", "registry.json")
	if err := r.Remove("x"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if r.Find("x") == nil {
		t.Fatal("failed removal changed memory")
	}
	if err := r.AddLocal("y"); err == nil {
		t.Fatal("expected persistence failure")
	}
	if r.Find("y") != nil {
		t.Fatal("failed addition changed memory")
	}
}
func TestRegistryRefreshDeletedWhileDownloading(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		close(entered)
		<-release
		_, _ = w.Write([]byte("proxies: [{name: node, type: http, server: localhost, port: 1234}]"))
	}))
	defer srv.Close()
	r := testRegistry(t)
	if err := r.Add("x", srv.URL); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	result := make(chan error, 1)
	go func() { result <- r.Fetch(context.Background(), "x", dir) }()
	<-entered
	if err := r.Remove("x"); err != nil {
		t.Fatal(err)
	}
	if err := r.Add("x", srv.URL); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-result; !errors.Is(err, ErrStaleSubscription) {
		t.Fatal("stale download committed", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 0 {
		t.Fatal("stale download left live or staging files")
	}
	if r.Find("x").NodeCount != 0 {
		t.Fatal("new generation modified")
	}
}
func TestRegistryConcurrentReadersAndUpdates(t *testing.T) {
	r := testRegistry(t)
	if err := r.AddLocal("x"); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 20 {
				s := r.Find("x")
				s.NodeCount++
				err := r.Update(s)
				if err != nil && !errors.Is(err, ErrStaleSubscription) {
					t.Error(err)
				}
				_ = r.Snapshot()
			}
		}()
	}
	wg.Wait()
}
