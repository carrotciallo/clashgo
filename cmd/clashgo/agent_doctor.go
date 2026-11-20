package main

// doctor: the post-deploy acceptance battery as a single command.
// Every check reports pass, fail or skip (with the reason) plus the
// evidence it saw, so an agent never has to re-derive health from a
// pile of shell one-liners. Exit code is non-zero when any check
// fails; skips do not fail the run.

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"` // pass | fail | skip
	Detail string `json:"detail,omitempty"`
}

type doctorReport struct {
	Schema int           `json:"schema"`
	OK     bool          `json:"ok"`
	Time   time.Time     `json:"time"`
	Checks []doctorCheck `json:"checks"`
}

func parseNoNewPrivs(statusText string) (int, bool) {
	for _, line := range strings.Split(statusText, "\n") {
		if strings.HasPrefix(line, "NoNewPrivs:") {
			n, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "NoNewPrivs:")))
			return n, err == nil
		}
	}
	return 0, false
}

func parseCapEff(statusText string) (uint64, bool) {
	for _, line := range strings.Split(statusText, "\n") {
		if strings.HasPrefix(line, "CapEff:") {
			v, err := strconv.ParseUint(strings.TrimSpace(strings.TrimPrefix(line, "CapEff:")), 16, 64)
			return v, err == nil
		}
	}
	return 0, false
}

func procStatus(pid int) (string, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	return string(data), err
}

// countAppProcesses counts running processes whose executable is
// this binary, excluding the CLI process itself.
func countAppProcesses() int {
	exe, err := os.Executable()
	if err != nil {
		return 0
	}
	if resolved, err := filepath.EvalSymlinks(exe); err == nil {
		exe = resolved
	}
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0
	}
	count := 0
	self := os.Getpid()
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil || pid == self {
			continue
		}
		target, err := os.Readlink(filepath.Join("/proc", e.Name(), "exe"))
		if err != nil {
			continue
		}
		if resolved, err := filepath.EvalSymlinks(target); err == nil {
			target = resolved
		}
		if target == exe {
			count++
		}
	}
	return count
}

func httpProbe(client *http.Client, rawURL string) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return 0, nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return resp.StatusCode, body, nil
}

func runDoctor(store *config.Store) doctorReport {
	rep := doctorReport{Schema: agentSchema, Time: time.Now(), OK: true}
	add := func(name, status, detail string) {
		rep.Checks = append(rep.Checks, doctorCheck{Name: name, Status: status, Detail: detail})
		if status == "fail" {
			rep.OK = false
		}
	}

	st, live := cliFetchStatus(store)
	set, _ := store.LoadSettings()
	mixedPort := set.MixedPort
	if mixedPort == 0 {
		mixedPort = st.Capture.MixedPort
	}
	ctrlPort := set.ControllerPort
	if ctrlPort == 0 {
		ctrlPort = st.Capture.ControllerPort
	}

	if live {
		add("instance_running", "pass", fmt.Sprintf("pid %d, heartbeat %s", st.Instance.PID, st.Instance.Heartbeat.Format(time.RFC3339)))
	} else {
		add("instance_running", "fail", "控制口无应答，状态来自磁盘快照")
	}

	if n := countAppProcesses(); n == 1 {
		add("single_instance", "pass", "恰好一个实例进程")
	} else {
		add("single_instance", "fail", fmt.Sprintf("同路径进程数 %d", n))
	}

	if live && st.Instance.PID > 0 {
		if text, err := procStatus(st.Instance.PID); err == nil {
			if nnp, ok := parseNoNewPrivs(text); ok && nnp == 0 {
				add("app_no_new_privs", "pass", "NoNewPrivs=0，核心可凭文件能力建 TUN")
			} else {
				add("app_no_new_privs", "fail", fmt.Sprintf("NoNewPrivs=%d", nnp))
			}
		} else {
			add("app_no_new_privs", "fail", err.Error())
		}
	} else {
		add("app_no_new_privs", "skip", "无实例")
	}

	coreUp := live && st.Core.Running && st.Core.PID > 0
	if coreUp {
		add("core_process", "pass", fmt.Sprintf("pid %d, version %s", st.Core.PID, st.Core.Version))
	} else {
		add("core_process", "fail", "核心未运行")
	}

	tunMode := st.Capture.Mode == "tun"
	if coreUp && tunMode {
		if text, err := procStatus(st.Core.PID); err == nil {
			if caps, ok := parseCapEff(text); ok && caps&(1<<12) != 0 && caps&(1<<13) != 0 {
				add("core_caps", "pass", fmt.Sprintf("CapEff %#x 含 NET_ADMIN+NET_RAW", caps))
			} else {
				add("core_caps", "fail", fmt.Sprintf("CapEff %#x 缺 NET_ADMIN/NET_RAW", caps))
			}
		} else {
			add("core_caps", "fail", err.Error())
		}
	} else {
		add("core_caps", "skip", "仅 TUN 模式需要")
	}

	if tunMode {
		if ifc, err := net.InterfaceByName(config.TunDeviceName); err == nil {
			if ifc.Flags&net.FlagUp != 0 && ifc.MTU == 1500 {
				add("tun_device", "pass", config.TunDeviceName+" 已 UP，MTU 1500")
			} else {
				add("tun_device", "fail", fmt.Sprintf("%s flags=%v mtu=%d", config.TunDeviceName, ifc.Flags, ifc.MTU))
			}
		} else {
			add("tun_device", "fail", config.TunDeviceName+" 网卡不存在")
		}
	} else {
		add("tun_device", "skip", "当前不是 TUN 模式")
	}

	if st.Capture.Mode == "system" {
		out, err := exec.Command("gsettings", "get", "org.gnome.system.proxy", "mode").Output()
		if err != nil {
			add("system_proxy_armed", "skip", "gsettings 不可用")
		} else if strings.TrimSpace(string(out)) == "'manual'" && st.Capture.SystemProxyArmed {
			add("system_proxy_armed", "pass", fmt.Sprintf("GNOME 系统代理 manual，指向 %d", mixedPort))
		} else {
			add("system_proxy_armed", "fail", fmt.Sprintf("GNOME 模式 %s，应用自报 armed=%v", strings.TrimSpace(string(out)), st.Capture.SystemProxyArmed))
		}
	} else {
		add("system_proxy_armed", "skip", "当前不是系统代理模式")
	}

	plain := &http.Client{Timeout: 10 * time.Second}
	if code, _, err := httpProbe(plain, fmt.Sprintf("http://127.0.0.1:%d/version", ctrlPort)); err == nil && code == http.StatusUnauthorized {
		add("controller_auth", "pass", "未授权访问控制器返回 401")
	} else if err == nil {
		add("controller_auth", "fail", fmt.Sprintf("未授权访问返回 %d，期望 401", code))
	} else {
		add("controller_auth", "fail", err.Error())
	}

	proxyClient := &http.Client{
		Timeout:   10 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(&url.URL{Scheme: "http", Host: fmt.Sprintf("127.0.0.1:%d", mixedPort)})},
	}
	if code, _, err := httpProbe(proxyClient, "http://www.gstatic.com/generate_204"); err == nil && code == 204 {
		add("proxy_egress", "pass", fmt.Sprintf("经混合端口 %d 出站 204", mixedPort))
	} else if err == nil {
		add("proxy_egress", "fail", fmt.Sprintf("经混合端口出站返回 %d", code))
	} else {
		add("proxy_egress", "fail", err.Error())
	}

	if tunMode {
		direct := &http.Client{Timeout: 10 * time.Second, Transport: &http.Transport{Proxy: nil}}
		if code, _, err := httpProbe(direct, "http://www.gstatic.com/generate_204"); err == nil && code == 204 {
			add("tun_egress", "pass", "直连（经 TUN 路由）出站 204")
		} else if err == nil {
			add("tun_egress", "fail", fmt.Sprintf("直连出站返回 %d", code))
		} else {
			add("tun_egress", "fail", err.Error())
		}
	} else {
		add("tun_egress", "skip", "仅 TUN 模式有意义")
	}

	if code, body, err := httpProbe(proxyClient, "https://api.ipify.org"); err == nil && code == 200 && net.ParseIP(strings.TrimSpace(string(body))) != nil {
		ip := strings.TrimSpace(string(body))
		detail := "出口 IP " + ip
		if st.Egress.IP != "" && st.Egress.IP != ip {
			detail += fmt.Sprintf("（应用自报 %s）", st.Egress.IP)
		}
		add("egress_ip", "pass", detail)
	} else if err == nil {
		add("egress_ip", "fail", fmt.Sprintf("出口 IP 查询返回 %d", code))
	} else {
		add("egress_ip", "fail", err.Error())
	}

	return rep
}
