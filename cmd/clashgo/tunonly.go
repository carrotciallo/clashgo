package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/carrotciallo/clashgo/internal/config"
)

// Tun-only mode keeps the machine's egress on the TUN device: the
// tunonly-helper (CAP_NET_ADMIN, fixed ruleset) installs an nftables
// table that drops whatever leaves the uplink outside the core's marked
// sockets, the proxy node, LAN and DHCP. The setting lives in the app
// store. There is no daemon and no latched state: turning the setting
// off — or deleting the table — restores normal egress, and LAN access
// is never touched.

func tunOnlyHelperPath() string {
	if p := os.Getenv("CLASHGO_TUNONLY_HELPER"); p != "" {
		return p
	}
	return "/usr/local/lib/clashgo/tunonly-helper"
}

func runTunOnlyHelper(arg string) (string, error) {
	out, err := exec.Command(tunOnlyHelperPath(), arg).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("tunonly-helper %s: %v: %s", arg, err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func (a *App) tunOnlyApplied() bool {
	out, err := runTunOnlyHelper("status")
	return err == nil && out == "on"
}

func (a *App) tunOnlyStatus() map[string]any {
	return map[string]any{"setting": a.settings().TunOnly, "applied": a.tunOnlyApplied()}
}

func (a *App) setTunOnly(on bool) error {
	if on && !a.settings().TUNEnabled {
		return fmt.Errorf("仅 TUN 出网需要先开启 TUN 模式")
	}
	arg, back := "off", "on"
	if on {
		arg, back = "on", "off"
	}
	if _, err := runTunOnlyHelper(arg); err != nil {
		return err
	}
	if err := a.changeSettings(func(s *config.Settings) { s.TunOnly = on }); err != nil {
		_, _ = runTunOnlyHelper(back) // keep table and setting from diverging
		return err
	}
	a.emitEvent("tunonly_changed", map[string]any{"on": on})
	return nil
}

func (a *App) tunOnlyAction(op string) (any, *ctrlError) {
	switch op {
	case "status":
		return a.tunOnlyStatus(), nil
	case "on", "off":
		if err := a.setTunOnly(op == "on"); err != nil {
			return nil, ctrlErr("tunonly_failed", "%v", err)
		}
		return a.tunOnlyStatus(), nil
	}
	return nil, ctrlErr("unknown_command", "未知 tunonly 操作 %s", op)
}

// applyTunOnlyAtStart re-applies the table when the persisted setting
// says the mode was on. A failure is surfaced, never silently ignored.
func (a *App) applyTunOnlyAtStart() {
	if !a.settings().TunOnly {
		return
	}
	if _, err := runTunOnlyHelper("on"); err != nil {
		a.emitEvent("tunonly_failed", map[string]any{"error": err.Error()})
		a.setMsg("仅 TUN 出网恢复失败：%v", err)
		return
	}
	a.emitEvent("tunonly_changed", map[string]any{"on": true})
}

func cliTunOnly(store *config.Store, args []string) int {
	f := parseCLIFlags(args)
	if len(f.pos) == 1 {
		switch f.pos[0] {
		case "on", "off", "status":
			return cliCall(store, ctrlRequest{Cmd: "tunonly." + f.pos[0]}, f)
		}
	}
	printAgentError("usage", "用法：clashgo tunonly on|off|status")
	return exitUsage
}
