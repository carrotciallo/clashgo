package subs

import "testing"

func TestParseUserinfo(t *testing.T) {
	sub := &Subscription{}
	parseUserinfo("upload=1024; download=2048; total=1099511627776; expire=1893456000", sub)
	if sub.UploadBytes != 1024 || sub.DownloadBytes != 2048 || sub.TotalBytes != 1099511627776 || sub.ExpireUnix != 1893456000 {
		t.Fatalf("parsed wrong: %+v", sub)
	}
	if sub.UsedBytes() != 3072 {
		t.Fatalf("UsedBytes = %d", sub.UsedBytes())
	}
	// Absent or malformed headers change nothing.
	parseUserinfo("", sub)
	parseUserinfo("upload=oops; nonsense", sub)
	if sub.UploadBytes != 1024 {
		t.Fatalf("malformed header clobbered values: %+v", sub)
	}
}
