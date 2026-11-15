package core

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
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
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, c.Base+path, rdr)
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
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return b, fmt.Errorf("%s %s: %s", method, path, resp.Status)
	}
	return b, nil
}

// WaitReady polls /version until the core answers or the deadline hits.
func (c *Client) WaitReady(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	var lastErr error
	for time.Now().Before(deadline) {
		if _, lastErr = c.do(http.MethodGet, "/version", nil); lastErr == nil {
			return nil
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("core did not become ready: %w", lastErr)
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
	b, err := c.do(http.MethodGet, "/proxies", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Proxies map[string]Proxy `json:"proxies"`
	}
	if err := json.Unmarshal(b, &resp); err != nil {
		return nil, err
	}
	out := resp.Proxies
	pb, err := c.do(http.MethodGet, "/providers/proxies", nil)
	if err != nil {
		return out, nil // provider view is best-effort
	}
	var presp struct {
		Providers map[string]struct {
			Proxies []Proxy `json:"proxies"`
		} `json:"providers"`
	}
	if json.Unmarshal(pb, &presp) == nil {
		for _, prov := range presp.Providers {
			for _, p := range prov.Proxies {
				if _, exists := out[p.Name]; !exists && p.Name != "" {
					out[p.Name] = p
				}
			}
		}
	}
	return out, nil
}

// Providers returns the proxy list of every provider, keyed by
// provider name — the per-subscription node pools the nodes page is
// organized by (a provider and a subscription share a name).
func (c *Client) Providers() (map[string][]Proxy, error) {
	b, err := c.do(http.MethodGet, "/providers/proxies", nil)
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
	q := url.Values{"url": {testURL}, "timeout": {fmt.Sprint(timeoutMs)}}
	b, err := c.do(http.MethodGet, "/proxies/"+url.PathEscape(name)+"/delay?"+q.Encode(), nil)
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

func (c *Client) Connections() ([]Connection, error) {
	b, err := c.do(http.MethodGet, "/connections", nil)
	if err != nil {
		return nil, err
	}
	var resp struct {
		Connections []struct {
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
		return nil, err
	}
	out := make([]Connection, 0, len(resp.Connections))
	for _, cn := range resp.Connections {
		host := cn.Metadata.Host
		if host == "" {
			host = cn.Metadata.DestinationIP
		}
		if cn.Metadata.DestinationPort != "" {
			host += ":" + cn.Metadata.DestinationPort
		}
		out = append(out, Connection{
			ID: cn.ID, Target: host, Network: cn.Metadata.Network,
			Process: cn.Metadata.Process, Chains: cn.Chains,
			Rule: cn.Rule, Upload: cn.Upload, Download: cn.Download, Start: cn.Start,
		})
	}
	return out, nil
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
