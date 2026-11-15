package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/sysproxy"
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// pageSettings follows FlClash's tools page: section captions
// over grouped cards whose rows carry a leading icon — switches
// inline for booleans, chevron rows for anything that opens a
// sub-page or an external target. The port/core form lives one
// level down in pageCoreSettings, like FlClash's 进阶配置.
func (a *App) pageSettings(c *ui.Context) {
	if a.setCorePage {
		a.pageCoreSettings(c)
		return
	}

	sectionHeader(c, "核心", "")
	listCard(c, func() {
		navRow(c, lucide.Settings(), "核心设置", fmt.Sprintf("%d · %d", a.settings().MixedPort, a.settings().ControllerPort), false, func() {
			a.setCorePage = true
		})
		hairline(c)
		actionRow(c, "立即恢复系统代理", lucide.RotateCcw(), false, func() {
			a.operate(a.restoreProxy)
		})
	})

	sectionHeader(c, "启动", "")
	listCard(c, func() {
		if a.autoSw == nil {
			b := autostartEnabled()
			a.autoSw = &b
		}
		switchRow(c, lucide.Rocket(), "开机自启", a.autoSw)
		if *a.autoSw != autostartEnabled() {
			if err := setAutostart(*a.autoSw); err != nil {
				a.setMsg("自启设置失败：%v", err)
				b := autostartEnabled()
				a.autoSw = &b
			} else {
				a.setMsg("自启已更新")
			}
		}
		hairline(c)
		silent := a.settings().SilentAutostart
		switchRow(c, lucide.Moon(), "自启时静默启动（只进托盘，不弹窗口）", &silent)
		if silent != a.settings().SilentAutostart {
			if err := a.changeSettings(func(s *config.Settings) { s.SilentAutostart = silent }); err != nil {
				a.setMsg("保存静默启动设置失败：%v", err)
			} else {
				a.setMsg("静默启动设置已保存")
			}
		}
	})

	sectionHeader(c, "路径", "")
	listCard(c, func() {
		iconPathRow(c, lucide.ScrollText(), "主配置", a.store.MasterPath())
		hairline(c)
		iconPathRow(c, lucide.Cpu(), "核心", a.corePath())
		hairline(c)
		iconPathRow(c, lucide.FolderCog(), "状态目录", a.store.StateDir)
		hairline(c)
		actionRow(c, "打开配置目录", lucide.FolderOpen(), false, func() {
			_ = exec.Command("xdg-open", a.store.ConfigDir).Start()
		})
		hairline(c)
		actionRow(c, "用编辑器打开主配置", lucide.FilePen(), false, func() {
			_ = exec.Command("xdg-open", a.store.MasterPath()).Start()
		})
	})

	sectionHeader(c, "其他", "")
	listCard(c, func() {
		actionRow(c, "打开状态目录", lucide.FolderOpen(), false, func() {
			_ = exec.Command("xdg-open", a.store.StateDir).Start()
		})
	})
}

// pageCoreSettings is the 核心设置 sub-page: ports, core path
// and the arm-on-start switch, with the apply actions.
func (a *App) pageCoreSettings(c *ui.Context) {
	backRow(c, "核心设置", func() { a.setCorePage = false })
	ui.Box(c).Height(8)
	listCard(c, func() {
		settingRow(c, "混合端口", func() {
			ov.TextBox(c, &a.setMixed, ov.TextBoxOptions{}).Label("mixed-port").Width(130)
		})
		hairline(c)
		settingRow(c, "控制端口", func() {
			ov.TextBox(c, &a.setCtrl, ov.TextBoxOptions{}).Label("controller port").Width(130)
		})
		hairline(c)
		settingRow(c, "核心路径", func() {
			ov.TextBox(c, &a.setCorePath, ov.TextBoxOptions{}).Label("留空 = 自动发现").Grow(1)
		})
		hairline(c)
		ui.Row(c).FillWidth().Padding(12, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			txt(c, "启动核心就绪后自动接管", fsBody, false, colText).Grow(1)
			ov.Switch(c, &a.setArm, "启动核心就绪后自动接管", ov.SwitchOptions{HideLabel: true})
		})
		hairline(c)
		ui.Row(c).FillWidth().Padding(12, 16).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "立即恢复系统代理", ov.ButtonOutline).Clicked() {
				a.operate(a.restoreProxy)
			}
			if ov.Button(c, "保存并应用", ov.ButtonPrimary).Clicked() {
				a.saveSettings()
			}
		})
	})
}

// switchRow is an icon + label + switch line in a list card.
func switchRow(c *ui.Context, icon *ui.SVG, label string, state *bool) {
	ui.Row(c).FillWidth().Padding(12, 16).Gap(11).AlignItems(ui.Center).Children(func() {
		ui.Row(c).TextColor(colMuted).Children(func() { ov.Icon(c, icon).Size(17, 17) })
		txt(c, label, fsBody, false, colText).Grow(1)
		ov.Switch(c, state, label, ov.SwitchOptions{HideLabel: true})
	})
}

// iconPathRow is a read-only path line with a leading icon.
func iconPathRow(c *ui.Context, icon *ui.SVG, label, value string) {
	ui.Row(c).FillWidth().Padding(12, 16).Gap(11).AlignItems(ui.Center).Children(func() {
		ui.Row(c).TextColor(colMuted).Children(func() { ov.Icon(c, icon).Size(17, 17) })
		txt(c, label, fsBody, false, colText).Width(64)
		txt(c, value, fsMicro, false, colFaint).Grow(1).SingleLine()
	})
}

// settingRow is one label + right-hand control line in a list card.
func settingRow(c *ui.Context, label string, control func()) {
	ui.Row(c).FillWidth().Padding(12, 16).Gap(11).AlignItems(ui.Center).Children(func() {
		txt(c, label, fsBody, false, colText).Width(84)
		control()
	})
}

func pathRow(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Padding(12, 16).Gap(11).AlignItems(ui.Center).Children(func() {
		txt(c, label, fsBody, false, colText).Width(84)
		txt(c, value, fsMicro, false, colFaint).Grow(1).SingleLine()
	})
}

func (a *App) restoreProxy() {
	if err := a.proxyManager().Disarm(); err != nil {
		a.setMsg("恢复系统代理失败：%v", err)
		return
	}
	a.setMsg("系统代理已释放（第三方设置保持不变）")
	a.refreshTray()
}

func (a *App) saveSettings() {
	mixed, err1 := strconv.Atoi(strings.TrimSpace(a.setMixed))
	ctrl, err2 := strconv.Atoi(strings.TrimSpace(a.setCtrl))
	if err1 != nil || err2 != nil || mixed < 1 || mixed > 65535 || ctrl < 1 || ctrl > 65535 || mixed == ctrl {
		a.setMsg("端口不合法或相同")
		return
	}
	// Capture form values on the UI thread, never from the worker.
	path, arm := strings.TrimSpace(a.setCorePath), a.setArm
	a.operate(func() {
		bin := core.FindCore(path)
		if bin == "" {
			a.setMsg("核心路径不可用")
			return
		}
		old := a.settings()
		changed := mixed != old.MixedPort || ctrl != old.ControllerPort || bin != a.corePath()
		if err := a.changeSettings(func(s *config.Settings) {
			s.MixedPort, s.ControllerPort, s.CorePath, s.ArmProxyOnStart = mixed, ctrl, path, arm
		}); err != nil {
			a.setMsg("保存设置失败：%v", err)
			return
		}
		if changed {
			px := a.proxyManager()
			wasArmed := px.Armed()
			px.Disarm()
			a.sup.Stop()
			if err := a.sup.SetBinary(bin); err != nil {
				a.setMsg("切换核心失败：%v", err)
				return
			}
			next := a.settings()
			a.stateMu.Lock()
			a.coreBin = bin
			a.cli = core.NewClient(fmt.Sprintf("http://127.0.0.1:%d", ctrl), next.ControllerSecret)
			a.px = sysproxy.New(a.store.StateDir, mixed)
			a.stateMu.Unlock()
			a.proxyManager().BeginSession()
			a.mu.Lock()
			a.coreOK = false
			a.mu.Unlock()
			a.applyConfig()
			if wasArmed && !next.TUNEnabled && a.coreHealthy() {
				a.proxyManager().Arm()
			}
		}
		a.refreshTray()
	})
}

func autostartUnitPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "systemd", "user", "clashgo.service")
}

func autostartEnabled() bool {
	home, _ := os.UserHomeDir()
	_, err := os.Stat(filepath.Join(home, ".config", "systemd", "user", "default.target.wants", "clashgo.service"))
	return err == nil
}

func setAutostart(on bool) error {
	unit := autostartUnitPath()
	if !on {
		_ = exec.Command("systemctl", "--user", "disable", "--now", "clashgo.service").Run()
		return os.Remove(unit)
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	content := fmt.Sprintf("[Unit]\nDescription=ClashGo\nAfter=graphical-session.target\n\n[Service]\nExecStart=%s --autostart\nRestart=on-failure\n\n[Install]\nWantedBy=default.target\n", exe)
	if err := os.MkdirAll(filepath.Dir(unit), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(unit, []byte(content), 0o644); err != nil {
		return err
	}
	_ = exec.Command("systemctl", "--user", "daemon-reload").Run()
	return exec.Command("systemctl", "--user", "enable", "clashgo.service").Run()
}
