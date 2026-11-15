package core

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProxySnapshotReadsProviderOnce(t *testing.T) {
	for _, failure := range []bool{false, true} {
		t.Run(fmt.Sprint(failure), func(t *testing.T) {
			var global, pools atomic.Int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/proxies":
					global.Add(1)
					fmt.Fprint(w, `{"proxies":{"节点选择":{"name":"节点选择","type":"Selector"},"same":{"name":"same","type":"Direct"}}}`)
				case "/providers/proxies":
					pools.Add(1)
					if failure {
						w.WriteHeader(503)
						return
					}
					fmt.Fprint(w, `{"providers":{"pool":{"proxies":[{"name":"node","type":"Shadowsocks"},{"name":"same","type":"Reject"}]}}}`)
				default:
					t.Errorf("unexpected endpoint %s", r.URL.Path)
				}
			}))
			defer srv.Close()
			px, pv, err := NewClient(srv.URL, "").ProxySnapshot(context.Background())
			if err != nil || global.Load() != 1 || pools.Load() != 1 {
				t.Fatalf("snapshot %v calls %d/%d", err, global.Load(), pools.Load())
			}
			if px["same"].Type != "Direct" {
				t.Fatal("provider overwrote global")
			}
			if failure {
				if pv != nil {
					t.Fatal("failure should preserve previous provider view")
				}
			} else if px["node"].Name != "node" || len(pv["pool"]) != 2 {
				t.Fatal("missing merged provider")
			}
		})
	}
}

func TestReadyQuickRetryAndCancellation(t *testing.T) {
	var attempts atomic.Int32
	first, second := make(chan time.Time, 1), make(chan time.Time, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			first <- time.Now()
			w.WriteHeader(503)
			return
		}
		second <- time.Now()
		fmt.Fprint(w, `{"version":"test"}`)
	}))
	defer srv.Close()
	if err := NewClient(srv.URL, "").WaitReady(time.Second); err != nil {
		t.Fatal(err)
	}
	gap := (<-second).Sub(<-first)
	if gap >= 200*time.Millisecond {
		t.Fatalf("first retry still uses slow polling: %v", gap)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := NewClient(srv.URL, "").WaitReadyContext(ctx, time.Second); err == nil {
		t.Fatal("ignored cancellation")
	}
}
