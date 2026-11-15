package main

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/carrotciallo/clashgo/internal/core"
)

func TestDNSPendingBoundExpirationAndCacheCleanup(t *testing.T) {
	tr := newDNSTracker()
	now := time.Now()
	for i := range dnsPendingLimit + 50 {
		tr.feed(fmt.Sprintf("[DNS] resolve h%d A from x", i), now.Add(time.Duration(i)*time.Millisecond))
	}
	if len(tr.pending) != dnsPendingLimit {
		t.Fatal("pending unbounded")
	}
	tr.feed("not DNS", now.Add(2*dnsPendingTTL))
	if len(tr.pending) != 0 {
		t.Fatal("pending expired entries survived")
	}
	tr.feed("[DNS] resolve h A from x", now)
	tr.feed("[DNS] cache hit h --> [1.2.3.4] A, expire at x", now)
	if len(tr.pending) != 0 {
		t.Fatal("cache result left pending")
	}
	if got := dnsVia("https://user:password@resolver.example/dns-query?token=private"); got != "resolver.example" {
		t.Fatal("resolver display exposed details", got)
	}
}
func TestDNSStreamCancelsWhenControllerChanges(t *testing.T) {
	entered := make(chan struct{})
	closed := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.(http.Flusher).Flush()
		close(entered)
		<-r.Context().Done()
		close(closed)
	}))
	defer srv.Close()
	a := &App{cli: core.NewClient(srv.URL, "")}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	done := make(chan struct{})
	go func() { a.dnsLogStream(ctx, newDNSTracker()); close(done) }()
	<-entered
	a.stateMu.Lock()
	a.cli = core.NewClient("http://127.0.0.1:1", "")
	a.stateMu.Unlock()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("old stream stayed alive")
	}
	select {
	case <-closed:
	case <-ctx.Done():
		t.Fatal("request not cancelled")
	}
}
