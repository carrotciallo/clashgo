package main

import (
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/sysproxy"
)

const readyTimeout = 20 * time.Second

var zeroTime time.Time

// commitConfiguration is called with opMu held. No candidate settings are
// published until validation succeeds. UI preference writes share stateMu.
func (a *App) commitConfiguration(next config.Settings, source string, replaceSource, activate bool) error {
	if next.ActiveProfile != "" && !config.ValidProfileName(next.ActiveProfile) {
		return fmt.Errorf("invalid profile name")
	}
	rendered, err := config.RenderChecked(source, a.reg.Sources(a.store.ProvidersDir()), next)
	if err != nil {
		return err
	}
	bin := core.FindCore(next.CorePath)
	if bin == "" {
		return fmt.Errorf("core executable unavailable")
	}
	a.stateMu.Lock()
	if a.settingsBusy {
		a.stateMu.Unlock()
		return fmt.Errorf("configuration transaction busy")
	}
	a.settingsBusy = true
	old := a.set
	oldBin := a.coreBin
	oldClient, oldProxy := a.cli, a.px
	a.stateMu.Unlock()
	defer func() { a.stateMu.Lock(); a.settingsBusy = false; a.stateMu.Unlock() }()
	// Preserve unrelated preferences edited while the operation was queued.
	next.Services = old.Services
	next.CollapsedSubs = old.CollapsedSubs
	next.SilentAutostart = old.SilentAutostart
	next.DefaultProfile = old.DefaultProfile
	changes := []config.FileChange{}
	if replaceSource {
		changes = append(changes, config.FileChange{Path: a.store.ProfilePath(next.ActiveProfile), Data: []byte(source)})
	}
	runtimePath := a.store.RenderedPath()
	if activate {
		b, e := json.MarshalIndent(next, "", "  ")
		if e != nil {
			return e
		}
		changes = append(changes, config.FileChange{Path: runtimePath, Data: []byte(rendered)}, config.FileChange{Path: a.store.SettingsPath(), Data: b})
	}
	wasRunning := a.sup.Running()
	restart := old.MixedPort != next.MixedPort || old.ControllerPort != next.ControllerPort || old.TUNEnabled != next.TUNEnabled || oldBin != bin
	nextClient := core.NewClient(fmt.Sprintf("http://127.0.0.1:%d", next.ControllerPort), next.ControllerSecret)
	validate := func(paths map[string]string) error {
		if activate && next.TUNEnabled && (restart || !wasRunning) {
			if err := core.CheckTUNLaunch(); err != nil {
				return err
			}
		}
		if p, ok := paths[runtimePath]; ok {
			return core.ValidateConfig(bin, a.store.RuntimeDir(), p)
		}
		f, e := os.CreateTemp(a.store.RuntimeDir(), ".validate-*")
		if e != nil {
			return e
		}
		defer os.Remove(f.Name())
		_, e = f.WriteString(rendered)
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
		return core.ValidateConfig(bin, a.store.RuntimeDir(), f.Name())
	}
	var apply, undo func() error
	if activate {
		apply = func() error {
			if restart || !wasRunning {
				if err := oldProxy.Disarm(); err != nil {
					return err
				}
				a.sup.Stop()
				if err := a.sup.SetBinary(bin); err != nil {
					return err
				}
				if err := a.sup.Start(runtimePath); err != nil {
					return err
				}
				if err := nextClient.WaitReady(readyTimeout); err != nil {
					a.sup.Stop()
					return err
				}
				if !a.sup.Running() {
					return fmt.Errorf("candidate core exited during readiness")
				}
			} else {
				if err := oldClient.ReloadConfig(runtimePath); err != nil {
					return err
				}
			}
			if err := a.waitTUNReady(nextClient, next.TUNEnabled); err != nil {
				return err
			}
			px := oldProxy
			if restart {
				px = sysproxy.New(a.store.StateDir, next.MixedPort)
			}
			if next.TUNEnabled {
				if err := px.Neutralize(); err != nil {
					return err
				}
			} else if next.ArmProxyOnStart {
				if err := px.Arm(); err != nil {
					return err
				}
			}
			a.stateMu.Lock()
			a.set = next
			a.cli = nextClient
			a.coreBin = bin
			a.px = px
			a.stateMu.Unlock()
			a.store.SetActiveProfile(next.ActiveProfile)
			a.mu.Lock()
			a.coreOK = true
			a.coreErr = ""
			a.prevSample = zeroTime
			a.mu.Unlock()
			return nil
		}
		undo = func() error {
			a.stateMu.Lock()
			a.set = old
			a.cli = oldClient
			a.px = oldProxy
			a.coreBin = oldBin
			a.stateMu.Unlock()
			a.store.SetActiveProfile(old.ActiveProfile)
			var e error
			if restart || !wasRunning {
				a.sup.Stop()
				_ = a.sup.SetBinary(oldBin)
				if wasRunning {
					e = a.sup.Start(runtimePath)
					if e == nil {
						e = oldClient.WaitReady(readyTimeout)
					}
				}
			} else {
				e = oldClient.ReloadConfig(runtimePath)
			}
			if e == nil && wasRunning {
				e = a.waitTUNReady(oldClient, old.TUNEnabled)
			}
			a.mu.Lock()
			a.coreOK = wasRunning && e == nil
			a.mu.Unlock()
			if e != nil {
				a.sup.Stop()
				return fmt.Errorf("restore core: %w", e)
			}
			if wasRunning {
				if old.TUNEnabled {
					return oldProxy.Neutralize()
				}
				if old.ArmProxyOnStart {
					return oldProxy.Arm()
				}
			}
			return nil
		}
	}
	return config.CommitFiles(changes, validate, apply, undo)
}

func (a *App) saveProfileEditor() {
	name, content, base := a.editProfileName, a.editProfileText, a.editProfileBase
	a.operate(func() {
		current, err := a.profileContent(name)
		if err != nil {
			a.setMsg("读取配置失败：%v", err)
			return
		}
		if current != base {
			a.setMsg("配置已被其他操作修改，请重新打开编辑器")
			return
		}
		if err := a.commitProfile(name, content); err != nil {
			a.setMsg("配置未保存：%v", err)
			return
		}
		a.onUI(func() {
			if a.editProfileName == name && a.editProfileText == content {
				a.dlgEditProfile = false
			}
		})
		a.setMsg("配置已验证并保存")
	})
}

func (a *App) commitProfile(name, content string) error {
	if name != "" && !config.ValidProfileName(name) {
		return fmt.Errorf("invalid profile name")
	}
	next := a.settings()
	active := name == next.ActiveProfile
	next.ActiveProfile = name
	return a.commitConfiguration(next, content, true, active)
}
