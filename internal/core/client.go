package core

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a minimal mihomo External Controller client.
type Client struct {
	Base   string // e.g. http://127.0.0.1:9097
	Secret string
	http   *http.Client
}

func NewClient(base, secret string) *Client {
	return &Client{Base: base, Secret: secret, http: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) do(method, path string, body any) ([]byte, error) {
	return c.doContext(context.Background(), method, path, body)
}

func (c *Client) doContext(ctx context.Context, method, path string, body any) ([]byte, error) {
	var rdr io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.Base+path, rdr)
	if err != nil {
		return nil, err
	}
	if c.Secret != "" {
		req.Header.Set("Authorization", "Bearer "+c.Secret)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	const maxResponse = 16 << 20
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxResponse+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxResponse {
		return nil, fmt.Errorf("controller response exceeds 16 MiB")
	}
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return b, nil
}

// WaitReady polls /version until the core answers or the deadline hits.
func (c *Client) WaitReady(timeout time.Duration) error {
	return c.WaitReadyContext(context.Background(), timeout)
}

func (c *Client) WaitReadyContext(parent context.Context, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(parent, timeout)
	defer cancel()
	interval := 20 * time.Millisecond
	for {
		b, err := c.doContext(ctx, http.MethodGet, "/version", nil)
		var version struct {
			Version string `json:"version"`
		}
		if err == nil && json.Unmarshal(b, &version) == nil && version.Version != "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("core did not become ready: %w", ctx.Err())
		case <-time.After(interval):
		}
		interval = min(interval*2, 250*time.Millisecond)
	}
}

// Proxy is one entry of GET /proxies: a node or a group.
type Proxy struct {
	Name    string   `json:"name"`
	Type    string   `json:"type"`
	UDP     bool     `json:"udp"`
	Now     string   `json:"now"`
	All     []string `json:"all"`
	History []struct {
		Delay int `json:"delay"`
	} `json:"history"`
}

// Proxies returns the flat proxy map. Note: this mihomo build's
// /proxies contains only config-defined proxies and groups — provider
// nodes live under /providers/proxies — so the two are merged here.
func (c *Client) Proxies() (map[string]Proxy, error) {
	proxies, _, err := c.ProxySnapshot(context.Background())
	return proxies, err
}

// ProxySnapshot shares one provider response between the flat lookup and the
// subscription sections. Provider failure remains best-effort (nil pools).
func (c *Client) ProxySnapshot(ctx context.Context) (map[string]Proxy, map[string][]Proxy, error) {
	b, err := c.doContext(ctx, http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, nil, err
	}
	var resp struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, nil, err
	}
	out := resp.Proxies
	if out == nil {
		out = make(map[string]Proxy)
	}
	pools, _ := c.providersContext(ctx)
	for _, nodes := range pools {
		for _, p := range nodes {
			if _, exists := out[p.Name]; !exists && p.Name != "" {
				out[p.Name] = p
			}
		}
	}
	return out, pools, nil
}

// Providers returns the proxy list of every provider, keyed by
// provider name — the per-subscription node pools the nodes page is
// organized by (a provider and a subscription share a name).
func (c *Client) Providers() (map[string][]Proxy, error) {
	return c.providersContext(context.Background())
}

func (c *Client) providersContext(ctx context.Context) (map[string][]Proxy, error) {
	b, err := c.doContext(ctx, http.MethodGet, "/providers/proxies", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Providers map[string]struct {
			Proxies []Proxy `json:"proxies"`
		} `json:"providers"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	out := map[string][]Proxy{}
	for name, p := range resp.Providers {
		out[name] = p.Proxies
	}
	return out, nil
}

func (c *Client) Switch(group, name string) error {
	_, err := c.do(http.MethodPut, "/proxies/"+url.PathEscape(group), map[string]string{"name": name})
	return err
}

// GroupDelay runs a delay test for every node in a group.
func (c *Client) GroupDelay(group, testURL string, timeoutMs int) (map[string]int, error) {
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeoutMs)}}
	b, err := c.do(http.MethodGet, "/group/"+url.PathEscape(group)+"/delay?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	var out map[string]int
	if err := json.Unmarshal(b, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ProxyDelay measures a single proxy's delay (milliseconds).
func (c *Client) ProxyDelay(name, testURL string, timeoutMs int) (int, error) {
	return c.delay("/proxies/"+url.PathEscape(name)+"/delay", testURL, timeoutMs)
}

// ProviderDelay tests a provider-only node, which is absent from /proxies.
func (c *Client) ProviderDelay(provider, name, testURL string, timeoutMs int) (int, error) {
	return c.delay("/providers/proxies/"+url.PathEscape(provider)+"/"+url.PathEscape(name)+"/healthcheck", testURL, timeoutMs)
}

func (c *Client) delay(path, testURL string, timeoutMs int) (int, error) {
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeoutMs)}}
	b, err := c.do(http.MethodGet, path+"?"+q.Encode(), nil)
	if err != nil {
		return 0, err
	}
	var out struct {
		Delay int `json:"delay"`
	}
	if err := json.Unmarshal(b, &out); err != nil {
		return 0, err
	}
	return out.Delay, nil
}

// TrafficTotals returns cumulative upload/download since core start.
func (c *Client) TrafficTotals() (up, down int64, err error) {
	b, err := c.do(http.MethodGet, "/connections", nil)
	if err != nil {
		return 0, 0, err
	}
	var resp struct {
		UploadTotal   int64 `json:"uploadTotal"`
		DownloadTotal int64 `json:"downloadTotal"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return 0, 0, err
	}
	return resp.UploadTotal, resp.DownloadTotal, nil
}

// CaptureConfig describes the live capture settings returned by GET /configs.
type CaptureConfig struct {
	MixedPort int `json:"mixed-port"`
	TUN       struct {
		Enable bool   `json:"enable"`
		Device string `json:"device"`
	} `json:"tun"`
}

func (c *Client) CaptureConfig(ctx context.Context) (CaptureConfig, error) {
	var config CaptureConfig
	b, err := c.doContext(ctx, http.MethodGet, "/configs", nil)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(b, &config)
	return config, err
}

// ConfigMode returns the running core's routing mode (rule/global/direct).
func (c *Client) ConfigMode() (string, error) {
	b, err := c.do(http.MethodGet, "/configs", nil)
	if err != nil {
		return "", err
	}
	var resp struct {
		Mode string `json:"mode"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return "", err
	}
	return resp.Mode, nil
}

// SetMode switches the routing mode without a config reload.
func (c *Client) SetMode(mode string) error {
	_, err := c.do(http.MethodPatch, "/configs", map[string]string{"mode": mode})
	return err
}

// ReloadConfig asks the running core to load a (validated) config file.
func (c *Client) ReloadConfig(configFile string) error {
	_, err := c.do(http.MethodPut, "/configs?force=true", map[string]string{"path": configFile})
	return err
}

// Connection is one live connection from GET /connections,
// trimmed to what the dashboard shows.
type Connection struct {
	ID       string
	Target   string // host:port (or IP:port)
	Network  string
	Process  string
	Chains   []string
	Rule     string
	Upload   int64
	Download int64
	Start    time.Time
}

// Exit returns the user-meaningful end of the connection's proxy
// chain. Mihomo lists chains leaf-first, so this is the first
// entry that is not internal plumbing (split subscriptions route
// through __dialer_* hops whose names mean nothing on a
// dashboard); "" when the chain is plumbing only.
func (cn Connection) Exit() string {
	for _, ch := range cn.Chains {
		if !strings.HasPrefix(ch, "__") {
			return ch
		}
	}
	return ""
}

type ConnectionSnapshot struct {
	UploadTotal, DownloadTotal int64
	Connections                []Connection
}

func (c *Client) Connections() ([]Connection, error) {
	snapshot, err := c.ConnectionsSnapshot()
	return snapshot.Connections, err
}

// ConnectionsSnapshot obtains counters and records at the same instant.
func (c *Client) ConnectionsSnapshot() (ConnectionSnapshot, error) {
	b, err := c.do(http.MethodGet, "/connections", nil)
	if err != nil {
		return ConnectionSnapshot{}, err
	}
	var resp struct {
		UploadTotal   int64 `json:"uploadTotal"`
		DownloadTotal int64 `json:"downloadTotal"`
		Connections   []struct {
			ID       string `json:"id"`
			Metadata struct {
				Host            string `json:"host"`
				DestinationIP   string `json:"destinationIP"`
				DestinationPort string `json:"destinationPort"`
				Network         string `json:"network"`
				Process         string `json:"process"`
			} `json:"metadata"`
			Chains   []string  `json:"chains"`
			Rule     string    `json:"rule"`
			Upload   int64     `json:"upload"`
			Download int64     `json:"download"`
			Start    time.Time `json:"start"`
		} `json:"connections"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return ConnectionSnapshot{}, err
	}
	out := make([]Connection, 0, len(resp.Connections))
	for _, cn := range resp.Connections {
		host := cn.Metadata.Host
		if host == "" {
			host = cn.Metadata.DestinationIP
		}
		if cn.Metadata.DestinationPort != "" {
			host = net.JoinHostPort(host, cn.Metadata.DestinationPort)
		}
		out = append(out, Connection{
			ID: cn.ID, Target: host, Network: cn.Metadata.Network,
			Process: cn.Metadata.Process, Chains: cn.Chains,
			Rule: cn.Rule, Upload: cn.Upload, Download: cn.Download, Start: cn.Start,
		})
	}
	return ConnectionSnapshot{resp.UploadTotal, resp.DownloadTotal, out}, nil
}

// CloseConnection terminates one live connection by ID.
func (c *Client) CloseConnection(id string) error {
	_, err := c.do(http.MethodDelete, "/connections/"+url.PathEscape(id), nil)
	return err
}

// CloseAllConnections terminates every live connection.
func (c *Client) CloseAllConnections() error {
	_, err := c.do(http.MethodDelete, "/connections", nil)
	return err
}

// DNSAnswer is one record from a core DNS query.
type DNSAnswer struct {
	Name string
	Type uint16
	TTL  uint32
	Data string
}

// DNSQuery asks the core's resolver for name (type A, AAAA, CNAME…)
// through GET /dns/query. Mihomo answers with a dns.Msg-shaped JSON
// document whose answer list key has been seen both capitalised and
// not, so both spellings are decoded.
func (c *Client) DNSQuery(name, qtype string) ([]DNSAnswer, error) {
	q := url.Values{"name": {name}, "type": {qtype}}
	b, err := c.do(http.MethodGet, "/dns/query?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	// DNSAnswer's fields arrive capitalised from the dns library.
	type rawAnswer struct {
		Name string `json:"name"`
		Type uint16 `json:"type"`
		TTL  uint32 `json:"TTL"`
		Data string `json:"data"`
	}
	var raw struct {
		Answer  []rawAnswer `json:"Answer"`
		Answer2 []rawAnswer `json:"answer"`
	}
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	list := raw.Answer
	if len(list) == 0 {
		list = raw.Answer2
	}
	out := make([]DNSAnswer, 0, len(list))
	for _, a := range list {
		out = append(out, DNSAnswer{Name: a.Name, Type: a.Type, TTL: a.TTL, Data: a.Data})
	}
	return out, nil
}

// Ping measures one controller round-trip (GET /version).
func (c *Client) Ping() (time.Duration, error) {
	t0 := time.Now()
	if _, err := c.do(http.MethodGet, "/version", nil); err != nil {
		return 0, err
	}
	return time.Since(t0), nil
}

// UpdateProvider re-fetches one http provider in the running core.
func (c *Client) UpdateProvider(name string) error {
	_, err := c.do(http.MethodPut, "/providers/proxies/"+url.PathEscape(name), nil)
	return err
}
