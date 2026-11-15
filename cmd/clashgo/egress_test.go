package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/carrotciallo/clashgo/internal/core"
)

func TestLatestReadCancelsAndRejectsStaleResults(t *testing.T) {
	var r latestRead
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	first, g1 := r.begin(parent)
	second, g2 := r.begin(parent)
	if first.Err() == nil {
		t.Fatal("previous request not canceled")
	}
	count := 0
	r.publish(first, g1, func() { count += 10 })
	r.publish(second, g1, func() { count += 10 })
	r.publish(second, g2, func() { count++ })
	cancel()
	r.publish(second, g2, func() { count += 10 })
	if count != 1 {
		t.Fatalf("invalid publication count %d", count)
	}
}

func TestSlowEgressDoesNotBlockCoreOperations(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		w.WriteHeader(502)
	}))
	defer server.Close()
	defer close(release)
	_, p, _ := net.SplitHostPort(server.Listener.Addr().String())
	port, _ := strconv.Atoi(p)
	a := &App{work: core.NewTasks(context.Background()), sup: &core.Supervisor{}}
	a.set.MixedPort = port
	a.cli = core.NewClient("http://127.0.0.1:1", "")
	defer a.work.Wait()
	a.opMu.Lock()
	a.refreshEgress()
	a.opMu.Unlock()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("metadata request did not start")
	}
	done := make(chan struct{})
	a.operate(func() { close(done) })
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("metadata held core operation lock")
	}
}

func TestReadEgressCachesOrganizationAndHonorsCancellation(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/trace" {
			fmt.Fprint(w, "ip=192.0.2.1\nloc=ZZ\n")
			return
		}
		calls++
		fmt.Fprint(w, `{"ip":"192.0.2.1","org":"AS123 Example"}`)
	}))
	defer server.Close()
	ip, loc, org := readEgress(context.Background(), server.Client(), server.URL+"/trace", server.URL+"/org", "", "")
	if ip != "192.0.2.1" || loc != "ZZ" || org != "Example" {
		t.Fatalf("unexpected metadata %q %q %q", ip, loc, org)
	}
	_, _, org = readEgress(context.Background(), server.Client(), server.URL+"/trace", server.URL+"/org", ip, org)
	if calls != 1 || org != "Example" {
		t.Fatal("cache ignored")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	ip, _, _ = readEgress(ctx, server.Client(), server.URL+"/trace", server.URL+"/org", "", "")
	if ip != "" {
		t.Fatal("canceled lookup returned data")
	}
}
