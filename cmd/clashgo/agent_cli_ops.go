package main

// Mutating CLI commands. All of them are thin clients of the control
// socket: parse, gate (--yes / --dry-run), send one request, print
// the structured result. The instance does the work through its own
// serialized paths.

import (
	"encoding/json"
	"fmt"

	"github.com/carrotciallo/clashgo/internal/config"
)

// needsYes marks commands that interrupt service; they refuse to run
// without an explicit --yes so an agent cannot stop the user's proxy
// by accident.
func opNeedsYes(cmd string, f cliFlags) bool {
	switch cmd {
	case "quit":
		return true
	case "core":
		return len(f.pos) > 0 && f.pos[0] == "stop"
	case "mode":
		return len(f.pos) > 1 && f.pos[0] == "set" && f.pos[1] == "off"
	}
	return false
}

func cliOp(store *config.Store, cmd string, args []string) int {
	f := parseCLIFlags(args)

	// Read-only subcommands first.
	if cmd == "mode" && (len(f.pos) == 0 || f.pos[0] == "get") {
		st, _ := cliFetchStatus(store)
		printJSON(map[string]any{"mode": st.Capture.Mode, "tunEnabled": st.Capture.TUNEnabled, "systemProxyArmed": st.Capture.SystemProxyArmed, "ready": st.Capture.Ready})
		return exitOK
	}
	if cmd == "route" && (len(f.pos) == 0 || f.pos[0] == "get") {
		st, _ := cliFetchStatus(store)
		printJSON(map[string]any{"mode": st.Routing.Mode})
		return exitOK
	}
	if cmd == "node" && len(f.pos) > 0 && f.pos[0] == "list" {
		return cliCall(store, ctrlRequest{Cmd: "node.list"}, f)
	}
	if cmd == "sub" && len(f.pos) > 0 && f.pos[0] == "list" {
		return cliCall(store, ctrlRequest{Cmd: "sub.list"}, f)
	}

	if opNeedsYes(cmd, f) && !f.yes && !f.dry {
		printAgentError("confirmation_required", "该操作会中断代理服务，需显式加 --yes")
		return exitNeedsYes
	}

	var req ctrlRequest
	switch cmd {
	case "mode":
		if len(f.pos) == 2 && f.pos[0] == "set" {
			req = ctrlRequest{Cmd: "mode.set", Args: mustArgs(map[string]any{"mode": f.pos[1]})}
		}
	case "route":
		if len(f.pos) == 2 && f.pos[0] == "set" {
			req = ctrlRequest{Cmd: "route.set", Args: mustArgs(map[string]any{"mode": f.pos[1]})}
		}
	case "node":
		if len(f.pos) == 2 && f.pos[0] == "select" {
			req = ctrlRequest{Cmd: "node.select", Args: mustArgs(map[string]any{"name": f.pos[1]})}
		}
	case "sub":
		if len(f.pos) >= 1 && f.pos[0] == "refresh" {
			m := map[string]any{}
			if len(f.pos) == 2 {
				m["name"] = f.pos[1]
			}
			req = ctrlRequest{Cmd: "sub.refresh", Args: mustArgs(m)}
		}
	case "core":
		if len(f.pos) == 1 && f.pos[0] == "restart" {
			req = ctrlRequest{Cmd: "core.restart"}
		} else if len(f.pos) == 1 && f.pos[0] == "stop" {
			req = ctrlRequest{Cmd: "core.stop"}
		}
	case "quit":
		req = ctrlRequest{Cmd: "app.quit"}
	}
	if req.Cmd == "" {
		printAgentError("bad_request", fmt.Sprintf("用法见 clashgo help（%s 子命令或参数不对）", cmd))
		return exitUsage
	}

	if f.dry {
		return cliDryRun(store, req)
	}
	return cliCall(store, req, f)
}

func mustArgs(m map[string]any) json.RawMessage {
	data, _ := json.Marshal(m)
	return data
}

// cliCall sends one control request and prints the outcome: the
// result payload wrapped with ok:true, or the structured error.
func cliCall(store *config.Store, req ctrlRequest, f cliFlags) int {
	resp, err := callControl(store, req)
	if err != nil {
		printAgentError("not_running", "没有可达的 ClashGo 实例（控制口连接失败）")
		return exitNoInstance
	}
	if !resp.OK {
		if resp.Error != nil {
			printJSON(map[string]any{"ok": false, "error": resp.Error})
		} else {
			printAgentError("internal", "实例返回了无结构失败")
		}
		return exitFailed
	}
	if f.human && req.Cmd == "node.list" {
		var nl struct {
			Groups []struct {
				Name  string   `json:"name"`
				Now   string   `json:"now"`
				Nodes []string `json:"nodes"`
			} `json:"groups"`
			Pools map[string][]string `json:"pools"`
		}
		if err := json.Unmarshal(resp.Result, &nl); err == nil {
			for _, g := range nl.Groups {
				fmt.Printf("[%s] 当前：%s（%d 个）\n", g.Name, g.Now, len(g.Nodes))
			}
			for name, nodes := range nl.Pools {
				fmt.Printf("订阅 %s：%d 个节点\n", name, len(nodes))
			}
			return exitOK
		}
	}
	if f.human && req.Cmd == "sub.list" {
		var sl struct {
			Subscriptions []agentSubStatus `json:"subscriptions"`
		}
		if err := json.Unmarshal(resp.Result, &sl); err == nil {
			for _, s := range sl.Subscriptions {
				fmt.Printf("%s：%d 个节点，更新于 %s %s\n", s.Name, s.Nodes, s.UpdatedAt.Format("01-02 15:04"), s.LastError)
			}
			return exitOK
		}
	}
	var result any
	if len(resp.Result) > 0 {
		_ = json.Unmarshal(resp.Result, &result)
	}
	printJSON(map[string]any{"ok": true, "result": result})
	return exitOK
}

// cliDryRun validates a mutating request against the live state and
// reports what would change, without sending the mutation.
func cliDryRun(store *config.Store, req ctrlRequest) int {
	st, live := cliFetchStatus(store)
	if !live {
		printAgentError("not_running", "没有可达的 ClashGo 实例，无法演算")
		return exitNoInstance
	}
	var args map[string]any
	if len(req.Args) > 0 {
		_ = json.Unmarshal(req.Args, &args)
	}
	plan := map[string]any{"ok": true, "dryRun": true, "command": req.Cmd, "args": args}
	switch req.Cmd {
	case "mode.set":
		plan["current"] = st.Capture.Mode
		plan["wouldChange"] = args["mode"] != st.Capture.Mode
	case "route.set":
		plan["current"] = st.Routing.Mode
		plan["wouldChange"] = args["mode"] != st.Routing.Mode
	case "node.select":
		resp, err := callControl(store, ctrlRequest{Cmd: "node.list"})
		if err != nil || !resp.OK {
			printAgentError("not_running", "无法读取节点列表")
			return exitNoInstance
		}
		var policy struct{ SelectionError *ctrlError }
		if err := json.Unmarshal(resp.Result, &policy); err != nil {
			printAgentError("bad_response", "无法核实节点选择策略")
			return exitFailed
		}
		if e := policy.SelectionError; e != nil {
			printAgentError(e.Code, e.Message)
			return exitFailed
		}
		plan["current"] = st.Selection.Node
		plan["wouldChange"] = args["name"] != st.Selection.Node
		plan["nodeListChecked"] = true
	case "core.stop", "app.quit":
		plan["current"] = st.Capture.Mode
		plan["wouldChange"] = true
	}
	printJSON(plan)
	return exitOK
}
