package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// trayIconPNG draws the placeholder tray glyph: a lavender-blue disc.
func trayIconPNG() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 32))
	ink := color.NRGBA{R: 139, G: 156, B: 249, A: 255}
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			dx, dy := x-16, y-16
			if dx*dx+dy*dy <= 110 {
				img.SetNRGBA(x, y, ink)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func (a *App) initTray() {
	tray, err := mygo.NewTray(mygo.TrayOptions{Icon: trayIconPNG(), ToolTip: "ClashGo"})
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
	if a.tray == nil {
		return
	}
	captureLabel := "切换到 TUN 模式"
	if a.set.TUNEnabled {
		captureLabel = "切换到系统代理"
	}
	status := "核心未就绪"
	if a.coreHealthy() {
		status = "核心运行中"
	}
	menu := mygo.NewMenu([]*mygo.MenuItem{
		{Label: "ClashGo · " + status},
		{Label: "打开主窗口", Click: func(*mygo.MenuItem, *mygo.Window) { a.openWindow() }},
		{Label: captureLabel, Click: func(*mygo.MenuItem, *mygo.Window) { a.toggleCapture() }},
		{Label: "重启核心", Click: func(*mygo.MenuItem, *mygo.Window) { a.restartCore() }},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	})
	a.tray.SetMenu(menu)
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
