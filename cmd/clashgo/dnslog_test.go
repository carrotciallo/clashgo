package main

import (
	"testing"
	"time"
)

// The sample lines are verbatim from a live core's debug log
// stream (2026-11-12), so the parser is pinned to the real wire
// format, not a paraphrase.
func TestDNSTrackerResultPairsLatency(t *testing.T) {
	tr := newDNSTracker()
	t0 := time.Date(2026, 11, 12, 8, 16, 20, 0, time.Local)
	if e := tr.feed("[DNS] resolve www.baidu.com A from https://doh.pub:443/dns-query", t0); e != nil {
		t.Fatalf("resolve line completed an entry: %+v", e)
	}
	if e := tr.feed("[DNS] resolve www.baidu.com A from https://dns.alidns.com:443/dns-query", t0); e != nil {
		t.Fatalf("second server line completed an entry: %+v", e)
	}
	e := tr.feed("[DNS] www.baidu.com --> [183.2.172.177] A from https://doh.pub:443/dns-query", t0.Add(19*time.Millisecond))
	if e == nil {
		t.Fatal("result line produced no entry")
	}
	if e.Host != "www.baidu.com" || e.Type != "A" || e.Result != "183.2.172.177" || e.Via != "直连" || e.Ms != 19 || e.Cached {
		t.Fatalf("entry = %+v", e)
	}
}

func TestDNSTrackerProxyViaAndCacheHit(t *testing.T) {
	tr := newDNSTracker()
	t0 := time.Date(2026, 11, 12, 8, 16, 21, 0, time.Local)
	tr.feed("[DNS] resolve www.wikipedia.org A from https://8.8.8.8:443/dns-query", t0)
	e := tr.feed("[DNS] www.wikipedia.org --> [103.102.166.224] A from https://1.1.1.1:443/dns-query", t0.Add(87*time.Millisecond))
	if e == nil || e.Via != "代理" || e.Ms != 87 {
		t.Fatalf("proxy entry = %+v", e)
	}
	e = tr.feed("[DNS] cache hit www.wikipedia.org --> [103.102.166.224] A, expire at 2026-11-12 08:19:12", t0)
	if e == nil || !e.Cached || e.Ms != 0 || e.Result != "103.102.166.224" {
		t.Fatalf("cache entry = %+v", e)
	}
	// The untyped summary line must not duplicate the result.
	if e := tr.feed("[DNS] www.wikipedia.org --> 103.102.166.224", t0); e != nil {
		t.Fatalf("summary line produced an entry: %+v", e)
	}
	// Non-DNS lines are ignored.
	if e := tr.feed("[TCP] 127.0.0.1:1(curl) --> x:443", t0); e != nil {
		t.Fatalf("TCP line produced an entry: %+v", e)
	}
}

func TestDNSTrackerEmptyAndError(t *testing.T) {
	tr := newDNSTracker()
	t0 := time.Date(2026, 11, 12, 8, 16, 22, 0, time.Local)
	tr.feed("[DNS] resolve gone.example.com A from https://1.1.1.1:443/dns-query", t0)
	e := tr.feed("[DNS] gone.example.com --> [] A from https://1.1.1.1:443/dns-query", t0.Add(5*time.Millisecond))
	if e == nil || e.Result != "无结果" {
		t.Fatalf("empty entry = %+v", e)
	}
	tr.feed("[DNS] resolve bad.example.com AAAA from https://8.8.8.8:443/dns-query", t0)
	e = tr.feed("[DNS] resolve bad.example.com error: couldn't find ip", t0.Add(30*time.Millisecond))
	if e == nil || e.Result != "失败" || e.Type != "AAAA" || e.Ms != 30 {
		t.Fatalf("error entry = %+v", e)
	}
}
