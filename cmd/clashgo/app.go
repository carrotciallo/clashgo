package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/subs"
	"github.com/carrotciallo/clashgo/internal/sysproxy"
	"github.com/egoist/mygo"
)

// App holds all mutable state. Status fields are guarded by mu and
// written by background workers; form fields live on the UI thread.
type App struct {
	stateMu sync.RWMutex
	opMu    sync.Mutex
	store   *config.Store
	set     config.Settings
	reg     *subs.Registry
	sup     *core.Supervisor
	cli     *core.Client
	px      *sysproxy.Manager
	coreBin string
	healed  bool

	tray *mygo.Tray
	win  *mygo.Window

	// quitting distinguishes a real quit (tray Quit, exit button,
	// framework quit) from a window close: closes hide to the tray,
	// quits let the window die. Set from OnBeforeQuit (fires before
	// any window closes) and by requestQuit.
	quitting atomic.Bool
	quitDone atomic.Bool
	quitOnce sync.Once
	work     *core.Tasks

	renderMu    sync.Mutex
	mu          sync.Mutex
	page        int
	msg         string
	busy        bool
	coreOK      bool
	coreErr     string
	egress      string
	egressOrg   string // org (ISP/AS name) of the current egress IP
	egressOrgIP string // the IP egressOrg was resolved for
	mode        string // core routing mode: rule/global/direct
	up, down    int64
	proxies     map[string]core.Proxy
	// providers holds each subscription's node pool (provider name ==
	// subscription name), refreshed together with proxies.
	providers map[string][]core.Proxy
	delays    map[string]map[string]int
	selGroup  string
	// traffic rate sampling (pollLoop): previous totals + rates.
	prevUp, prevDown int64
	prevSample       time.Time
	upRate, downRate int64
	// dashboard data: core start time, controller round-trip,
	// live connections, LAN address.
	coreSince time.Time
	coreRTT   time.Duration
	conns     []core.Connection
	localIP   string
	gateway   string
	// session peak traffic rates for the overview cards.
	maxUpRate, maxDownRate int64
	overview               overviewFacts
	// recent holds connections that closed since launch, newest
	// first (the requests page's recent tab); prevConnIDs is the
	// previous poll's live set, diffed against the current one.
	recent      []recentConn
	prevConnIDs map[string]core.Connection
	// service status checks (services.go), guarded by mu.
	svcResults  map[string]svcResult
	svcChecking bool
	svcRan      bool
	// autostartLaunch marks a start via the autostart entry
	// (--autostart flag); with Settings.SilentAutostart the app
	// then starts tray-only. silentSaved is the last persisted
	// value of that option (the settings page saves on change).
	autostartLaunch bool
	silentSaved     bool
	// dialog visibility for the add forms.
	dlgAddSub, dlgAddRule, dlgNewProfile bool
	dlgImport                            bool

	windowOpen bool

	// forms (UI thread only)
	subName, subURL string
	profileName     string
	// expandedNode is the node whose detail strip is open on the
	// nodes page ("" = none).
	expandedNode string
	nodeFilter   string
	// collapsedSubs marks subscriptions folded on the nodes
	// page; seeded from and persisted to Settings.CollapsedSubs.
	collapsedSubs map[string]bool
	// Shadowrocket import form + the last import's report (shown in
	// the dialog after a successful import).
	impName, impURL, impText string
	impReport                *config.ImportReport
	impNameUsed              string
	ruleTypeIdx              int
	ruleValue                string
	ruleTargetIdx            int
	setCorePath              string
	setCorePage              bool
	autoSw                   *bool
	setMixed                 string
	setCtrl                  string
	setArm                   bool
	// requests page: tab (0 live, 1 recent, 2 DNS), network filter,
	// DNS form and its last result.
	reqTab int
	reqNet string
	// dnsLog is the core's DNS query history (newest first),
	// folded from its debug log stream by dnsLogLoop; dnsTotal
	// counts every entry seen since launch, like the counter
	// on FlClash's DNS tab.
	dnsLog   []dnsEntry
	dnsTotal int
	// overview service-status sub-page and its manage toggles.
	showServices bool
	svcManage    bool
	svcToggle    map[string]*bool
	// ovTab selects the overview connections card's module:
	// 0 live, 1 recent, 2 DNS lookup — all in place, FlClash-style.
	ovTab int
	// add-subscription dialog sources: 0 link, 1 file, 2 paste.
	// A decoded QR code lands in the link form, so there is no
	// separate QR mode.
	subSource int
	subText   string
	subFile   string
	// config page (Shadowrocket shape): the profile whose detail
	// is open ("" = the list), the detail section being viewed,
	// whether the rule-provider list is up, and its dialogs.
	cfgDetail    string
	cfgOpen      bool // detail open; cfgDetail alone can't say so — the built-in profile's name IS ""
	cfgSection   string
	cfgProviders bool
	dlgRestore   bool
	dlgTestRule  bool
	testRuleIn   string
	testRuleOut  string
	dlgReplace   bool
	repFrom      string
	repTo        string
}

// recentConn is one closed connection, kept for the requests page.
type recentConn struct {
	core.Connection
	ClosedAt time.Time
}

var ruleTypes = []string{"DOMAIN-SUFFIX", "DOMAIN", "DOMAIN-KEYWORD", "IP-CIDR", "GEOSITE", "GEOIP", "PROCESS-NAME", "MATCH"}

func (a *App) setMsg(format string, args ...any) {
	a.mu.Lock()
	a.msg = fmt.Sprintf(format, args...)
	a.mu.Unlock()
}

func (a *App) setBusy(b bool, msg string) {
	a.mu.Lock()
	a.busy = b
	if msg != "" {
		a.msg = msg
	}
	a.mu.Unlock()
}

func (a *App) snapshot() (msg string, busy, coreOK bool, coreErr, egress string, up, down int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.msg, a.busy, a.coreOK, a.coreErr, a.egress, a.up, a.down
}

// ---- config pipeline ----

// renderAndInstall renders the effective config, validates it with
// the core, and installs it. A failure at any step leaves the
// previously installed config fully in place.
func (a *App) renderAndInstall() error {
	return a.installRuntime(true)
}

func (a *App) installRuntime(reload bool) error {
	a.renderMu.Lock()
	defer a.renderMu.Unlock()
	// Geodata must be in the core home before ANY mihomo invocation,
	// including validation — otherwise the core blocks trying to
	// download it (Phase 1 bootstrap bug, found live).
	core.SeedGeodata(a.store.RuntimeDir(), core.GeodataDirs())
	master, err := a.store.LoadMaster()
	if err != nil {
		return fmt.Errorf("读取主配置失败：%w", err)
	}
	rendered, err := config.RenderChecked(master, a.reg.Sources(a.store.ProvidersDir()), a.settings())
	if err != nil {
		return fmt.Errorf("渲染配置失败：%w", err)
	}
	var activate func(string) error
	if reload && a.sup.Running() {
		activate = a.client().ReloadConfig
	}
	return config.InstallRuntime(a.store.RenderedPath(), rendered, func(path string) error {
		return core.ValidateConfig(a.corePath(), a.store.RuntimeDir(), path)
	}, activate)
}

// applyConfig renders, validates, installs and (if running) reloads the
// effective config. A validation failure leaves the previous config
// fully in place. Runs on a goroutine.
func (a *App) applyConfig() {
	a.setBusy(true, "渲染并校验配置…")
	defer a.setBusy(false, "")
	if err := a.renderAndInstall(); err != nil {
		a.setMsg("配置未生效，旧配置保持不变：%v", err)
		return
	}
	if a.sup.Running() {
		a.setMsg("配置已生效")
	} else {
		a.startCore()
		return
	}
	a.refreshProxies()
}

// applyConfigRestart is applyConfig with a full core restart instead
// of a hot reload, for changes a reload cannot apply (TUN). Reports
// whether the core ended up healthy on the new config.
func (a *App) applyConfigRestart() bool {
	if err := a.installRuntime(false); err != nil {
		a.setMsg("配置未生效，旧配置保持不变：%v", err)
		return false
	}
	a.sup.Stop()
	for i := 0; i < 40 && a.sup.Running(); i++ {
		time.Sleep(100 * time.Millisecond)
	}
	a.startCore()
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.coreOK
}

// setCapture switches the capture mode. System proxy and TUN are
// mutually exclusive — exactly one owns the traffic: switching
// re-renders, restarts the core, and only then moves the OS side
// (Arm for proxy mode, Neutralize for TUN) via startCore. If the core
// does not come up healthy in the new mode, the setting and the
// running config roll back to the mode that worked before.
func (a *App) setCapture(tun bool) {
	a.operate(func() {
		a.setBusy(true, "切换接管方式…")
		defer a.setBusy(false, "")
		if tun == a.settings().TUNEnabled {
			return
		}
		if tun {
			if err := core.EnsureTunCaps(a.corePath()); err != nil {
				a.setMsg("TUN 需要先给核心授权：%v", err)
				return
			}
		}
		prev := a.settings().TUNEnabled
		if err := a.changeSettings(func(s *config.Settings) { s.TUNEnabled = tun }); err != nil {
			a.setMsg("保存设置失败：%v", err)
			return
		}
		if !a.applyConfigRestart() {
			_ = a.changeSettings(func(s *config.Settings) { s.TUNEnabled = prev })
			a.applyConfigRestart()
			a.setMsg("切换接管方式失败，已回滚")
			return
		}
		if !tun {
			// An explicit switch to proxy mode arms even when the
			// autostart preference is off — the click is the intent.
			a.proxyManager().Arm()
		}
		if tun {
			a.setMsg("接管方式已切换：TUN")
		} else {
			a.setMsg("接管方式已切换：系统代理")
		}
		a.refreshTray()
	})
}

// startCore seeds geodata, spawns the core, waits for readiness, and
// only then arms the system proxy. Runs on a goroutine.
func (a *App) startCore() {
	a.setBusy(true, "启动核心…")
	defer a.setBusy(false, "")
	core.SeedGeodata(a.store.RuntimeDir(), core.GeodataDirs())
	if err := a.sup.Start(a.store.RenderedPath()); err != nil {
		a.mu.Lock()
		a.coreOK, a.coreErr = false, err.Error()
		a.mu.Unlock()
		a.setMsg("核心启动失败：%v", err)
		return
	}
	if err := a.client().WaitReady(20 * time.Second); err != nil {
		a.sup.Stop()
		a.mu.Lock()
		a.coreOK = false
		a.coreErr = err.Error() + "\n" + a.sup.LogTail(8)
		a.mu.Unlock()
		a.setMsg("核心未就绪，未接管系统代理。日志尾部见概览页。")
		return
	}
	if !a.sup.Running() {
		a.setMsg("核心在就绪检查期间退出")
		a.mu.Lock()
		a.coreOK = false
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	a.coreOK, a.coreErr = true, ""
	a.coreSince = time.Now()
	a.mu.Unlock()
	// Capture modes are mutually exclusive: exactly one of system
	// proxy / TUN owns the machine's traffic at any time. A quit in
	// flight must win over a capture switch still landing (the two
	// raced once, 2026-11-11: the park landed after Disarm had
	// already restored the desktop, leaving a stale parked record).
	if a.quitting.Load() {
		return
	}
	var captureErr error
	if a.settings().TUNEnabled {
		captureErr = a.proxyManager().Neutralize()
	} else if a.settings().ArmProxyOnStart && !a.proxyManager().Armed() {
		captureErr = a.proxyManager().Arm()
	}
	if captureErr != nil {
		a.setMsg("核心就绪，但系统代理设置失败：%v", captureErr)
	} else {
		a.setMsg("")
	}
	a.refreshProxies()
	a.refreshMode()
	a.refreshEgress()
	a.refreshTray()
}

func (a *App) restartCore() {
	a.operate(func() {
		a.setBusy(true, "重启核心…")
		a.sup.Stop()
		for i := 0; i < 40 && a.sup.Running(); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		a.setBusy(false, "")
		a.startCore()
	})
}

// ---- data refresh ----

func (a *App) refreshProxies() {
	if !a.sup.Running() {
		return
	}
	px, err := a.client().Proxies()
	if err != nil {
		return
	}
	pv, _ := a.client().Providers() // best-effort; sections fall back gracefully
	a.mu.Lock()
	a.proxies = px
	if pv != nil {
		a.providers = pv
	}
	if a.selGroup == "" {
		if _, ok := px["节点选择"]; ok {
			a.selGroup = "节点选择"
		}
	}
	a.mu.Unlock()
}

func (a *App) refreshMode() {
	if !a.sup.Running() {
		return
	}
	m, err := a.client().ConfigMode()
	if err != nil || m == "" {
		return
	}
	a.mu.Lock()
	a.mode = m
	a.mu.Unlock()
}

func (a *App) setMode(m string) {
	a.operate(func() {
		if err := a.client().SetMode(m); err != nil {
			a.setMsg("切换模式失败：%v", err)
			return
		}
		a.mu.Lock()
		a.mode = m
		a.mu.Unlock()
		a.setMsg("出站模式：%s", modeLabel(m))
		a.refreshEgress()
	})
}

func modeLabel(m string) string {
	switch m {
	case "global":
		return "全局"
	case "direct":
		return "直连"
	default:
		return "规则"
	}
}

// requestQuit is the only in-app quit path: it flags the quit so the
// window's OnClose lets the close through, then asks the app to quit.
func (a *App) requestQuit() {
	a.quitting.Store(true)
	a.quitOnce.Do(func() {
		// Native API calls may synchronously wait for the main thread. Keep
		// its event loop alive until managed workers and cleanup have finished.
		go func() {
			a.shutdown()
			a.quitDone.Store(true)
			mygo.RunOnMain(func() { mygo.App.Quit() })
		}()
	})
}

func (a *App) refreshEgress() {
	a.operate(func() {
		port := a.settings().MixedPort
		tr := &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", port)})}
		cl := &http.Client{Transport: tr, Timeout: 15 * time.Second}
		defer cl.CloseIdleConnections()
		req, _ := http.NewRequestWithContext(a.appContext(), http.MethodGet, "https://www.cloudflare.com/cdn-cgi/trace", nil)
		resp, err := cl.Do(req)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		var ip, loc string
		for _, ln := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(ln, "ip=") {
				ip = strings.TrimPrefix(ln, "ip=")
			}
			if strings.HasPrefix(ln, "loc=") {
				loc = strings.TrimPrefix(ln, "loc=")
			}
		}
		if ip != "" {
			a.mu.Lock()
			a.egress = ip + " · " + loc
			needOrg := ip != a.egressOrgIP
			a.mu.Unlock()
			if needOrg {
				if org, ok := fetchEgressOrg(cl, ip); ok {
					a.mu.Lock()
					a.egressOrg, a.egressOrgIP = org, ip
					a.mu.Unlock()
				}
			}
		}
	})
}

// fetchEgressOrg resolves the organization (ISP / AS name)
// behind the egress IP — the dashboard status card's 组织
// field, as in FlClash. Keyed by IP: callers only invoke it
// when the egress IP changes, so the cadence stays at one
// lookup per node switch.
func fetchEgressOrg(cl *http.Client, wantIP string) (string, bool) {
	resp, err := cl.Get("https://ipinfo.io/json")
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	var v struct {
		IP  string `json:"ip"`
		Org string `json:"org"`
	}
	if json.NewDecoder(resp.Body).Decode(&v) != nil || v.IP != wantIP {
		return "", false
	}
	// ipinfo prefixes the ASN ("AS38136 Akari Networks"); the
	// dashboard shows the bare organization name.
	org := v.Org
	if f := strings.Fields(org); len(f) > 1 && strings.HasPrefix(f[0], "AS") {
		org = strings.Join(f[1:], " ")
	}
	return org, true
}

// pollLoop refreshes traffic totals while the window is open, and the
// egress readout every ~30 s (it can be stale right after startup,
// before auto-selection settles).
func (a *App) pollLoop(ctx context.Context) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	ticks := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !a.opMu.TryLock() {
			continue
		}
		a.pollTick(&ticks)
		a.opMu.Unlock()
	}
}

func (a *App) pollTick(ticks *int) {
	a.mu.Lock()
	open, ok := a.windowOpen, a.coreOK
	a.mu.Unlock()
	if ok && !a.sup.Running() {
		a.mu.Lock()
		a.coreOK = false
		a.coreErr = "核心进程已退出"
		a.mu.Unlock()
		ok = false
		if err := a.proxyManager().Disarm(); err != nil {
			a.setMsg("核心退出后恢复代理失败：%v", err)
		}
	}
	// Ownership checks also run while the native window is hidden.
	if ok {
		if err := a.proxyManager().Reassert(); err != nil {
			a.setMsg("代理所有权检查失败：%v", err)
		}
	}
	if open {
		a.refreshOverviewFacts()
	}
	if !open || !ok {
		return
	}
	sample, sampleErr := a.client().ConnectionsSnapshot()
	if sampleErr == nil {
		up, down := sample.UploadTotal, sample.DownloadTotal
		now := time.Now()
		a.mu.Lock()
		if !a.prevSample.IsZero() {
			if dt := now.Sub(a.prevSample).Seconds(); dt > 0 {
				a.upRate = int64(float64(up-a.prevUp) / dt)
				a.downRate = int64(float64(down-a.prevDown) / dt)
				if a.upRate > a.maxUpRate {
					a.maxUpRate = a.upRate
				}
				if a.downRate > a.maxDownRate {
					a.maxDownRate = a.downRate
				}
				if a.upRate < 0 {
					a.upRate = 0
				}
				if a.downRate < 0 {
					a.downRate = 0
				}
			}
		}
		a.prevUp, a.prevDown, a.prevSample = up, down, now
		a.up, a.down = up, down
		a.mu.Unlock()
	}
	if sampleErr == nil {
		conns := sample.Connections
		// Drop the core's own plumbing connections (dialer
		// tunnels have no user-meaningful exit).
		kept := conns[:0]
		for _, cn := range conns {
			if cn.Exit() != "" {
				kept = append(kept, cn)
			}
		}
		sort.Slice(kept, func(i, j int) bool { return kept[i].Start.After(kept[j].Start) })
		a.mu.Lock()
		cur := make(map[string]core.Connection, len(kept))
		for _, cn := range kept {
			if cn.ID != "" {
				cur[cn.ID] = cn
			}
		}
		for id, old := range a.prevConnIDs {
			if _, ok := cur[id]; !ok {
				a.recent = append([]recentConn{{Connection: old, ClosedAt: time.Now()}}, a.recent...)
			}
		}
		if len(a.recent) > 60 {
			a.recent = a.recent[:60]
		}
		a.prevConnIDs = cur
		a.conns = kept
		a.mu.Unlock()
	}
	(*ticks)++
	if *ticks%15 == 0 {
		a.refreshEgress()
		a.refreshProxies()
		a.refreshMode()
		if rtt, err := a.client().Ping(); err == nil {
			a.mu.Lock()
			a.coreRTT = rtt
			a.mu.Unlock()
		}
		if ip := lanIP(); ip != "" {
			a.mu.Lock()
			a.localIP = ip
			a.mu.Unlock()
		}
		if gw := lanGateway(); gw != "" {
			a.mu.Lock()
			a.gateway = gw
			a.mu.Unlock()
		}
		a.mu.Lock()
		run := !a.svcRan && a.coreOK
		if run {
			a.svcRan = true
		}
		a.mu.Unlock()
		if run {
			a.operate(a.checkServices)
		}
	}
}

// ---- actions used by views ----

func (a *App) toggleCapture() {
	a.setCapture(!a.settings().TUNEnabled)
}

func (a *App) coreHealthy() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.coreOK
}

func (a *App) switchNode(group, name string) {
	a.operate(func() {
		if err := a.client().Switch(group, name); err != nil {
			a.setMsg("切换失败：%v", err)
			return
		}
		a.setMsg("已切换：%s → %s", group, name)
		a.refreshProxies()
		time.Sleep(600 * time.Millisecond)
		a.refreshEgress()
	})
}

func (a *App) testDelay(group string) {
	a.operate(func() {
		a.setBusy(true, "测速中…")
		defer a.setBusy(false, "")
		res, err := a.client().GroupDelay(group, "https://www.gstatic.com/generate_204", 5000)
		if err != nil {
			a.setMsg("测速失败：%v", err)
			return
		}
		a.mu.Lock()
		if a.delays == nil {
			a.delays = map[string]map[string]int{}
		}
		a.delays[group] = res
		a.mu.Unlock()
		a.setMsg("%s 测速完成", group)
	})
}

// testNodeDelay measures one node and files the result in the
// 节点选择 delay table the node rows already read (0 = timeout).
func (a *App) testNodeDelay(provider, name string) {
	a.operate(func() {
		d, err := a.client().ProviderDelay(provider, name, "https://www.gstatic.com/generate_204", 5000)
		a.mu.Lock()
		if a.delays == nil {
			a.delays = map[string]map[string]int{}
		}
		if a.delays["节点选择"] == nil {
			a.delays["节点选择"] = map[string]int{}
		}
		if err != nil {
			a.delays["节点选择"][name] = 0
		} else {
			a.delays["节点选择"][name] = d
		}
		a.mu.Unlock()
		if err != nil {
			a.setMsg("测速失败：%v", err)
		}
	})
}

// toggleCore is the dashboard's big start/stop button. Stopping in
// system-proxy mode disarms first, so nothing is left pointing at a
// dead port; in TUN mode the parked (direct) settings already mean
// "no proxy", and startCore re-applies the capture on the way back.
func (a *App) toggleCore() {
	a.operate(func() {
		if a.sup.Running() {
			if !a.settings().TUNEnabled {
				a.proxyManager().Disarm()
			}
			a.sup.Stop()
			a.mu.Lock()
			a.coreOK = false
			a.coreSince = time.Time{}
			a.mu.Unlock()
			a.setMsg("核心已停止")
			a.refreshTray()
			return
		}
		a.startCore()
	})
}

// lanIP returns the primary LAN address: the first IPv4 on a
// physical interface. Virtual interfaces (the core's own Meta TUN,
// docker bridges, VPNs) are skipped — under TUN the default route
// leaves from the TUN, which is not the LAN address FlClash-style
// dashboards show.
func lanIP() string {
	ifaces, err := net.Interfaces()
	if err != nil {
		return ""
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback != 0 || ifc.Flags&net.FlagUp == 0 {
			continue
		}
		name := strings.ToLower(ifc.Name)
		virtual := false
		for _, p := range []string{"meta", "tun", "utun", "docker", "br-", "veth", "tailscale", "wg", "virbr", "lo"} {
			if strings.HasPrefix(name, p) {
				virtual = true
				break
			}
		}
		if virtual {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			if ipn, ok := a.(*net.IPNet); ok && ipn.IP.To4() != nil {
				return ipn.IP.String()
			}
		}
	}
	return ""
}

// rssBytes reads a process's resident set size from /proc.
func rssBytes(pid int) int64 {
	if pid <= 0 {
		return 0
	}
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		return 0
	}
	for _, ln := range strings.Split(string(b), "\n") {
		if strings.HasPrefix(ln, "VmRSS:") {
			var kb int64
			if _, err := fmt.Sscanf(strings.TrimSpace(strings.TrimPrefix(ln, "VmRSS:")), "%d", &kb); err == nil {
				return kb * 1024
			}
		}
	}
	return 0
}

// trafficPeaks returns the session peak up/down rates (bytes/s).
func (a *App) trafficPeaks() (int64, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.maxUpRate, a.maxDownRate
}

// lanGateway reads the default gateway from the kernel route table.
func lanGateway() string {
	b, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	for _, ln := range strings.Split(string(b), "\n") {
		f := strings.Fields(ln)
		if len(f) >= 3 && f[1] == "00000000" {
			v, err := strconv.ParseUint(f[2], 16, 32)
			if err != nil {
				return ""
			}
			return fmt.Sprintf("%d.%d.%d.%d", byte(v), byte(v>>8), byte(v>>16), byte(v>>24))
		}
	}
	return ""
}

// memUsage reports app + core resident memory for the dashboard.
func (a *App) memUsage() (app, core int64) {
	return rssBytes(os.Getpid()), rssBytes(a.sup.PID())
}

// trafficRates returns the latest sampled up/down rates (bytes/s).
func (a *App) trafficRates() (int64, int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.upRate, a.downRate
}

// egressChain walks the picker's selection chain to its leaf:
// [节点选择, maybe a strategy group, …, leaf node]. Empty when the
// core has no proxy data yet.
func (a *App) egressChain() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.egressChainLocked()
}

func (a *App) egressChainLocked() []string {
	g, ok := a.proxies["节点选择"]
	if !ok {
		return nil
	}
	chain := []string{"节点选择"}
	cur := g.Now
	for i := 0; i < 8 && cur != ""; i++ {
		chain = append(chain, cur)
		p, ok := a.proxies[cur]
		if !ok || len(p.All) == 0 {
			break
		}
		cur = p.Now
	}
	return chain
}

// useNode makes name the effective egress, from wherever it lives:
// directly in the picker, or one hop down inside a group the picker
// can select (the shape split subscriptions produce — U15 lives in
// 国外流量， which lives in 节点选择）. This is the nodes page's
// one-tap switch.
func (a *App) useNode(name string) {
	a.mu.Lock()
	proxies := a.proxies
	a.mu.Unlock()
	picker, ok := proxies["节点选择"]
	if !ok {
		a.setMsg("核心未就绪，无法切换节点")
		return
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
		a.switchNode("节点选择", name)
		return
	}
	for gname, g := range proxies {
		if len(g.All) == 0 || gname == "节点选择" {
			continue
		}
		if contains(g.All, name) && contains(picker.All, gname) {
			gname := gname
			a.operate(func() {
				if err := a.client().Switch(gname, name); err != nil {
					a.setMsg("切换失败：%v", err)
					return
				}
				if err := a.client().Switch("节点选择", gname); err != nil {
					a.setMsg("切换失败：%v", err)
					return
				}
				a.setMsg("已切换：节点选择 → %s → %s", gname, name)
				a.refreshProxies()
				time.Sleep(600 * time.Millisecond)
				a.refreshEgress()
			})
			return
		}
	}
	a.setMsg("节点 %s 不在可达分组里，无法直接切换", name)
}

// ---- profiles （配置快速切换） ----

// switchProfile makes another config file the active master and
// applies it. A render failure reverts the switch completely — the
// running core and the persisted choice both stay on the old file.
func (a *App) switchProfile(name string) {
	a.operate(func() {
		if name == a.settings().ActiveProfile {
			return
		}
		a.setBusy(true, "切换配置…")
		defer a.setBusy(false, "")
		old := a.settings().ActiveProfile
		a.store.SetActiveProfile(name)
		if err := a.changeSettings(func(s *config.Settings) { s.ActiveProfile = name }); err != nil {
			a.store.SetActiveProfile(old)
			a.setMsg("保存配置选择失败：%v", err)
			return
		}
		if err := a.renderAndInstall(); err != nil {
			a.store.SetActiveProfile(old)
			_ = a.changeSettings(func(s *config.Settings) { s.ActiveProfile = old })
			a.setMsg("配置未切换，新配置校验不过：%v", err)
			return
		}
		if !a.sup.Running() {
			a.startCore()
			a.mu.Lock()
			ok := a.coreOK
			a.mu.Unlock()
			if !ok {
				return
			}
		}
		a.setMsg("已切换到配置「%s」", profileDisplayName(name))
		a.refreshProxies()
	})
}

func (a *App) setDefaultProfile(name string) {
	if err := a.changeSettings(func(s *config.Settings) { s.DefaultProfile = name }); err != nil {
		a.setMsg("保存默认配置失败：%v", err)
		return
	}
	a.setMsg("已设「%s」为默认配置", profileDisplayName(name))
}

// createProfile seeds a new profile from the active config's
// current content (a copy the user then edits), without switching.
func (a *App) createProfile() {
	name := strings.TrimSpace(a.profileName)
	if name == "" {
		a.setMsg("先给新配置起个名")
		return
	}
	master, err := a.store.LoadMaster()
	if err != nil {
		a.setMsg("读当前配置失败：%v", err)
		return
	}
	if err := a.store.CreateProfile(name, master); err != nil {
		a.setMsg("新建配置失败：%v", err)
		return
	}
	a.profileName = ""
	a.setMsg("已新建配置「%s」（内容复制自当前配置，未切换）", name)
}

// restoreDefaultProfile is Shadowrocket's 恢复默认配置： the
// built-in template becomes the profile named 默认配置 (created
// or overwritten) and the app switches to it through the usual
// validated path. Other profiles are untouched.
func (a *App) restoreDefaultProfile() {
	const name = "默认配置"
	if a.store.ProfileExists(name) {
		if err := os.WriteFile(a.store.ProfilePath(name), []byte(config.DefaultMaster), 0o644); err != nil {
			a.setMsg("恢复默认配置失败：%v", err)
			return
		}
	} else if err := a.store.CreateProfile(name, config.DefaultMaster); err != nil {
		a.setMsg("恢复默认配置失败：%v", err)
		return
	}
	if a.settings().ActiveProfile == name {
		if err := a.store.SaveMaster(config.DefaultMaster); err != nil {
			a.setMsg("恢复默认配置失败：%v", err)
			return
		}
		a.applyConfig()
		a.setMsg("已恢复默认配置")
		return
	}
	a.switchProfile(name)
}

// duplicateProfile copies a profile under a fresh 副本 name.
func (a *App) duplicateProfile(name string) {
	content, err := a.profileContent(name)
	if err != nil {
		a.setMsg("读配置失败：%v", err)
		return
	}
	newName := name + " 副本"
	for i := 2; a.store.ProfileExists(newName); i++ {
		newName = fmt.Sprintf("%s 副本 %d", name, i)
	}
	if err := a.store.CreateProfile(newName, content); err != nil {
		a.setMsg("复制配置失败：%v", err)
		return
	}
	a.setMsg("已复制为「%s」", newName)
}

// exportProfile copies a profile file to a user-chosen path.
func (a *App) exportProfile(name string) {
	path := a.store.ProfilePath(name)
	a.pickSaveFile("导出配置", profileDisplayName(name)+".yaml", func(dst string) {
		data, err := os.ReadFile(path)
		if err == nil {
			err = os.WriteFile(dst, data, 0o644)
		}
		if err != nil {
			a.setMsg("导出失败：%v", err)
			return
		}
		a.setMsg("已导出到 %s", dst)
	})
}

// openProfileText hands the profile file to the desktop's text
// editor (编辑纯文本）.
func (a *App) openProfileText(name string) {
	path := a.store.ProfilePath(name)
	a.launch(func() {
		if err := exec.Command("xdg-open", path).Start(); err != nil {
			a.setMsg("打开编辑器失败：%v", err)
		}
	})
}

// subCollapsed reports whether a subscription's node list is
// folded away on the nodes page.
func (a *App) subCollapsed(name string) bool {
	return a.collapsedSubs[name]
}

// toggleSubCollapsed flips a subscription's fold and persists
// the fold set so it survives restarts.
func (a *App) toggleSubCollapsed(name string) {
	if a.collapsedSubs == nil {
		a.collapsedSubs = map[string]bool{}
	}
	a.collapsedSubs[name] = !a.collapsedSubs[name]
	list := []string{}
	for n, folded := range a.collapsedSubs {
		if folded {
			list = append(list, n)
		}
	}
	sort.Strings(list)
	if err := a.changeSettings(func(s *config.Settings) { s.CollapsedSubs = list }); err != nil {
		a.setMsg("保存折叠状态失败：%v", err)
	}
}

// cfgTarget resolves which profile the config-page dialogs act
// on: the one whose detail is open, else the active profile.
func (a *App) cfgTarget() string {
	if a.cfgOpen {
		return a.cfgDetail
	}
	return a.settings().ActiveProfile
}

// replacePolicy rewrites every rule in the target profile whose
// policy token equals repFrom to repTo （替换策略）.
func (a *App) replacePolicy() {
	from := strings.TrimSpace(a.repFrom)
	to := strings.TrimSpace(a.repTo)
	if from == "" || to == "" || from == to {
		a.setMsg("原策略和新策略都要填，且不能相同")
		return
	}
	name := a.cfgTarget()
	content, err := a.profileContent(name)
	if err != nil {
		a.setMsg("读配置失败：%v", err)
		return
	}
	rules := config.ExtractRules(content)
	n := 0
	for i, r := range rules {
		head, policy := rulePolicy(r)
		if policy != from {
			continue
		}
		if strings.HasSuffix(r, ",no-resolve") {
			rules[i] = head + "," + to + ",no-resolve"
		} else {
			rules[i] = head + "," + to
		}
		n++
	}
	if n == 0 {
		a.setMsg("没有规则在用策略「%s」", from)
		return
	}
	if err := a.saveProfileContent(name, config.ReplaceRules(content, rules)); err != nil {
		a.setMsg("保存失败：%v", err)
		return
	}
	a.setMsg("已把 %d 条规则的策略 %s 换成 %s", n, from, to)
	if name == a.settings().ActiveProfile {
		a.applyConfig()
	}
}

// runTestRule evaluates the target profile's rules against the
// entered host, offline （测试规则）.
func (a *App) runTestRule() {
	host := hostFromInput(a.testRuleIn)
	if host == "" {
		a.testRuleOut = "先输入一个域名"
		return
	}
	name := a.cfgTarget()
	content, err := a.profileContent(name)
	if err != nil {
		a.testRuleOut = "读配置失败：" + err.Error()
		return
	}
	rules := config.ExtractRules(content)
	if idx, rule, ok := evalRules(rules, host); ok {
		_, policy := rulePolicy(rule)
		a.testRuleOut = fmt.Sprintf("%s 命中第 %d 条：%s → %s", host, idx+1, rule, policy)
		return
	}
	a.testRuleOut = host + " 没有命中任何可离线判定的规则"
}

// importConfig runs the format adaptation: fetch (when a URL was
// given) or take the pasted text, translate it into a ClashGo
// master config, and save it as a new profile. It never switches —
// switching goes through the profile list's validated path. The
// report stays in the dialog so nothing the converter dropped or
// approximated is silent.
func (a *App) importConfig() {
	text, u, name := a.impText, strings.TrimSpace(a.impURL), strings.TrimSpace(a.impName)
	a.operate(func() {
		if u != "" {
			a.setBusy(true, "拉取配置…")
			body, err := fetchText(a.appContext(), u)
			a.setBusy(false, "")
			if err != nil {
				a.setMsg("拉取配置失败：%v", err)
				return
			}
			text = body
		}
		if strings.TrimSpace(text) == "" {
			a.setMsg("先粘贴 Shadowrocket 配置内容，或填配置地址")
			return
		}
		master, rep, err := config.TranslateShadowrocket(text)
		if err != nil {
			a.setMsg("导入失败：%v", err)
			return
		}
		if name == "" {
			name = "Shadowrocket 导入"
		}
		if err := a.store.CreateProfile(name, master); err != nil {
			a.setMsg("导入失败：%v", err)
			return
		}
		a.onUI(func() {
			a.impReport = &rep
			a.impNameUsed = name
			a.impText, a.impURL = "", ""
		})
		a.setMsg("已导入配置「%s」（未切换）", name)
	})
}

// fetchText GETs a config file for the import dialog.
func fetchText(parent context.Context, rawURL string) (string, error) {
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "ClashGo/1.0")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func (a *App) deleteProfile(name string) {
	if name == a.settings().ActiveProfile {
		a.setMsg("正在使用的配置不能删除，先切到别的配置")
		return
	}
	if name == a.settings().DefaultProfile {
		a.setMsg("默认配置不能删除，先改默认")
		return
	}
	if err := a.store.DeleteProfile(name); err != nil {
		a.setMsg("删除配置失败：%v", err)
		return
	}
	a.setMsg("已删除配置「%s」", profileDisplayName(name))
}

// profileDisplayName renders the built-in profile ("") readably.
func profileDisplayName(name string) string {
	if name == "" {
		return "默认配置"
	}
	return name
}

func (a *App) addSubscription() {
	name := strings.TrimSpace(a.subName)
	if a.subSource == 1 || a.subSource == 2 {
		// File / paste: the content is the subscription; there is no
		// URL to fetch, now or later.
		var body []byte
		var err error
		if a.subSource == 1 {
			if a.subFile == "" {
				a.setMsg("先选一个订阅文件")
				return
			}
			body, err = os.ReadFile(a.subFile)
			if err != nil {
				a.setMsg("读取文件失败：%v", err)
				return
			}
		} else {
			body = []byte(a.subText)
		}
		a.operate(func() {
			if err := a.reg.AddLocal(name); err != nil {
				a.setMsg("添加订阅失败：%v", err)
				return
			}
			if err := a.reg.Ingest(name, body, a.store.ProvidersDir()); err != nil {
				a.setMsg("订阅内容不可用：%v", err)
				return
			}
			sub := a.reg.Find(name)
			if sub == nil {
				return
			}
			a.onUI(func() { a.subName, a.subText, a.subFile = "", "", "" })
			a.setMsg("订阅 %s 已添加（%d 个节点），应用配置…", name, sub.NodeCount)
			a.applyConfig()
		})
		return
	}
	rawURL := strings.TrimSpace(a.subURL)
	a.operate(func() {
		if err := a.reg.Add(name, rawURL); err != nil {
			a.setMsg("添加订阅失败：%v", err)
			return
		}
		a.onUI(func() { a.subName, a.subURL = "", "" })
		a.setMsg("订阅 %s 已添加，抓取中…", name)
		a.fetchSubscription(name, true)
	})
}

func (a *App) fetchSubscription(name string, reRender bool) {
	sub := a.reg.Find(name)
	if sub == nil {
		return
	}
	if sub.Local {
		a.setMsg("订阅 %s 是本地内容，没有可刷新的地址", name)
		return
	}
	a.setBusy(true, "抓取订阅 "+name+"…")
	defer a.setBusy(false, "")
	ctx, cancel := context.WithTimeout(a.appContext(), 60*time.Second)
	defer cancel()
	if err := a.reg.Fetch(ctx, name, a.store.ProvidersDir()); err != nil {
		a.setMsg("订阅 %s 刷新失败（旧节点保留）：%v", name, err)
		return
	}
	sub = a.reg.Find(name)
	if sub == nil {
		return
	}
	if reRender || sub.Split {
		// Split subscriptions carry groups, not just a provider file:
		// a provider reload is not enough, the config must re-render.
		a.setMsg("订阅 %s 已更新（%d 个节点），应用配置…", name, sub.NodeCount)
		a.applyConfig()
	} else if a.sup.Running() {
		if err := a.client().UpdateProvider(name); err != nil {
			a.setMsg("订阅 %s 已更新（%d 个节点），但核心重载 provider 失败：%v", name, sub.NodeCount, err)
			return
		}
		a.setMsg("订阅 %s 已更新（%d 个节点）", name, sub.NodeCount)
		a.refreshProxies()
	}
}

// applyQRCode lands a decoded QR payload in the add-subscription
// form: subscription QR codes carry the URL, so it becomes a link
// subscription. Anything else is reported, not silently dropped.
func (a *App) applyQRCode(s string) {
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		a.subURL = s
		a.subSource = 0
		a.setMsg("二维码已识别为订阅链接")
		return
	}
	a.setMsg("二维码内容不是订阅链接")
}

func (a *App) refreshSubscription(name string) {
	a.operate(func() { a.fetchSubscription(name, false) })
}

func (a *App) removeSubscription(name string) {
	a.operate(func() {
		if err := a.reg.Remove(name); err != nil {
			a.setMsg("删除失败：%v", err)
			return
		}
		_ = os.Remove(filepath.Join(a.store.ProvidersDir(), name+".yaml"))
		a.setMsg("订阅 %s 已删除", name)
		a.applyConfig()
	})
}

func (a *App) ruleTargets() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var groups []string
	for name, p := range a.proxies {
		if len(p.All) > 0 {
			groups = append(groups, name)
		}
	}
	sort.Strings(groups)
	out := append([]string{"DIRECT", "REJECT"}, groups...)
	if len(out) == 2 {
		out = append(out, "节点选择")
	}
	return out
}

func (a *App) addRule() {
	value := strings.TrimSpace(a.ruleValue)
	typ := ruleTypes[a.ruleTypeIdx]
	targets := a.ruleTargets()
	target := targets[a.ruleTargetIdx%len(targets)]
	a.operate(func() {
		master, err := a.store.LoadMaster()
		if err != nil {
			a.setMsg("读主配置失败：%v", err)
			return
		}
		var rule string
		if typ == "MATCH" {
			rule = "MATCH," + target
		} else {
			if value == "" {
				a.setMsg("规则值不能为空")
				return
			}
			rule = typ + "," + value + "," + target
		}
		rules := config.ExtractRules(master)
		// MATCH must stay last.
		if n := len(rules); n > 0 && strings.HasPrefix(rules[n-1], "MATCH,") && typ != "MATCH" {
			rules = append(rules[:n-1], rule, rules[n-1])
		} else {
			rules = append(rules, rule)
		}
		if err := a.store.SaveMaster(config.ReplaceRules(master, rules)); err != nil {
			a.setMsg("保存规则失败：%v", err)
			return
		}
		a.onUI(func() { a.ruleValue = "" })
		a.setMsg("规则已添加：%s", rule)
		a.applyConfig()
	})
}

func (a *App) deleteRule(idx int) {
	a.operate(func() {
		master, err := a.store.LoadMaster()
		if err != nil {
			a.setMsg("读主配置失败：%v", err)
			return
		}
		rules := config.ExtractRules(master)
		if idx < 0 || idx >= len(rules) {
			return
		}
		removed := rules[idx]
		rules = append(rules[:idx], rules[idx+1:]...)
		if err := a.store.SaveMaster(config.ReplaceRules(master, rules)); err != nil {
			a.setMsg("保存规则失败：%v", err)
			return
		}
		a.setMsg("规则已删除：%s", removed)
		a.applyConfig()
	})
}

func (a *App) shutdown() {
	if a.work != nil {
		a.work.Wait()
	}
	if err := a.proxyManager().Disarm(); err != nil {
		a.setMsg("退出时恢复系统代理失败：%v", err)
	}
	if err := a.proxyManager().EndSession(); err != nil {
		a.setMsg("代理恢复记录保留：%v", err)
	}
	a.sup.Stop()
	if a.tray != nil {
		a.tray.Destroy()
	}
}

func humanBytes(n int64) string {
	const u = 1024
	if n < u {
		return fmt.Sprintf("%d B", n)
	}
	f := float64(n)
	for _, unit := range []string{"KB", "MB", "GB", "TB"} {
		f /= u
		if f < u {
			return fmt.Sprintf("%.1f %s", f, unit)
		}
	}
	return fmt.Sprintf("%.1f PB", f)
}
