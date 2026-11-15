package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

type trayPresentation struct {
	CoreReady bool
	TUNWanted bool
	Active    bool
}

func (a *App) currentTrayPresentation() trayPresentation {
	a.mu.Lock()
	ready, sample := a.coreOK, a.capture
	a.mu.Unlock()
	wanted := a.settings().TUNEnabled
	active := ready && sample.Active && sample.TUN == wanted && sample.PID > 0 && a.sup != nil && sample.PID == a.sup.PID()
	return trayPresentation{CoreReady: ready, TUNWanted: wanted, Active: active}
}

func (a *App) initTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIconPNG(false), ToolTip: "ClashGo · 代理未生效"})
	if err != nil {
		log.Printf("clashgo: tray: %v", err)
		return
	}
	a.tray = tray
	// Clicking the tray icon opens the main window. Registering
	// the handler is also what makes the item activatable at all:
	// without a listener the panel does not answer activation and
	// clicks go nowhere (the long-standing "tray click does
	// nothing" bug).
	tray.OnClick(func() { a.openWindow() })
	a.refreshTray()
}

func (a *App) refreshTray() {
	a.onUI(a.refreshTrayOnUI)
}

func (a *App) refreshTrayOnUI() {
	if a.tray == nil {
		return
	}
	state := a.currentTrayPresentation()
	if a.trayState != nil && *a.trayState == state {
		return
	}
	if err := a.tray.SetIcon(trayIconPNG(state.Active), false); err != nil {
		log.Printf("clashgo: tray state: %v", err)
		return
	}
	captureLabel := "切换到 TUN 模式"
	if state.TUNWanted {
		captureLabel = "切换到系统代理"
	}
	status := "代理未生效"
	if state.Active {
		status = "代理已生效"
	} else if !state.CoreReady {
		status = "代理未生效 · 核心未就绪"
	}
	a.tray.SetToolTip("ClashGo · " + status)
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "ClashGo · " + status},
		{Label: "打开主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.openWindow() }},
		{Label: captureLabel, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleCapture() }},
		{Label: "重启核心", Click: func(*mygo.MenuItem, *mygo.Window) { a.restartCore() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
	a.tray.SetMenu(menu)
	a.trayState = &state
}

// openWindow shows the main window, creating it on first use. The
// window is never destroyed by a user close: OnClose hides it into
// the tray (Phase 1 bug: closing the window quit the whole app,
// tray included). Only a flagged quit lets the close through.
func (a *App) openWindow() {
	a.mu.Lock()
	w := a.win
	a.mu.Unlock()
	if w != nil {
		w.Show()
		w.Focus()
		a.mu.Lock()
		a.windowOpen = true
		a.mu.Unlock()
		return
	}
	w = mygo.NewWindow(mygo.WindowOptions{
		Title:   "ClashGo",
		Width:   920,
		Height:  620,
		Content: ui.View(a.view),
	})
	if err := w.SetIcon(trayIconPNG(true)); err != nil {
		log.Printf("clashgo: window icon: %v", err)
	}
	w.OnClose(func(e *mygo.CloseEvent) {
		if a.quitting.Load() {
			return // real quit: let it close
		}
		e.PreventDefault()
		w.Hide()
		a.mu.Lock()
		a.windowOpen = false
		a.mu.Unlock()
	})
	w.OnClosed(func() {
		a.mu.Lock()
		a.win = nil
		a.windowOpen = false
		a.mu.Unlock()
	})
	a.mu.Lock()
	a.win = w
	a.windowOpen = true
	a.mu.Unlock()
}
