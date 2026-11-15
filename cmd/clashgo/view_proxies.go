package main

import (
	"fmt"
	"strings"

	"github.com/carrotciallo/clashgo/internal/core"
	"github.com/carrotciallo/clashgo/internal/subs"
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// pageProxies is the nodes page: an egress strip, then one section
// per subscription listing that subscription's own nodes. Picking a
// node row routes the egress through it. Subscriptions are node
// pools — the only grouping on this page is by subscription.
func (a *App) pageProxies(c *ui.Context) {
	_, _, _, _, _, _, _ = a.snapshot()
	a.mu.Lock()
	providers := a.providers
	delays := a.delays
	a.mu.Unlock()
	subsList := append([]subs.Subscription{}, a.reg.Subs...)
	chain := a.egressChain()
	leaf := ""
	if len(chain) > 0 {
		leaf = chain[len(chain)-1]
	}
	// The shell already renders the page title and status; this row
	// only carries the page's actions.
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		ui.Row(c).Grow(1).Children(func() {})
		if iconButton(c, "添加订阅", lucide.CirclePlus()).Clicked() {
			a.dlgAddSub = true
		}
		if iconButton(c, "测速全部", lucide.Zap()).Clicked() {
			a.testDelay("节点选择")
		}
	})

	// Egress strip: where traffic leaves right now, hop by hop.
	listCard(c, func() {
		ui.Row(c).FillWidth().Padding(13, 16).Gap(10).AlignItems(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(4).Children(func() {
				caption(c, "当前出口")
				if len(chain) < 2 {
					txt(c, "—", fsValue, true, colFaint)
					return
				}
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					// The root picker's own name is plumbing, not a
					// hop worth showing (same cut as the overview).
					hops := make([]string, 0, len(chain)-1)
					for _, h := range chain[1:] {
						hops = append(hops, displayName(h))
					}
					txt(c, strings.Join(hops, " → "), fsValue, true, colText).SingleLine()
				})
			})
		})
	})

	if len(subsList) == 0 {
		card(c, func() {
			txt(c, "还没有订阅。点右上角「＋ 添加订阅」添加第一个。", fsSmall, false, colFaint)
		})
	}
	for _, s := range subsList {
		a.subSection(c, s, providers, delays, leaf)
	}
}

// subSection renders one subscription: a header (name, kind tag,
// stats, refresh/delete) over its node rows.
func (a *App) subSection(c *ui.Context, s subs.Subscription, providers map[string][]core.Proxy, delays map[string]map[string]int, leaf string) {
	collapsed := a.subCollapsed(s.Name)
	listCard(c, func() {
		ui.Row(c).FillWidth().Padding(12, 16).Gap(9).AlignItems(ui.Center).Children(func() {
			chev, chevLabel := lucide.ChevronDown(), "折叠 "+s.Name
			if collapsed {
				chev, chevLabel = lucide.ChevronRight(), "展开 "+s.Name
			}
			if iconButton(c, chevLabel, chev).Clicked() {
				a.toggleSubCollapsed(s.Name)
			}
			head := ui.Column(c).Grow(1).Gap(3)
			head.Children(func() {
				ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
					txt(c, s.Name, fsValue, true, colText).SingleLine()
					if s.Split {
						tag(c, "整包配置", colMuted)
					}
				})
				info := fmt.Sprintf("%d 个节点", s.NodeCount)
				if !s.UpdatedAt.IsZero() {
					info += " · 更新于 " + s.UpdatedAt.Format("01-02 15:04")
				}
				if !subs.ProviderFileExists(a.store.ProvidersDir(), s.Name) {
					info += " · 未抓取"
				}
				if collapsed && leaf != "" {
					for _, p := range providers[s.Name] {
						if p.Name == leaf {
							info += " · 当前 " + leaf
							break
						}
					}
				}
				txt(c, info, fsMicro, false, colFaint).SingleLine()
				if s.LastError != "" {
					txt(c, "上次刷新失败："+s.LastError, fsMicro, false, colDanger).SingleLine()
				}
			})
			if head.Clicked() {
				a.toggleSubCollapsed(s.Name)
			}
			if iconButton(c, "测速 "+s.Name, lucide.Zap()).Clicked() {
				a.testDelay("节点选择")
			}
			if iconButton(c, "刷新 "+s.Name, lucide.RefreshCw()).Clicked() {
				a.refreshSubscription(s.Name)
			}
			if iconButton(c, "删除 "+s.Name, lucide.Trash()).Clicked() {
				a.removeSubscription(s.Name)
			}
		})
		if !collapsed {
			hairline(c)
			a.providerSubRows(c, s, providers, delays, leaf)
		}
	})
}

// nodeDelay reads a node's last measured delay: the delay table
// first (group and single-node tests land there), then the core's
// own history. ok=false means never measured; timeout=true means
// measured and failed.
func nodeDelay(p core.Proxy, delays map[string]map[string]int) (ms int, timeout, ok bool) {
	if d, hit := delays["节点选择"][p.Name]; hit {
		if d > 0 {
			return d, false, true
		}
		return 0, true, true
	}
	if len(p.History) > 0 {
		if d := p.History[len(p.History)-1].Delay; d > 0 {
			return d, false, true
		}
	}
	return 0, false, false
}

// providerSubRows lists a plain subscription's own proxies as
// Shadowrocket-style rows: orange dot on the selected node, a round
// flag badge, name over a protocol sub-line, the delay in green,
// and an info button opening a detail strip. Picking a row routes
// the egress through it (useNode).
func (a *App) providerSubRows(c *ui.Context, s subs.Subscription, providers map[string][]core.Proxy, delays map[string]map[string]int, leaf string) {
	nodes := providers[s.Name]
	shown := 0
	for _, p := range nodes {
		if strings.HasPrefix(p.Name, "__") {
			continue
		}
		shown++
	}
	if shown == 0 {
		ui.Row(c).FillWidth().Padding(13, 16).Children(func() {
			txt(c, "暂无节点（核心未就绪或尚未抓取）。", fsSmall, false, colFaint)
		})
		return
	}
	idx := 0
	for _, p := range nodes {
		p := p
		if strings.HasPrefix(p.Name, "__") {
			continue
		}
		if idx > 0 {
			hairline(c)
		}
		idx++
		a.nodeRow(c, s, p, delays, leaf)
		if a.expandedNode == p.Name {
			a.nodeDetail(c, s, p, delays, leaf)
		}
	}
}

// nodeRow is one Shadowrocket-style node row.
func (a *App) nodeRow(c *ui.Context, s subs.Subscription, p core.Proxy, delays map[string]map[string]int, leaf string) {
	selected := p.Name == leaf
	code := regionCode(p.Name)
	ms, timeout, measured := nodeDelay(p, delays)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Padding(0, 4, 0, 0).Children(func() {
		a.nodeRowMain(c, p, selected, code, ms, timeout, measured)
		if iconButton(c, "节点详情 "+displayName(p.Name), lucide.Info()).Clicked() {
			if a.expandedNode == p.Name {
				a.expandedNode = ""
			} else {
				a.expandedNode = p.Name
			}
		}
	})
}

// nodeRowMain is the clickable body of the row (everything but
// the info button): dot slot, flag badge, name/proto column,
// delay. A plain row with its own Clicked state — not a Button
// wrapping a FillWidth row: that combination's hit rect ran
// under the trailing info button and swallowed its clicks
// (user-reported on the config page's rows, same pattern here).
func (a *App) nodeRowMain(c *ui.Context, p core.Proxy, selected bool, code string, ms int, timeout, measured bool) {
	main := ui.Row(c).Grow(1).Gap(11).AlignItems(ui.Center).Padding(10, 14).Radius(10)
	switch {
	case selected:
		main.Background(colWash)
	case main.Hovered():
		main.Background(colHover)
	}
	main.Label(displayName(p.Name))
	main.Children(func() {
		// Selection marker slot: always reserved so rows do not
		// shift when the selection moves. A slim accent bar reads
		// as a marker on a row rather than a stray glyph.
		ui.Row(c).Width(3).Height(20).Radius(2).Children(func() {
			if selected {
				ui.Box(c).Grow(1).FillHeight().Radius(2).Background(colAccent)
			}
		})
		ui.Box(c).Width(6)
		// Round flag badge: the circle-cut flag image; a globe
		// in a wash circle for unknown regions.
		if img := flagImage(code); img != nil {
			ui.Image(c, img).Size(28, 28)
		} else {
			ui.Row(c).Width(28).Height(28).Radius(999).Background(colInset).
				AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				txt(c, "🌐", fsMicro, false, colMuted)
			})
		}
		ui.Column(c).Grow(1).Gap(2).Children(func() {
			txt(c, displayName(p.Name), fsBody, selected, colText).SingleLine()
			if pl := protoLine(p); pl != "" {
				txt(c, pl, fsMicro, false, colFaint).SingleLine()
			}
		})
		// Delay column: reserved width so the figures line up down
		// the list, and right-aligned so they read as a column.
		ui.Row(c).Width(64).Justify(ui.End).Children(func() {
			if !measured {
				return
			}
			if timeout {
				txt(c, "超时", fsSmall, false, colDanger)
				return
			}
			num(c, fmt.Sprintf("%dms", ms), fsSmall, true, delayColor(ms))
		})
	})
	if main.Clicked() && !selected {
		a.useNode(p.Name)
	}
}

// nodeDetail is the strip under a row opened by its info button:
// provenance and measured facts, plus a per-node delay test.
func (a *App) nodeDetail(c *ui.Context, s subs.Subscription, p core.Proxy, delays map[string]map[string]int, leaf string) {
	ms, timeout, measured := nodeDelay(p, delays)
	delayText := "未测速"
	if measured {
		if timeout {
			delayText = "超时"
		} else {
			delayText = fmt.Sprintf("%d ms", ms)
		}
	}
	udpText := "不支持"
	if p.UDP {
		udpText = "支持"
	}
	ui.Row(c).FillWidth().Padding(2, 16, 10, 16).Gap(9).AlignItems(ui.Center).Children(func() {
		ui.Box(c).Width(48)
		if p.Name == leaf {
			tag(c, "当前出口", colAccent)
		}
		tag(c, s.Name, colMuted)
		tag(c, p.Type, colMuted)
		tag(c, "UDP "+udpText, colMuted)
		delayTone := colMuted
		if measured {
			if timeout {
				delayTone = colDanger
			} else {
				delayTone = delayColor(ms)
			}
		}
		tag(c, delayText, delayTone)
		ui.Box(c).Grow(1)
		if ov.Button(c, "测速此节点", ov.ButtonOutline).Clicked() {
			a.testNodeDelay(p.Name)
		}
	})
}
