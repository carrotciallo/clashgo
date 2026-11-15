package main

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
)

// svcDef is one dashboard service check. Checks run through the
// local mixed port, so a result reflects the real routed path a
// user's request to that service would take.
type svcDef struct {
	Name string
	URL  string
	Mono string // monogram shown in the badge (no brand icons in lucide)
}

var serviceDefs = []svcDef{
	{"Google", "https://www.google.com/generate_204", "G"},
	{"GitHub", "https://github.com", "GH"},
	{"YouTube", "https://www.youtube.com/generate_204", "YT"},
	{"ChatGPT", "https://chatgpt.com", "CG"},
	{"Claude", "https://claude.ai", "CL"},
	{"Gemini", "https://gemini.google.com", "GE"},
	{"Netflix", "https://www.netflix.com", "N"},
	{"Disney+", "https://www.disneyplus.com", "D+"},
	{"Prime Video", "https://www.primevideo.com", "PV"},
	{"Spotify", "https://open.spotify.com", "SP"},
	{"TikTok", "https://www.tiktok.com", "TT"},
	{"Discord", "https://discord.com", "DC"},
	{"Bilibili", "https://www.bilibili.com", "BI"},
}

// svcResult is one service's last check outcome.
type svcResult struct {
	State   int // 0 unchecked, 1 checking, 2 reachable, 3 failed
	Latency time.Duration
}

// svcStatusText renders a result as the (text, dot colour) pair the
// dashboard rows show.
func svcStatusText(r svcResult) (string, ui.Color) {
	switch r.State {
	case 1:
		return "检测中", colWarn
	case 2:
		if ms := r.Latency.Milliseconds(); ms > 0 {
			return fmt.Sprintf("%d ms", ms), delayColor(int(ms))
		}
		return "<1 ms", colOK
	case 3:
		return "检测失败", colDanger
	}
	return "待检测", colMuted
}

// enabledServices resolves the user's selection from settings
// (nil selection = the whole catalog; empty selection = none).
func (a *App) enabledServices() []svcDef {
	if a.set.Services == nil {
		return serviceDefs
	}
	want := map[string]bool{}
	for _, n := range a.set.Services {
		want[n] = true
	}
	out := make([]svcDef, 0, len(serviceDefs))
	for _, d := range serviceDefs {
		if want[d.Name] {
			out = append(out, d)
		}
	}
	return out
}

func (a *App) serviceEnabled(name string) bool {
	for _, d := range a.enabledServices() {
		if d.Name == name {
			return true
		}
	}
	return false
}

// setServiceEnabled persists one manage-page toggle. Once the user
// customises the set it is stored explicitly (materialised from the
// all-on default first).
func (a *App) setServiceEnabled(name string, on bool) {
	names := []string{}
	if a.set.Services == nil {
		for _, d := range serviceDefs {
			names = append(names, d.Name)
		}
	} else {
		names = append(names, a.set.Services...)
	}
	out := names[:0]
	found := false
	for _, n := range names {
		if n == name {
			found = true
			if on {
				out = append(out, n)
			}
			continue
		}
		out = append(out, n)
	}
	if on && !found {
		out = append(out, name)
	}
	a.set.Services = out
	if err := a.store.SaveSettings(a.set); err != nil {
		a.setMsg("保存服务设置失败：%v", err)
	}
}

// checkServices probes every enabled service through the proxy port
// in parallel. Any HTTP response at all counts as reachable — several
// services answer 403/404 to a bare GET, which still proves the path.
func (a *App) checkServices() {
	a.mu.Lock()
	if a.svcChecking {
		a.mu.Unlock()
		return
	}
	a.svcChecking = true
	if a.svcResults == nil {
		a.svcResults = map[string]svcResult{}
	}
	defs := a.enabledServices()
	for _, d := range defs {
		a.svcResults[d.Name] = svcResult{State: 1}
	}
	port := a.set.MixedPort
	a.mu.Unlock()

	proxyURL, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", port))
	cl := &http.Client{
		Timeout:   8 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyURL)},
	}
	defer cl.CloseIdleConnections()
	var wg sync.WaitGroup
	for _, d := range defs {
		wg.Add(1)
		go func(d svcDef) {
			defer wg.Done()
			t0 := time.Now()
			res := svcResult{State: 3}
			if resp, err := cl.Get(d.URL); err == nil {
				_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
				resp.Body.Close()
				res = svcResult{State: 2, Latency: time.Since(t0)}
			}
			a.mu.Lock()
			a.svcResults[d.Name] = res
			a.mu.Unlock()
		}(d)
	}
	wg.Wait()
	a.mu.Lock()
	a.svcChecking = false
	a.mu.Unlock()
}
