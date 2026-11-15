// ClashGo — a lightweight desktop client for the mihomo core.
// Phase 1 (MVP): user-owned master config, subscriptions as node pools,
// system-proxy mode with crash self-heal, tray + main window.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/subs"
	"github.com/carrotciallo/clashgo/internal/sysproxy"
	"github.com/egoist/mygo"
)

func fatal(format string, args ...any) {
	log.Printf("clashgo: "+format, args...)
	os.Exit(1)
}

// detachFromTerminal re-executes the app in a new session when it
// was launched from an interactive terminal, so closing that
// terminal does not take the app (and the core) down with it via
// SIGHUP. Launches without a controlling terminal (systemd units,
// the compositor's autostart, desktop entries) are already
// detached and pass through untouched. CLASHGO_NO_DETACH=1 keeps
// the foreground behavior for debugging; CLASHGO_DETACHED=1 marks
// the re-executed child.
func detachFromTerminal() {
	if os.Getenv("CLASHGO_DETACHED") == "1" || os.Getenv("CLASHGO_NO_DETACH") == "1" {
		return
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return // no controlling terminal: nothing to detach from
	}
	tty.Close()
	exe, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Env = append(os.Environ(), "CLASHGO_DETACHED=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return // could not re-exec: run in the terminal after all
	}
	os.Exit(0)
}

func main() {
	detachFromTerminal()
	log.SetFlags(log.Ltime)
	store, err := config.NewStore()
	if err != nil {
		fatal("store: %v", err)
	}
	instance, err := config.AcquireInstance(store.ConfigDir)
	if err != nil {
		fatal("instance: %v", err)
	}
	defer instance.Close()
	if err := store.EnsureLayout(); err != nil {
		fatal("layout: %v", err)
	}
	set, err := store.LoadSettings()
	if err != nil {
		fatal("settings: %v", err)
	}
	// Resolve the active profile: the persisted choice, falling back
	// to the default profile, then to the built-in master config.
	active := set.ActiveProfile
	if active != "" && !store.ProfileExists(active) {
		active = set.DefaultProfile
	}
	if active != "" && !store.ProfileExists(active) {
		active = ""
	}
	if active != set.ActiveProfile {
		set.ActiveProfile = active
		_ = store.SaveSettings(set)
	}
	store.SetActiveProfile(active)
	reg, err := subs.Load(store.SubsPath())
	if err != nil {
		fatal("subscriptions: %v", err)
	}
	coreBin := core.FindCore(set.CorePath)
	if coreBin == "" {
		fatal("找不到 mihomo 核心（设置 corePath 或把 mihomo 放进 PATH）")
	}

	autostart := false
	for _, arg := range os.Args[1:] {
		if arg == "--autostart" {
			autostart = true
		}
	}
	a := &App{
		work:  core.NewTasks(context.Background()),
		store: store, set: set, reg: reg, coreBin: coreBin,
		delays:          map[string]map[string]int{},
		setArm:          set.ArmProxyOnStart,
		setCorePath:     set.CorePath,
		autostartLaunch: autostart,
		silentSaved:     set.SilentAutostart,
	}
	a.collapsedSubs = map[string]bool{}
	for _, n := range set.CollapsedSubs {
		a.collapsedSubs[n] = true
	}
	a.setMixed = fmt.Sprint(set.MixedPort)
	a.setCtrl = fmt.Sprint(set.ControllerPort)
	a.sup = &core.Supervisor{
		BinPath: coreBin,
		HomeDir: store.RuntimeDir(),
		LogPath: filepath.Join(store.LogsDir(), "core.log"),
	}
	a.cli = core.NewClient(fmt.Sprintf("http://127.0.0.1:%d", set.ControllerPort), set.ControllerSecret)
	a.px = sysproxy.New(store.StateDir, set.MixedPort)

	// Self-heal before anything else: a previous unclean exit may have
	// left the system proxy pointing at a dead core.
	if a.px.HealIfNeeded() {
		a.healed = true
		log.Printf("clashgo: healed stale system proxy left by an unclean exit")
	}
	a.px.BeginSession()

	sigc := make(chan os.Signal, 2)
	signal.Notify(sigc, syscall.SIGTERM, syscall.SIGINT)
	go func() {
		<-sigc
		a.requestQuit()
	}()

	mygo.App.WhenReady(func() {
		a.initTray()
		// Silent autostart (default on): an autostart launch
		// lives in the tray without opening the window; any
		// manual launch opens it as before.
		if !(a.autostartLaunch && set.SilentAutostart) {
			a.openWindow()
		}
		a.work.Go(a.bootstrap)
	})
	// OnBeforeQuit fires before any window is closed during a quit, so
	// the window's OnClose can tell a real quit from a user close
	// (which hides to the tray instead). Covers the tray's RoleQuit
	// item and every other framework quit path.
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !a.quitDone.Load() {
			e.PreventDefault()
			a.requestQuit()
		}
	})
	if err := mygo.App.Run(); err != nil {
		fatal("run: %v", err)
	}
}

// bootstrap fetches any never-downloaded subscriptions, then renders
// the initial config and brings the core up. Fetching first matters:
// with provider cache files on disk, config validation stays a local,
// fast operation; without them the core tries to fetch providers
// during validation and can stall on a slow airport.
func (a *App) bootstrap(ctx context.Context) {
	if a.healed {
		a.setMsg("已修复上次异常退出留下的系统代理")
	}
	for _, s := range a.reg.Snapshot() {
		if ctx.Err() != nil {
			return
		}
		if s.Local || subs.ProviderFileExists(a.store.ProvidersDir(), s.Name) {
			continue
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		if err := a.reg.Fetch(ctx, s.Name, a.store.ProvidersDir()); err != nil {
			a.setMsg("订阅 %s 首次抓取失败：%v（可到节点页重试）", s.Name, err)
		}
		cancel()
	}
	if ctx.Err() != nil {
		return
	}
	_ = a.reg.Save()
	a.applyConfig()
	a.work.Go(a.pollLoop)
	a.work.Go(a.dnsLogLoop)
}
