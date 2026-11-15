package main

import (
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// pageServices is the service-status sub-page behind the dashboard
// card: every check with its monogram badge and result, plus a
// manage mode choosing which services the card (and the checks)
// cover.
func (a *App) pageServices(c *ui.Context) {
	a.mu.Lock()
	results := a.svcResults
	a.mu.Unlock()

	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		if iconButton(c, "返回", lucide.ArrowLeft()).Clicked() {
			a.showServices = false
			a.svcManage = false
		}
		ui.Box(c).Grow(1)
		manageIcon := lucide.SlidersHorizontal()
		if iconButton(c, "管理服务", manageIcon).Clicked() {
			a.svcManage = !a.svcManage
		}
		if iconButton(c, "重新检测", lucide.Zap()).Clicked() {
			go a.checkServices()
		}
	})

	defs := a.enabledServices()
	if a.svcManage {
		defs = serviceDefs
	}
	listCard(c, func() {
		for i, d := range defs {
			if i > 0 {
				hairline(c)
			}
			d := d
			ui.Row(c).FillWidth().Padding(10, 16).Gap(12).AlignItems(ui.Center).Children(func() {
				svcBadge(c, d.Mono)
				txt(c, d.Name, fsBody, false, colText).Grow(1)
				if a.svcManage {
					if a.svcToggle == nil {
						a.svcToggle = map[string]*bool{}
					}
					b, ok := a.svcToggle[d.Name]
					if !ok {
						v := a.serviceEnabled(d.Name)
						b = &v
						a.svcToggle[d.Name] = b
					}
					ov.Switch(c, b, d.Name, ov.SwitchOptions{HideLabel: true})
					if *b != a.serviceEnabled(d.Name) {
						a.setServiceEnabled(d.Name, *b)
					}
				} else {
					val, col := svcStatusText(results[d.Name])
					tag(c, val, col)
				}
			})
		}
		if len(defs) == 0 {
			emptyRow(c, "没有启用的服务")
		}
	})
}

// svcBadge is the rounded monogram tile in front of a service row,
// standing in for the brand logo.
func svcBadge(c *ui.Context, mono string) {
	ui.Column(c).Width(32).Height(32).Radius(9).Background(colInset).
		AlignItems(ui.Center).Padding(8).TextColor(colMuted).Children(func() {
		txt(c, mono, fsSmall, true, colMuted)
	})
}
