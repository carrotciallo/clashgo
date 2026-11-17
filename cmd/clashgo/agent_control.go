package main

// Control socket: the running instance serves newline-delimited JSON
// requests on a same-user Unix socket (0600). The CLI (agent_cli.go)
// is the client. One request per connection:
//
//	{"cmd":"mode.set","args":{"mode":"tun"}}
//	{"ok":true,"result":{...}}
//	{"ok":false,"error":{"code":"invalid_mode","message":"..."}}
//
// Mutating commands run through the app's own serialized operation
// paths (operate/opMu and the existing apply/rollback machinery) —
// never by editing the app's files from outside.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

type ctrlError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *ctrlError) Error() string { return e.Code + ": " + e.Message }

func ctrlErr(code, format string, args ...any) *ctrlError {
	return &ctrlError{Code: code, Message: fmt.Sprintf(format, args...)}
}

type ctrlRequest struct {
	Cmd  string          `json:"cmd"`
	Args json.RawMessage `json:"args,omitempty"`
}

type ctrlResponse struct {
	OK     bool       `json:"ok"`
	Result any        `json:"result,omitempty"`
	Error  *ctrlError `json:"error,omitempty"`
}

// controlSocketPath is shared by the server and the CLI. The runtime
// dir is preferred (tmpfs, session-scoped); the state dir is the
// fallback when XDG_RUNTIME_DIR is unset.
func controlSocketPath(store *config.Store) string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "clashgo", "control.sock")
	}
	return filepath.Join(store.StateDir, "control.sock")
}

func (a *App) serveControl() {
	path := controlSocketPath(a.store)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return
	}
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return
	}
	_ = os.Chmod(path, 0o600)
	defer ln.Close()
	defer os.Remove(path)
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go a.handleControl(conn)
	}
}

func (a *App) handleControl(conn net.Conn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(240 * time.Second))
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 64*1024)
	if !sc.Scan() {
		return
	}
	var req ctrlRequest
	if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
		writeCtrlResponse(conn, ctrlResponse{OK: false, Error: ctrlErr("bad_request", "请求不是合法 JSON")})
		return
	}
	result, cerr, after := a.dispatchControl(req)
	resp := ctrlResponse{OK: cerr == nil, Result: result}
	if cerr != nil {
		resp.Error = cerr
	}
	writeCtrlResponse(conn, resp)
	if after != nil {
		after()
	}
}

func writeCtrlResponse(conn net.Conn, resp ctrlResponse) {
	data, err := json.Marshal(resp)
	if err != nil {
		return
	}
	_, _ = conn.Write(append(data, '\n'))
}

// dispatchControl returns the result, an optional structured error,
// and an optional action to run after the response has been written
// (used by app.quit, which must answer before it tears down).
func (a *App) dispatchControl(req ctrlRequest) (any, *ctrlError, func()) {
	if a.quitting.Load() && req.Cmd != "status" {
		return nil, ctrlErr("quitting", "应用正在退出"), nil
	}
	mutating := true
	var (
		result any
		cerr   *ctrlError
		after  func()
	)
	switch req.Cmd {
	case "status":
		mutating = false
		result = a.buildStatus()
	case "node.list":
		mutating = false
		result = a.nodeList()
	case "sub.list":
		mutating = false
		result = a.subList()
	case "mode.set":
		result, cerr = a.ctrlModeSet(req.Args)
	case "route.set":
		result, cerr = a.ctrlRouteSet(req.Args)
	case "node.select":
		result, cerr = a.ctrlNodeSelect(req.Args)
	case "sub.refresh":
		result, cerr = a.ctrlSubRefresh(req.Args)
	case "core.restart":
		result, cerr = a.ctrlCoreRestart()
	case "core.stop":
		result, cerr = a.ctrlCoreStop()
	case "app.quit":
		result = map[string]any{"quitting": true}
		after = func() {
			time.Sleep(150 * time.Millisecond)
			a.requestQuit()
		}
	default:
		return nil, ctrlErr("unknown_command", "未知命令 %s", req.Cmd), nil
	}
	if mutating {
		fields := map[string]any{"cmd": req.Cmd, "ok": cerr == nil}
		if cerr != nil {
			fields["code"] = cerr.Code
		}
		a.emitEvent("control_op", fields)
		a.writeStatusFile()
	}
	return result, cerr, after
}

func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if time.Now().After(deadline) {
			return cond()
		}
		time.Sleep(200 * time.Millisecond)
	}
}

func (a *App) currentMsg() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.msg
}

func decodeArgs(raw json.RawMessage, dst any) *ctrlError {
	if len(raw) == 0 {
		return nil
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return ctrlErr("bad_request", "参数不合法：%v", err)
	}
	return nil
}

func (a *App) ctrlModeSet(raw json.RawMessage) (any, *ctrlError) {
	var args struct {
		Mode string `json:"mode"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	switch args.Mode {
	case "tun", "system":
		a.setCapture(args.Mode == "tun")
		wantTun := args.Mode == "tun"
		ok := waitFor(60*time.Second, func() bool {
			if a.settings().TUNEnabled != wantTun || !a.coreHealthy() {
				return false
			}
			a.mu.Lock()
			active := a.capture.Active
			a.mu.Unlock()
			if !active {
				return false
			}
			if !wantTun && !a.proxyManager().Armed() {
				return false
			}
			return true
		})
		if !ok {
			return nil, ctrlErr("op_timeout", "切换接管方式未在限时内就绪：%s", a.currentMsg())
		}
	case "off":
		a.operate(func() { a.stopCoreNow() })
		if !waitFor(20*time.Second, func() bool { return !a.sup.Running() }) {
			return nil, ctrlErr("op_timeout", "核心未在限时内停止")
		}
	default:
		return nil, ctrlErr("invalid_mode", "接管方式只接受 tun、system、off")
	}
	return a.buildStatus(), nil
}

func (a *App) ctrlRouteSet(raw json.RawMessage) (any, *ctrlError) {
	var args struct {
		Mode string `json:"mode"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	switch args.Mode {
	case "rule", "global", "direct":
	default:
		return nil, ctrlErr("invalid_mode", "出站模式只接受 rule、global、direct")
	}
	a.setMode(args.Mode)
	if !waitFor(10*time.Second, func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.mode == args.Mode
	}) {
		return nil, ctrlErr("op_failed", "切换出站模式失败：%s", a.currentMsg())
	}
	return a.buildStatus(), nil
}

// nodeList returns the picker groups and per-subscription pools with
// the current selections — the read side of node selection.
func (a *App) nodeList() map[string]any {
	a.mu.Lock()
	defer a.mu.Unlock()
	groups := []map[string]any{}
	for _, name := range sortedKeys(a.proxies) {
		p := a.proxies[name]
		if len(p.All) == 0 {
			continue
		}
		groups = append(groups, map[string]any{
			"name": name, "now": p.Now, "nodes": p.All,
		})
	}
	pools := map[string][]string{}
	for _, name := range sortedKeys(a.providers) {
		names := []string{}
		for _, p := range a.providers[name] {
			names = append(names, p.Name)
		}
		pools[name] = names
	}
	return map[string]any{"groups": groups, "pools": pools}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// nodeReachable mirrors useNode's reachability rule: the name is a
// direct member of the picker group, or a member of a group the
// picker can select.
func (a *App) nodeReachable(name string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	picker, ok := a.proxies["节点选择"]
	if !ok {
		return false
	}
	contains := func(list []string, s string) bool {
		for _, v := range list {
			if v == s {
				return true
			}
		}
		return false
	}
	if contains(picker.All, name) {
		return true
	}
	for gname, g := range a.proxies {
		if gname == "节点选择" || len(g.All) == 0 {
			continue
		}
		if contains(g.All, name) && contains(picker.All, gname) {
			return true
		}
	}
	return false
}

func (a *App) ctrlNodeSelect(raw json.RawMessage) (any, *ctrlError) {
	var args struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	if strings.TrimSpace(args.Name) == "" {
		return nil, ctrlErr("bad_request", "缺少节点名")
	}
	if !a.coreHealthy() {
		return nil, ctrlErr("core_down", "核心未运行，无法切换节点")
	}
	if !a.nodeReachable(args.Name) {
		return nil, ctrlErr("node_not_reachable", "节点 %s 不在可达分组里", args.Name)
	}
	a.useNode(args.Name)
	if !waitFor(30*time.Second, func() bool {
		chain := a.egressChain()
		return len(chain) > 0 && chain[len(chain)-1] == args.Name
	}) {
		return nil, ctrlErr("op_failed", "切换节点失败：%s", a.currentMsg())
	}
	return a.buildStatus(), nil
}

func (a *App) subList() map[string]any {
	return map[string]any{"subscriptions": sanitizeSubs(a.reg.Snapshot())}
}

func (a *App) ctrlSubRefresh(raw json.RawMessage) (any, *ctrlError) {
	var args struct {
		Name string `json:"name"`
	}
	if err := decodeArgs(raw, &args); err != nil {
		return nil, err
	}
	var targets []string
	if args.Name != "" {
		sub := a.reg.Find(args.Name)
		if sub == nil {
			return nil, ctrlErr("sub_not_found", "订阅 %s 不存在", args.Name)
		}
		if sub.Local {
			return nil, ctrlErr("sub_local", "本地订阅没有可刷新的地址")
		}
		targets = []string{args.Name}
	} else {
		for _, s := range a.reg.Snapshot() {
			if !s.Local {
				targets = append(targets, s.Name)
			}
		}
	}
	results := []map[string]any{}
	for _, name := range targets {
		before := a.reg.Find(name)
		a.refreshSubscription(name)
		name := name
		waitFor(100*time.Second, func() bool {
			cur := a.reg.Find(name)
			if cur == nil {
				return true
			}
			a.mu.Lock()
			busy := a.busy
			a.mu.Unlock()
			if busy {
				return false
			}
			return !cur.UpdatedAt.Equal(before.UpdatedAt) || cur.LastError != before.LastError
		})
		cur := a.reg.Find(name)
		res := map[string]any{"name": name}
		if cur != nil {
			res["nodes"] = cur.NodeCount
			res["ok"] = cur.LastError == ""
			if cur.LastError != "" {
				res["error"] = cur.LastError
			}
		}
		results = append(results, res)
	}
	return map[string]any{"refreshed": results}, nil
}

func (a *App) ctrlCoreRestart() (any, *ctrlError) {
	started := time.Now()
	a.restartCore()
	if !waitFor(60*time.Second, func() bool {
		if !a.sup.Running() || !a.coreHealthy() {
			return false
		}
		a.mu.Lock()
		defer a.mu.Unlock()
		return a.coreSince.After(started.Add(-time.Second))
	}) {
		return nil, ctrlErr("op_timeout", "核心未在限时内恢复健康：%s", a.currentMsg())
	}
	return a.buildStatus(), nil
}

func (a *App) ctrlCoreStop() (any, *ctrlError) {
	a.operate(func() { a.stopCoreNow() })
	if !waitFor(20*time.Second, func() bool { return !a.sup.Running() }) {
		return nil, ctrlErr("op_timeout", "核心未在限时内停止")
	}
	return a.buildStatus(), nil
}
