package core

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func TestWaitCaptureAfterVersionBeforeSettings(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	_, s, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(s)
	var reads atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/version" {
			fmt.Fprint(w, `{"version":"ready-controller"}`)
			return
		}
		p := 0
		if reads.Add(1) > 2 {
			p = port
		}
		fmt.Fprintf(w, `{"mixed-port":%d}`, p)
	}))
	defer srv.Close()
	client := NewClient(srv.URL, "")
	if err := client.WaitReady(time.Second); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckCaptureReady(context.Background(), port, false); err == nil {
		t.Fatal("zero-port runtime accepted")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := client.WaitCaptureReady(ctx, port, false); err != nil {
		t.Fatal(err)
	}
	if reads.Load() < 3 {
		t.Fatal("did not wait for applied config")
	}
	listener.Close()
	ctx2, cancel2 := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancel2()
	if err := client.WaitCaptureReady(ctx2, port, false); err == nil {
		t.Fatal("configured but closed listener accepted")
	}
}
