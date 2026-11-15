// Package subs is the subscription registry: subscriptions are only
// node pools. A failed refresh never touches the last good provider
// file, so a dead subscription degrades to stale nodes, never to a
// broken network.
package subs

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

type Subscription struct {
	revision  uint64
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
	NodeCount int       `json:"nodeCount"`
	LastError string    `json:"lastError,omitempty"`
	// Split records that the payload was a full client config and
	// ClashGo flattened it at fetch time: the payload's groups were
	// discarded and its leaf groups became plain provider nodes.
	// ProxyNames lists those nodes for display.
	Split      bool     `json:"split,omitempty"`
	ProxyNames []string `json:"proxyNames,omitempty"`
	// Quota from the subscription-userinfo response header, when the
	// airport sends one: traffic in bytes and the expiry as unix
	// seconds (0 = not reported).
	UploadBytes   int64 `json:"uploadBytes,omitempty"`
	DownloadBytes int64 `json:"downloadBytes,omitempty"`
	TotalBytes    int64 `json:"totalBytes,omitempty"`
	ExpireUnix    int64 `json:"expireUnix,omitempty"`
	// Local subscriptions came from pasted content or a file: there
	// is no URL to refetch, so refreshing one is a no-op.
	Local bool `json:"local,omitempty"`
}

// Sources adapts the registry to what config rendering needs. For
// split subscriptions it also reads the plumbing file back and
// exposes its proxies as inline YAML: rendering splices them into
// the main config's proxies: section, where the dialer chains
// register globally (a provider file cannot hold them — its
// exclude-filter would unregister them; see SplitFullConfig).
// A missing plumbing file yields no inline YAML.
func (r *Registry) Sources(providersDir string) []config.ProviderSource {
	snapshot := r.Snapshot()
	out := make([]config.ProviderSource, 0, len(snapshot))
	for _, s := range snapshot {
		src := config.ProviderSource{Name: s.Name, URL: s.URL, Split: s.Split, Local: s.Local, Managed: true}
		if s.Split {
			if body, err := os.ReadFile(filepath.Join(providersDir, s.Name+".plumbing.yaml")); err == nil {
				src.InlineYAML = ProviderItems(body)
			}
		}
		out = append(out, src)
	}
	return out
}

// Fetch downloads a subscription into the providers dir. The payload
// must look like a mihomo provider file (a top-level "proxies:" key);
// anything else is rejected and the previous file kept.
func Fetch(ctx context.Context, sub *Subscription, providersDir string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sub.URL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "clash.meta")
	cl := &http.Client{Timeout: 45 * time.Second}
	resp, err := cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %s", resp.Status)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxPayloadSize+1))
	if err != nil {
		return err
	}
	candidate := *sub
	parseUserinfo(resp.Header.Get("Subscription-Userinfo"), &candidate)
	if err := ingest(&candidate, body, providersDir); err != nil {
		return err
	}
	*sub = candidate
	return nil
}

// IngestLocal installs pasted or file-based subscription content
// through the same validation and splitting as a fetched payload.
func IngestLocal(sub *Subscription, body []byte, providersDir string) error {
	return ingest(sub, body, providersDir)
}

// UsedBytes is the subscription's reported traffic consumption.
func (s *Subscription) UsedBytes() int64 { return s.UploadBytes + s.DownloadBytes }

// parseUserinfo folds a subscription-userinfo header
// ("upload=…; download=…; total=…; expire=…") into the registry
// record. Malformed pairs are skipped; an absent header changes
// nothing.
func parseUserinfo(h string, sub *Subscription) {
	for _, part := range strings.Split(h, ";") {
		kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
		if len(kv) != 2 {
			continue
		}
		v, err := strconv.ParseInt(strings.TrimSpace(kv[1]), 10, 64)
		if err != nil || v < 0 {
			continue
		}
		switch strings.TrimSpace(kv[0]) {
		case "upload":
			sub.UploadBytes = v
		case "download":
			sub.DownloadBytes = v
		case "total":
			sub.TotalBytes = v
		case "expire":
			sub.ExpireUnix = v
		}
	}
}

// ingest validates a subscription payload and installs it as the
// provider file, splitting full client configs on the way. Any
// rejection leaves the previous file untouched.
func ingest(sub *Subscription, body []byte, providersDir string) error {
	if !ValidName(sub.Name) {
		return fmt.Errorf("无效订阅名")
	}
	payload, plumbing, names, split, err := preparePayload(body)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(providersDir, 0o700); err != nil {
		return err
	}
	if err := installPool(providersDir, sub.Name, payload, plumbing); err != nil {
		return err
	}
	sub.Split = split
	sub.ProxyNames = names
	sub.NodeCount = len(names)
	sub.UpdatedAt = time.Now()
	sub.LastError = ""
	return nil
}

func looksLikeProvider(body []byte) bool {
	for _, ln := range strings.Split(string(body), "\n") {
		if strings.HasPrefix(ln, "proxies:") {
			return true
		}
	}
	return false
}

// CountNodes estimates the node count of a provider payload by counting
// list entries under the proxies: key.
func CountNodes(body []byte) int {
	nodes, err := decodePool(body)
	if err != nil {
		return 0
	}
	return len(nodes)
}

// ProviderFileExists reports whether a subscription has a downloaded
// provider file in the runtime dir.
func ProviderFileExists(providersDir, name string) bool {
	_, err := os.Stat(filepath.Join(providersDir, name+".yaml"))
	return err == nil
}
