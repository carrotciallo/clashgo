package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// latestRead coalesces background metadata requests. At most one read runs;
// a newer request cancels its predecessor and invalidates its publication.
type latestRead struct {
	mu         sync.Mutex
	gate       sync.Mutex
	cancel     context.CancelFunc
	generation uint64
}

func (r *latestRead) begin(parent context.Context) (context.Context, uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		r.cancel()
	}
	ctx, cancel := context.WithTimeout(parent, 8*time.Second)
	r.cancel = cancel
	r.generation++
	return ctx, r.generation
}
func (r *latestRead) publish(ctx context.Context, generation uint64, fn func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if ctx.Err() == nil && generation == r.generation {
		fn()
	}
}

// refreshEgress is called by serialized core operations, but never holds opMu
// across external requests. Core/client generation is rechecked on publication.
func (a *App) refreshEgress() {
	ctx, generation := a.egressRead.begin(a.appContext())
	client, pid, port := a.client(), a.sup.PID(), a.settings().MixedPort
	a.launch(func() {
		a.egressRead.gate.Lock()
		defer a.egressRead.gate.Unlock()
		if ctx.Err() != nil {
			return
		}
		tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)})}
		cl := &http.Client{Transport: tr, Timeout: 8 * time.Second}
		defer cl.CloseIdleConnections()
		a.mu.Lock()
		cachedIP, cachedOrg := a.egressOrgIP, a.egressOrg
		a.mu.Unlock()
		ip, loc, org := readEgress(ctx, cl, "https://www.cloudflare.com/cdn-cgi/trace", "https://ipinfo.io/json", cachedIP, cachedOrg)
		if ip == "" {
			return
		}
		a.opMu.Lock()
		defer a.opMu.Unlock()
		if a.client() != client || a.sup.PID() != pid {
			return
		}
		a.egressRead.publish(ctx, generation, func() {
			a.mu.Lock()
			defer a.mu.Unlock()
			a.egress = ip + " · " + loc
			if ip != a.egressOrgIP {
				a.egressOrg = ""
				a.egressOrgIP = ip
			}
			if org != "" {
				a.egressOrg = org
			}
		})
	})
}

func readEgress(ctx context.Context, cl *http.Client, traceURL, orgURL, cachedIP, cachedOrg string) (ip, loc, org string) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, traceURL, nil)
	if err != nil {
		return
	}
	resp, err := cl.Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "ip=") {
			ip = strings.TrimPrefix(ln, "ip=")
		}
		if strings.HasPrefix(ln, "loc=") {
			loc = strings.TrimPrefix(ln, "loc=")
		}
	}
	if ip != "" {
		if ip == cachedIP && cachedOrg != "" {
			return ip, loc, cachedOrg
		}
		orgCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		org, _ = fetchEgressOrgContext(orgCtx, cl, orgURL, ip)
	}
	return
}
