package main

import (
	"fmt"
	"strings"

	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/egoist/mygo/ui"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// pageRequests is the connections page (FlClash's 请求 page):
// live connections, recently closed ones and the DNS query log
// as three modules inside ONE card — tabs on top, the active
// module's table below, nothing floating outside the surface.
func (a *App) pageRequests(c *ui.Context) {
	a.mu.Lock()
	conns := a.conns
	recent := a.recent
	dnsTotal := a.dnsTotal
	a.mu.Unlock()

	cardGrow(c, 0, func() {
		segmented(c, []string{
			fmt.Sprintf("实时连接 %d", len(conns)),
			fmt.Sprintf("最近请求 %d", len(recent)),
			fmt.Sprintf("DNS 查询 %d", dnsTotal),
		}, a.reqTab, func(i int) { a.reqTab = i })
		ui.Box(c).Height(2)
		switch a.reqTab {
		case 1:
			a.recentTable(c, recent, 0)
		case 2:
			a.dnsLogView(c)
		default:
			a.liveToolbar(c)
			a.liveTable(c, conns, 0, true)
		}
	})
}

// liveToolbar is the live module's filter row: network type
// segments on the left, close-all on the right.
func (a *App) liveToolbar(c *ui.Context) {
	ui.Row(c).AlignItems(ui.Center).Gap(4).Children(func() {
		names := []string{"", "tcp", "udp"}
		idx := 0
		for i, n := range names {
			if a.reqNet == n {
				idx = i
			}
		}
		segmented(c, []string{"全部", "TCP", "UDP"}, idx, func(i int) { a.reqNet = names[i] })
		ui.Box(c).Grow(1)
		if iconButton(c, "断开全部连接", lucide.Eraser()).Clicked() {
			go func() {
				if err := a.cli.CloseAllConnections(); err != nil {
					a.setMsg("断开失败：%v", err)
				} else {
					a.setMsg("已断开全部连接")
				}
			}()
		}
	})
	ui.Box(c).Height(6)
}

// liveTable is the live-connections table (header band + zebra
// rows), shared by the requests page and the overview card.
// limit <= 0 renders every row; closable adds the per-row
// disconnect button.
func (a *App) liveTable(c *ui.Context, conns []core.Connection, limit int, closable bool) {
	connHeader(c, closable)
	shown := 0
	for _, cn := range conns {
		if a.reqNet != "" && !strings.EqualFold(cn.Network, a.reqNet) {
			continue
		}
		if limit > 0 && shown >= limit {
			break
		}
		cn := cn
		row := ui.Row(c).FillWidth().Padding(9, 16).Gap(10).AlignItems(ui.Center)
		if shown%2 == 1 {
			row.Background(colZebra)
		}
		shown++
		row.Children(func() {
			txt(c, orDash(cn.Process), fsSmall, false, colFaint).Width(110).SingleLine()
			txt(c, cn.Target, fsSmall, true, colText).Grow(1).SingleLine()
			txt(c, orDash(displayName(cn.Exit())), fsSmall, false, colMuted).Width(130).SingleLine()
			num(c, humanBytes(cn.Download), fsSmall, false, colText).Width(84).TextAlign(ui.End)
			num(c, humanBytes(cn.Upload), fsSmall, false, colMuted).Width(84).TextAlign(ui.End)
			num(c, humanBytes(cn.Download+cn.Upload), fsSmall, false, colMuted).Width(84).TextAlign(ui.End)
			if closable {
				if iconButton(c, "断开此连接", lucide.X()).Clicked() {
					go func() {
						if err := a.cli.CloseConnection(cn.ID); err != nil {
							a.setMsg("断开失败：%v", err)
						}
					}()
				}
			} else {
				ui.Box(c).Width(31)
			}
		})
	}
	if shown == 0 {
		emptyRow(c, "暂无活动连接")
	}
}

// recentTable is the closed-connections table, same shape as
// liveTable with a close-time first column.
func (a *App) recentTable(c *ui.Context, recent []recentConn, limit int) {
	tableHeader(c, func(closable bool) {
		txt(c, "关闭时间", fsMicro, true, colFaint).Width(76)
		txt(c, "目标", fsMicro, true, colFaint).Grow(1)
		txt(c, "出口", fsMicro, true, colFaint).Width(130)
		txt(c, "下载", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		txt(c, "上传", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		txt(c, "流量", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		ui.Box(c).Width(31)
	})
	for i, rc := range recent {
		if limit > 0 && i >= limit {
			break
		}
		row := ui.Row(c).FillWidth().Padding(9, 16).Gap(10).AlignItems(ui.Center)
		if i%2 == 1 {
			row.Background(colZebra)
		}
		rc := rc
		row.Children(func() {
			num(c, rc.ClosedAt.Format("15:04:05"), fsSmall, false, colFaint).Width(76)
			txt(c, rc.Target, fsSmall, true, colText).Grow(1).SingleLine()
			txt(c, orDash(displayName(rc.Exit())), fsSmall, false, colMuted).Width(130).SingleLine()
			num(c, humanBytes(rc.Download), fsSmall, false, colText).Width(84).TextAlign(ui.End)
			num(c, humanBytes(rc.Upload), fsSmall, false, colMuted).Width(84).TextAlign(ui.End)
			num(c, humanBytes(rc.Download+rc.Upload), fsSmall, false, colMuted).Width(84).TextAlign(ui.End)
			ui.Box(c).Width(31)
		})
	}
	if len(recent) == 0 {
		emptyRow(c, "暂无已关闭的连接")
	}
}

// tableHeader is the shared column caption band: a faint inset rule
// across the card so the columns read as a data region, not prose.
func tableHeader(c *ui.Context, build func(closable bool)) {
	ui.Row(c).FillWidth().Padding(8, 16).Gap(10).Background(colInset).Children(func() {
		build(false)
	})
}

// connHeader is the live table's column caption band.
func connHeader(c *ui.Context, closable bool) {
	ui.Row(c).FillWidth().Padding(8, 16).Gap(10).Background(colInset).Children(func() {
		txt(c, "进程", fsMicro, true, colFaint).Width(110)
		txt(c, "目标", fsMicro, true, colFaint).Grow(1)
		txt(c, "出口", fsMicro, true, colFaint).Width(130)
		txt(c, "下载", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		txt(c, "上传", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		txt(c, "流量", fsMicro, true, colFaint).Width(84).TextAlign(ui.End)
		ui.Box(c).Width(31)
	})
}

func emptyRow(c *ui.Context, s string) {
	ui.Row(c).FillWidth().Padding(14, 16).Children(func() {
		txt(c, s, fsSmall, false, colFaint)
	})
}
