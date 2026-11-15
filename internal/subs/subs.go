// Package subs is the subscription registry: subscriptions are only
// node pools. A failed refresh never touches the last good provider
// file, so a dead subscription degrades to stale nodes, never to a
// broken network.
package subs

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

type Subscription struct {
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

type Registry struct {
	path string
	Subs []Subscription `json:"subscriptions"`
}

var nameRe = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ValidName reports whether a name is safe as a provider key and as a
// file name.
func ValidName(name string) bool { return nameRe.MatchString(name) }

func Load(path string) (*Registry, error) {
	r := &Registry{path: path}
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return r, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, r); err != nil {
		return nil, fmt.Errorf("subscriptions.json: %w", err)
	}
	r.path = path
	return r, nil
}

func (r *Registry) Save() error {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

func (r *Registry) Add(name, rawURL string) error {
	if !ValidName(name) {
		return fmt.Errorf("订阅名只能用字母、数字、下划线和连字符")
	}
	if !strings.HasPrefix(rawURL, "http://") && !strings.HasPrefix(rawURL, "https://") {
		return fmt.Errorf("订阅地址必须以 http:// 或 https:// 开头")
	}
	for _, s := range r.Subs {
		if s.Name == name {
			return fmt.Errorf("已存在同名订阅 %s", name)
		}
	}
	r.Subs = append(r.Subs, Subscription{Name: name, URL: rawURL})
	return r.Save()
}

// AddLocal registers a content-based subscription (paste or file):
// same name rules as Add, but no URL is stored.
func (r *Registry) AddLocal(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("订阅名只能用字母、数字、下划线和连字符")
	}
	for _, s := range r.Subs {
		if s.Name == name {
			return fmt.Errorf("已存在同名订阅 %s", name)
		}
	}
	r.Subs = append(r.Subs, Subscription{Name: name, Local: true})
	return r.Save()
}

func (r *Registry) Remove(name string) error {
	for i, s := range r.Subs {
		if s.Name == name {
			r.Subs = append(r.Subs[:i], r.Subs[i+1:]...)
			return r.Save()
		}
	}
	return fmt.Errorf("没有订阅 %s", name)
}

func (r *Registry) Find(name string) *Subscription {
	for i := range r.Subs {
		if r.Subs[i].Name == name {
			return &r.Subs[i]
		}
	}
	return nil
}

// Sources adapts the registry to what config rendering needs. For
// split subscriptions it also reads the plumbing file back and
// exposes its proxies as inline YAML: rendering splices them into
// the main config's proxies: section, where the dialer chains
// register globally (a provider file cannot hold them — its
// exclude-filter would unregister them; see SplitFullConfig).
// A missing plumbing file yields no inline YAML.
func (r *Registry) Sources(providersDir string) []config.ProviderSource {
	out := make([]config.ProviderSource, 0, len(r.Subs))
	for _, s := range r.Subs {
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
