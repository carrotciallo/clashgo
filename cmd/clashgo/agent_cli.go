package main

// CLI mode: `clashgo <command> ...` runs headless and exits, instead
// of launching the GUI. Read commands degrade gracefully without a
// live instance (disk status, then offline reconstruction); mutating
// commands go through the running instance's control socket and fail
// with a structured not_running error when there is none.
//
// Output contract: stdout is JSON by default; --human switches the
// read commands to a short text form. Errors are JSON on stdout too,
// with the exit codes documented in AGENTS.md.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

const (
	exitOK         = 0
	exitFailed     = 1
	exitUsage      = 2
	exitNoInstance = 3
	exitNeedsYes   = 4
)

var agentCommands = map[string]bool{
	"status": true, "doctor": true, "mode": true, "route": true,
	"node": true, "sub": true, "core": true, "logs": true,
	"watch": true, "quit": true, "help": true,
}

// runAgentCLI reports whether the invocation was a CLI command, and
// the exit code to use. Called at the top of main, before any GUI or
// single-instance machinery.
func runAgentCLI(args []string) (int, bool) {
	if len(args) == 0 || !agentCommands[args[0]] {
		return 0, false
	}
	store, err := config.NewStore()
	if err != nil {
		printAgentError("internal", err.Error())
		return exitFailed, true
	}
	switch args[0] {
	case "help":
		printAgentHelp()
		return exitOK, true
	case "status":
		return cliStatus(store, args[1:]), true
	case "doctor":
		return cliDoctor(store, args[1:]), true
	case "mode", "route", "node", "sub", "core", "quit":
		return cliOp(store, args[0], args[1:]), true
	case "logs":
		return cliLogs(store, args[1:]), true
	case "watch":
		return cliWatch(store), true
	}
	return exitUsage, true
}

func printAgentHelp() {
	fmt.Println(`clashgo — agent CLI (schema v1, see AGENTS.md)

  status [--human]            实例/核心/接管/节点/订阅状态
  doctor [--human]            验收电池：逐项 pass/fail/skip
  mode get                    当前接管方式
  mode set tun|system|off [--yes] [--dry-run]
  route get                   当前出站模式
  route set rule|global|direct [--dry-run]
  node list [--human]         分组与节点池
  node select <name> [--dry-run]
  sub list [--human]          订阅（不含 URL）
  sub refresh [name]          刷新一个或全部订阅
  core restart                重启核心
  core stop [--yes]           停止核心并解除接管
  quit [--yes]                退出应用
  logs [--since 2h] [--human] 结构化事件日志
  watch                       流式输出状态变更与事件（Ctrl-C 结束）`)
}

type cliFlags struct {
	pos   []string
	human bool
	yes   bool
	dry   bool
	since string
}

func parseCLIFlags(args []string) cliFlags {
	var f cliFlags
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--human":
			f.human = true
		case "--yes":
			f.yes = true
		case "--dry-run":
			f.dry = true
		case "--since":
			if i+1 < len(args) {
				i++
				f.since = args[i]
			}
		default:
			f.pos = append(f.pos, args[i])
		}
	}
	return f
}

func printJSON(v any) {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		fmt.Println(`{"ok":false,"error":{"code":"internal","message":"marshal failed"}}`)
		return
	}
	fmt.Println(string(data))
}

func printAgentError(code, message string) {
	printJSON(map[string]any{"ok": false, "error": map[string]any{"code": code, "message": message}})
}

// ---- control socket client ----

type ctrlClientResponse struct {
	OK     bool            `json:"ok"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *ctrlError      `json:"error,omitempty"`
}

func callControl(store *config.Store, req ctrlRequest) (ctrlClientResponse, error) {
	var resp ctrlClientResponse
	conn, err := net.DialTimeout("unix", controlSocketPath(store), 2*time.Second)
	if err != nil {
		return resp, err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(240 * time.Second))
	data, err := json.Marshal(req)
	if err != nil {
		return resp, err
	}
	if _, err := conn.Write(append(data, '\n')); err != nil {
		return resp, err
	}
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 64*1024), 4*1024*1024)
	if !sc.Scan() {
		return resp, fmt.Errorf("控制口无应答")
	}
	if err := json.Unmarshal(sc.Bytes(), &resp); err != nil {
		return resp, err
	}
	return resp, nil
}

// cliFetchStatus returns the freshest status available: the live
// instance via the control socket when it answers, else the disk
// snapshot (marked stale), else an offline reconstruction.
func cliFetchStatus(store *config.Store) (agentStatus, bool) {
	if resp, err := callControl(store, ctrlRequest{Cmd: "status"}); err == nil && resp.OK {
		var st agentStatus
		if err := json.Unmarshal(resp.Result, &st); err == nil {
			return st, true
		}
	}
	if st, ok := readStatusFile(store); ok {
		st.Stale = true
		st.Instance.Running = false
		return st, false
	}
	return offlineStatus(store), false
}

func cliStatus(store *config.Store, args []string) int {
	f := parseCLIFlags(args)
	st, live := cliFetchStatus(store)
	if f.human {
		fmt.Printf("实例：%s（pid %d）  核心：%s %s\n", map[bool]string{true: "运行中", false: "未运行"}[live], st.Instance.PID,
			map[bool]string{true: "健康", false: "未运行/异常"}[st.Core.Healthy], st.Core.Version)
		fmt.Printf("接管：%s（就绪 %v）  出站：%s  节点：%s\n", st.Capture.Mode, st.Capture.Ready, st.Routing.Mode, st.Selection.Node)
		fmt.Printf("出口：%s %s  订阅：%d 个\n", st.Egress.IP, st.Egress.Org, len(st.Subscriptions))
		if st.Stale {
			fmt.Println("（状态来自磁盘快照，可能已过期）")
		}
		return exitOK
	}
	printJSON(st)
	return exitOK
}

func cliDoctor(store *config.Store, args []string) int {
	f := parseCLIFlags(args)
	rep := runDoctor(store)
	if f.human {
		for _, c := range rep.Checks {
			mark := map[string]string{"pass": "✓", "fail": "✗", "skip": "–"}[c.Status]
			fmt.Printf("%s %-20s %s\n", mark, c.Name, c.Detail)
		}
		fmt.Println(map[bool]string{true: "全部通过", false: "有失败项"}[rep.OK])
	} else {
		printJSON(rep)
	}
	if !rep.OK {
		return exitFailed
	}
	return exitOK
}

func cliLogs(store *config.Store, args []string) int {
	f := parseCLIFlags(args)
	var since time.Time
	if f.since != "" {
		if d, err := time.ParseDuration(f.since); err == nil {
			since = time.Now().Add(-d)
		} else if t, err := time.Parse(time.RFC3339, f.since); err == nil {
			since = t
		} else {
			printAgentError("bad_request", "--since 只接受时长（如 2h）或 RFC3339 时间")
			return exitUsage
		}
	}
	for _, rec := range readEvents(store.LogsDir()) {
		if !since.IsZero() {
			if ts, _ := rec["time"].(string); ts != "" {
				if t, err := time.Parse(time.RFC3339Nano, ts); err == nil && t.Before(since) {
					continue
				}
			}
		}
		if f.human {
			kind, _ := rec["kind"].(string)
			ts, _ := rec["time"].(string)
			rest := []string{}
			for k, v := range rec {
				if k != "kind" && k != "time" {
					rest = append(rest, fmt.Sprintf("%s=%v", k, v))
				}
			}
			fmt.Printf("%s %-18s %s\n", ts, kind, strings.Join(rest, " "))
		} else {
			data, _ := json.Marshal(rec)
			fmt.Println(string(data))
		}
	}
	return exitOK
}

func cliWatch(store *config.Store) int {
	enc := json.NewEncoder(os.Stdout)
	lastStatus := ""
	seenEvents := 0
	for {
		st, _ := cliFetchStatus(store)
		st.Time = time.Time{}
		st.Instance.Heartbeat = time.Time{}
		data, _ := json.Marshal(st)
		if string(data) != lastStatus {
			lastStatus = string(data)
			_ = enc.Encode(map[string]any{"type": "status", "status": st})
		}
		events := readEvents(store.LogsDir())
		if len(events) < seenEvents {
			seenEvents = 0 // rotated
		}
		for ; seenEvents < len(events); seenEvents++ {
			_ = enc.Encode(map[string]any{"type": "event", "event": events[seenEvents]})
		}
		time.Sleep(time.Second)
	}
}
