package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// dnsEntry is one completed DNS resolution as FlClash's DNS tab
// shows it: when, what, which type, which path served it, the
// first answer, and how long it took.
type dnsEntry struct {
	At     time.Time
	Host   string
	Type   string
	Via    string // 代理 / 直连 / 其他 path label; "" for cache hits
	Result string // first answer IP, 无结果， or 失败
	Cached bool
	Ms     int // milliseconds; -1 = unknown
}

// dnsTracker folds mihomo's debug DNS log lines into completed
// entries. The core logs one "resolve" line per nameserver tried
// and one result line from the winner; cache hits arrive whole.
type dnsTracker struct {
	pending map[string]time.Time
}

func newDNSTracker() *dnsTracker {
	return &dnsTracker{pending: map[string]time.Time{}}
}

// feed consumes one log payload; it returns a completed entry or
// nil when the line only opens a query (or is not DNS at all).
func (t *dnsTracker) feed(payload string, now time.Time) *dnsEntry {
	if !strings.HasPrefix(payload, "[DNS] ") {
		return nil
	}
	body := strings.TrimPrefix(payload, "[DNS] ")

	if strings.HasPrefix(body, "cache hit ") {
		// cache hit H --> [ips] T, expire at ...
		rest := strings.TrimPrefix(body, "cache hit ")
		host, ips, qtype, ok := parseArrow(rest)
		if !ok {
			return nil
		}
		return &dnsEntry{At: now, Host: host, Type: qtype, Result: firstResult(ips), Cached: true, Ms: 0}
	}
	if strings.HasPrefix(body, "resolve ") {
		rest := strings.TrimPrefix(body, "resolve ")
		if i := strings.Index(rest, " error:"); i >= 0 {
			host := rest[:i]
			e := &dnsEntry{At: now, Host: host, Result: "失败", Ms: -1}
			for _, qt := range []string{"A", "AAAA"} {
				if start, ok := t.pending[host+"|"+qt]; ok {
					e.Type = qt
					e.Ms = int(now.Sub(start).Milliseconds())
					delete(t.pending, host+"|"+qt)
					break
				}
			}
			return e
		}
		// resolve H T from SERVER — a query opens; remember the
		// first server line per host+type for the latency span.
		f := strings.Fields(rest)
		if len(f) >= 4 && f[2] == "from" {
			key := f[0] + "|" + f[1]
			if _, ok := t.pending[key]; !ok {
				t.pending[key] = now
			}
		}
		return nil
	}
	if strings.Contains(body, " --> [") && strings.Contains(body, " from ") {
		host, ips, qtype, ok := parseArrow(body)
		if !ok {
			return nil
		}
		server := body[strings.LastIndex(body, " from ")+len(" from "):]
		e := &dnsEntry{At: now, Host: host, Type: qtype, Via: dnsVia(server), Result: firstResult(ips), Ms: -1}
		key := host + "|" + qtype
		if start, ok := t.pending[key]; ok {
			e.Ms = int(now.Sub(start).Milliseconds())
			delete(t.pending, key)
		}
		return e
	}
	// "[DNS] H --> ip" summary lines duplicate the typed result.
	return nil
}

// parseArrow splits "H --> [ip ip] T ..." into host, answers, type.
func parseArrow(s string) (string, []string, string, bool) {
	i := strings.Index(s, " --> [")
	if i < 0 {
		return "", nil, "", false
	}
	host := s[:i]
	rest := s[i+len(" --> ["):]
	j := strings.Index(rest, "]")
	if j < 0 {
		return "", nil, "", false
	}
	var ips []string
	if inner := strings.TrimSpace(rest[:j]); inner != "" {
		ips = strings.Fields(inner)
	}
	f := strings.Fields(rest[j+1:])
	if len(f) == 0 {
		return "", nil, "", false
	}
	// Cache-hit lines continue ", expire at ..." right after the
	// type token, so it can arrive as "A,".
	return host, ips, strings.TrimSuffix(f[0], ","), true
}

func firstResult(ips []string) string {
	if len(ips) == 0 {
		return "无结果"
	}
	return ips[0]
}

// dnsVia labels which nameserver path served a query: the main
// nameserver pair rides the proxy; the policy nameservers for
// domestic domains go direct.
func dnsVia(server string) string {
	s := server
	if i := strings.Index(s, "://"); i >= 0 {
		s = s[i+3:]
	}
	host := s
	if i := strings.IndexAny(host, ":/"); i >= 0 {
		host = host[:i]
	}
	switch host {
	case "1.1.1.1", "8.8.8.8", "dns.google", "cloudflare-dns.com":
		return "代理"
	case "dns.alidns.com", "doh.pub", "223.5.5.5", "223.6.6.6", "119.29.29.29":
		return "直连"
	}
	return host
}

// dnsLogLoop keeps a debug-level subscription to the core's log
// stream and folds its DNS lines into the query log. The stream
// level is per-subscription: the core's own file logging stays
// at info. Reconnects whenever the core restarts or the stream
// breaks.
func (a *App) dnsLogLoop() {
	tracker := newDNSTracker()
	for {
		a.dnsLogStream(tracker)
		time.Sleep(2 * time.Second)
	}
}

func (a *App) dnsLogStream(tracker *dnsTracker) {
	a.mu.Lock()
	cli := a.cli
	a.mu.Unlock()
	if cli == nil {
		return
	}
	req, err := http.NewRequest(http.MethodGet, cli.Base+"/logs?level=debug", nil)
	if err != nil {
		return
	}
	if cli.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+cli.Secret)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		var line struct {
			Payload string `json:"payload"`
		}
		if json.Unmarshal(sc.Bytes(), &line) != nil {
			continue
		}
		e := tracker.feed(line.Payload, time.Now())
		if e == nil {
			continue
		}
		a.mu.Lock()
		a.dnsLog = append([]dnsEntry{*e}, a.dnsLog...)
		if len(a.dnsLog) > 400 {
			a.dnsLog = a.dnsLog[:400]
		}
		a.dnsTotal++
		a.mu.Unlock()
	}
}

// dnsLogView is the DNS module shared by the overview card and
// the requests page: the core's query history as a zebra table
// (FlClash's DNS 查询 tab).
func (a *App) dnsLogView(c *ui.Context) {
	a.mu.Lock()
	entries := a.dnsLog
	a.mu.Unlock()
	ui.Row(c).FillWidth().Padding(8, 16).Gap(10).Background(colInset).Children(func() {
		txt(c, "时间", fsMicro, true, colFaint).Width(64)
		txt(c, "域名", fsMicro, true, colFaint).Grow(1)
		txt(c, "类型", fsMicro, true, colFaint).Width(40).TextAlign(ui.End)
		txt(c, "发起", fsMicro, true, colFaint).Width(44).TextAlign(ui.End)
		txt(c, "结果", fsMicro, true, colFaint).Width(170)
		txt(c, "耗时", fsMicro, true, colFaint).Width(58)
	})
	if len(entries) == 0 {
		emptyRow(c, "暂无 DNS 查询记录")
		return
	}
	for i, e := range entries {
		if i >= 120 {
			break
		}
		row := ui.Row(c).FillWidth().Padding(9, 16).Gap(10).AlignItems(ui.Center)
		if i%2 == 1 {
			row.Background(colZebra)
		}
		e := e
		row.Children(func() {
			num(c, e.At.Format("15:04:05"), fsSmall, false, colFaint).Width(64)
			txt(c, e.Host, fsSmall, true, colText).Grow(1).SingleLine()
			txt(c, e.Type, fsSmall, false, colMuted).Width(40).TextAlign(ui.End)
			txt(c, orDash(e.Via), fsSmall, false, colMuted).Width(44).TextAlign(ui.End)
			ui.Row(c).Width(170).Gap(6).AlignItems(ui.Center).Children(func() {
				if e.Cached {
					tag(c, "缓存", colMuted)
				}
				txt(c, orDash(e.Result), fsSmall, false, colMuted).SingleLine()
			})
			ms := "—"
			if e.Ms >= 0 {
				ms = fmt.Sprintf("%d ms", e.Ms)
			}
			num(c, ms, fsSmall, false, colMuted).Width(58).TextAlign(ui.End)
		})
	}
}
