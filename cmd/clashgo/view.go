package main

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/carrotciallo/clashgo/internal/subs"
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

var pageNames = []string{"概览", "节点", "配置", "请求", "设置"}

var pageIcons = []*ui.SVG{lucide.Gauge(), lucide.Globe(), lucide.ScrollText(), lucide.Clock(), lucide.Settings()}

// iconButton is a small ghost button carrying only an icon; the label
// remains its accessible name. It keeps a square hit area so a row of
// them lines up with the text beside it.
func iconButton(c *ui.Context, label string, icon *ui.SVG) ui.Element {
	b := ov.Button(c, "", ov.ButtonGhost)
	b.Label(label)
	b.Padding(6).Radius(9)
	b.Children(func() {
		ui.Row(c).TextColor(colMuted).Children(func() {
			ov.Icon(c, icon).Size(15, 15)
		})
	})
	if b.Hovered() {
		b.Background(colHover)
	}
	return b
}

func (a *App) view(c *ui.Context) {
	t := lavenderTheme(c)
	c.SetTheme(t)
	syncPalette(t)
	c.AnimationFrame() // known debt: continuous redraw while open; on-change redraw is future work
	msg, busy, _, _, _, _, _ := a.snapshot()
	ui.Row(c).Fill().Background(colBg).Children(func() {
		// ---- nav rail ----
		ui.Column(c).Width(212).FillHeight().Background(colRail).Padding(20, 12).Gap(3).Children(func() {
			// Wordmark only — no logo mark beside it.
			ui.Row(c).Padding(6, 10).Children(func() {
				txt(c, "ClashGo", fsPage, true, colText).LetterSpacing(-0.2)
			})
			ui.Box(c).Height(18)
			for i, name := range pageNames {
				a.navItem(c, i, name, pageIcons[i])
			}
			ui.Box(c).Grow(1)
			if busy {
				ui.Row(c).Padding(6, 10).Children(func() {
					txt(c, "处理中…", fsMicro, false, colFaint)
				})
			}
		})
		// Rail separator: the rail sits a shade off the page, and a
		// hairline makes the boundary deliberate instead of a guess.
		ui.Box(c).Width(1).FillHeight().Background(colBorder)
		// ---- content ----
		ui.Column(c).Grow(1).FillHeight().Padding(20, 28).Children(func() {
			title := pageNames[a.page]
			if a.page == 0 && a.showServices {
				title = "服务状态"
			}
			ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Gap(16).Children(func() {
				txt(c, title, fsPage, true, colText).LetterSpacing(-0.3)
				// Feedback rides in the title row, right-aligned: a row of
				// its own under the title came and went with the message
				// and shifted the whole page layout each time.
				if msg != "" {
					txt(c, msg, fsSmall, false, colMuted).Grow(1).TextAlign(ui.End).SingleLine()
				} else {
					ui.Box(c).Grow(1)
				}
				if a.page == 0 && !a.showServices {
					if iconButton(c, "刷新出口", lucide.RefreshCw()).Clicked() {
						a.refreshEgress()
					}
					if iconButton(c, "重启核心", lucide.Power()).Clicked() {
						a.restartCore()
					}
				}
			})
			ui.Box(c).Height(8)
			// The page column is forced to at least the viewport height in
			// DIPs. A percentage minimum does not resolve here (the
			// scroll viewport's content height is not a definite base
			// while the column is being measured), so the last module on
			// a page could never Grow into the leftover space and every
			// short page ended in a dead band above the window edge.
			avail := pageAvailH(c)
			ui.Scroll(c).Grow(1).Fill().Children(func() {
				ui.Column(c).FillWidth().MinHeight(avail).Gap(16).Children(func() {
					switch a.page {
					case 0:
						if a.showServices {
							a.pageServices(c)
						} else {
							a.pageOverview(c)
						}
					case 1:
						a.pageProxies(c)
					case 2:
						a.pageProfiles(c)
					case 3:
						a.pageRequests(c)
					case 4:
						a.pageSettings(c)
					}
				})
			})
		})
	})
	a.dialogs(c)
}

// dialogs hosts the add forms as modal panels (FlClash/Shadowrocket
// keep pages clean: a + entry opens the form, the page itself stays
// a list). Called every frame; each Dialog self-hides when closed.
func (a *App) dialogs(c *ui.Context) {
	ov.Dialog(c, &a.dlgAddSub, ov.DialogOptions{Label: "添加订阅"}, func(p ui.Element) {
		txt(c, "添加订阅", fsValue, true, colText)
		// Source picker: link / file / paste, plus QR actions that
		// decode into the link field.
		ui.Row(c).Gap(2).Background(colInset).Radius(11).Padding(3).AlignSelf(ui.Start).Children(func() {
			sources := []struct {
				label string
				icon  *ui.SVG
			}{
				{"链接", lucide.Link2()},
				{"文件", lucide.FileUp()},
				{"粘贴", lucide.ClipboardPaste()},
				{"二维码", lucide.QrCode()},
			}
			for i, s := range sources {
				i, s := i, s
				active := a.subSource == i
				b := ov.Button(c, "", ov.ButtonGhost)
				b.Label(s.label)
				b.Padding(5, 11).Radius(8)
				if active {
					b.Background(colPanel).Shadow(0, 1, 3, 0, colShadowCol)
				}
				b.Children(func() {
					icol, tcol := colMuted, colText
					if active {
						icol, tcol = colAccent, colAccent
					}
					ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
						ui.Row(c).TextColor(icol).Children(func() { ov.Icon(c, s.icon).Size(14, 14) })
						txt(c, s.label, fsSmall, active, tcol)
					})
				})
				if b.Clicked() {
					a.subSource = i
				}
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			txt(c, "名称", fsSmall, false, colMuted).Width(32)
			ov.TextBox(c, &a.subName, ov.TextBoxOptions{}).Label("订阅名称").Grow(1)
		})
		switch a.subSource {
		case 1:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if iconButton(c, "选择订阅文件", lucide.FolderOpen()).Clicked() {
					a.pickFile("选择订阅文件", func(path string) { a.subFile = path })
				}
				name := a.subFile
				if name == "" {
					name = "未选择文件"
				} else {
					name = filepath.Base(name)
				}
				txt(c, name, fsSmall, false, colFaint).Grow(1).SingleLine()
			})
		case 2:
			ov.TextBox(c, &a.subText, ov.TextBoxOptions{Multiline: true}).Label("订阅内容（proxies 列表或完整配置）").Lines(6, 12).Grow(1)
		case 3:
			ui.Row(c).Gap(8).Children(func() {
				if ov.Button(c, "从图片文件识别", ov.ButtonOutline).Clicked() {
					a.pickFile("选择二维码图片", a.readQR)
				}
				if ov.Button(c, "从剪贴板图片识别", ov.ButtonOutline).Clicked() {
					a.readQR("")
				}
			})
		default:
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				txt(c, "地址", fsSmall, false, colFaint).Width(36)
				ov.TextBox(c, &a.subURL, ov.TextBoxOptions{}).Label("订阅地址").Grow(1)
			})
		}
		txt(c, "名称只能用字母、数字、下划线、连字符。", fsMicro, false, colFaint)
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgAddSub = false
			}
			if ov.Button(c, "添加", ov.ButtonPrimary).Clicked() {
				a.dlgAddSub = false
				a.addSubscription()
			}
		})
	})
	ov.Dialog(c, &a.dlgNewProfile, ov.DialogOptions{Label: "新建配置"}, func(p ui.Element) {
		txt(c, "新建配置", fsValue, true, colText)
		ov.TextBox(c, &a.profileName, ov.TextBoxOptions{}).Label("新配置名，如 备用规则").Grow(1)
		txt(c, "内容复制自当前配置生成一份副本，新建不会自动切换。", fsMicro, false, colFaint)
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgNewProfile = false
			}
			if ov.Button(c, "新建", ov.ButtonPrimary).Clicked() {
				a.dlgNewProfile = false
				a.createProfile()
			}
		})
	})
	ov.Dialog(c, &a.dlgImport, ov.DialogOptions{Label: "导入 Shadowrocket 配置"}, func(p ui.Element) {
		txt(c, "导入 Shadowrocket 配置", fsValue, true, colText)
		if rep := a.impReport; rep != nil {
			txt(c, fmt.Sprintf("已生成配置「%s」：规则 %d 条 · 规则集 %d 个。", a.impNameUsed, rep.Rules, rep.Providers), fsSmall, false, colText)
			if len(rep.Dropped) > 0 {
				txt(c, "没能搬过来的（逐条列明）：", fsSmall, true, colDanger)
				for _, d := range rep.Dropped {
					txt(c, d, fsMicro, false, colFaint)
				}
			}
			if len(rep.Notes) > 0 {
				txt(c, "转换说明：", fsSmall, true, colText)
				for _, n := range rep.Notes {
					txt(c, n, fsMicro, false, colFaint)
				}
			}
			ui.Row(c).Gap(8).Children(func() {
				ui.Box(c).Grow(1)
				if ov.Button(c, "完成", ov.ButtonPrimary).Clicked() {
					a.dlgImport = false
					a.impReport = nil
				}
			})
			return
		}
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			txt(c, "名称", fsSmall, false, colFaint).Width(36)
			ov.TextBox(c, &a.impName, ov.TextBoxOptions{}).Label("新配置名，留空用「Shadowrocket 导入」").Grow(1)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			txt(c, "地址", fsSmall, false, colFaint).Width(36)
			ov.TextBox(c, &a.impURL, ov.TextBoxOptions{}).Label("配置文件的 http(s) 地址，填了就不看粘贴").Grow(1)
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			txt(c, "或把 .conf 内容整份粘贴进来（只贴 [Rule] 段也行）：", fsSmall, false, colFaint).Grow(1)
			if iconButton(c, "从文件读入", lucide.FileUp()).Clicked() {
				a.pickFile("选择 Shadowrocket 配置", func(path string) {
					if b, err := os.ReadFile(path); err == nil {
						a.impText = string(b)
						if strings.TrimSpace(a.impName) == "" {
							a.impName = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
						}
					} else {
						a.setMsg("读取文件失败：%v", err)
					}
				})
			}
		})
		ov.TextBox(c, &a.impText, ov.TextBoxOptions{Multiline: true}).Label("Shadowrocket 配置内容").Lines(6, 12).Grow(1)
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgImport = false
			}
			if ov.Button(c, "导入", ov.ButtonPrimary).Clicked() {
				a.importConfig()
			}
		})
	})
	ov.Dialog(c, &a.dlgRestore, ov.DialogOptions{Label: "恢复默认配置"}, func(p ui.Element) {
		txt(c, "恢复默认配置", fsValue, true, colText)
		txt(c, "用内置模板生成「默认配置」并切换过去，其他配置保留不动。", fsSmall, false, colFaint)
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgRestore = false
			}
			if ov.Button(c, "恢复并切换", ov.ButtonPrimary).Clicked() {
				a.dlgRestore = false
				a.restoreDefaultProfile()
			}
		})
	})
	ov.Dialog(c, &a.dlgTestRule, ov.DialogOptions{Label: "测试规则"}, func(p ui.Element) {
		txt(c, "测试规则", fsValue, true, colText)
		ov.TextBox(c, &a.testRuleIn, ov.TextBoxOptions{}).Label("域名或网址，如 www.google.com").Grow(1)
		if a.testRuleOut != "" {
			txt(c, a.testRuleOut, fsSmall, false, colText)
		}
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "关闭", ov.ButtonGhost).Clicked() {
				a.dlgTestRule = false
			}
			if ov.Button(c, "测试", ov.ButtonPrimary).Clicked() {
				a.runTestRule()
			}
		})
	})
	ov.Dialog(c, &a.dlgReplace, ov.DialogOptions{Label: "替换策略"}, func(p ui.Element) {
		txt(c, "替换策略", fsValue, true, colText)
		ov.TextBox(c, &a.repFrom, ov.TextBoxOptions{}).Label("原策略，如 PROXY").Grow(1)
		ov.TextBox(c, &a.repTo, ov.TextBoxOptions{}).Label("新策略，如 节点选择").Grow(1)
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgReplace = false
			}
			if ov.Button(c, "替换", ov.ButtonPrimary).Clicked() {
				a.dlgReplace = false
				a.replacePolicy()
			}
		})
	})
	ov.Dialog(c, &a.dlgAddRule, ov.DialogOptions{Label: "添加规则"}, func(p ui.Element) {
		txt(c, "添加规则", fsValue, true, colText)
		targets := a.ruleTargets()
		if a.ruleTargetIdx >= len(targets) {
			a.ruleTargetIdx = 0
		}
		ui.Row(c).Gap(8).Children(func() {
			if ov.Button(c, "类型："+ruleTypes[a.ruleTypeIdx], ov.ButtonOutline).Clicked() {
				a.ruleTypeIdx = (a.ruleTypeIdx + 1) % len(ruleTypes)
			}
			if ov.Button(c, "去向："+targets[a.ruleTargetIdx], ov.ButtonOutline).Clicked() {
				a.ruleTargetIdx = (a.ruleTargetIdx + 1) % len(targets)
			}
		})
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			txt(c, "值", fsSmall, false, colFaint).Width(36)
			ov.TextBox(c, &a.ruleValue, ov.TextBoxOptions{}).Label("规则值，如 example.com").Grow(1)
		})
		ui.Row(c).Gap(8).Children(func() {
			ui.Box(c).Grow(1)
			if ov.Button(c, "取消", ov.ButtonGhost).Clicked() {
				a.dlgAddRule = false
			}
			if ov.Button(c, "添加规则", ov.ButtonPrimary).Clicked() {
				a.dlgAddRule = false
				a.addRule()
			}
		})
	})
}

// navItem is one rail entry: icon + label. The active page is marked
// by an accent wash and an accent-tinted icon and label — state read
// as tint, not as a solid slab that outweighs the page itself.
func (a *App) navItem(c *ui.Context, idx int, label string, icon *ui.SVG) {
	active := a.page == idx
	fg, iconCol := colText, colMuted
	b := ov.Button(c, "", ov.ButtonGhost)
	b.Label(label)
	b.Padding(8, 10).Radius(10)
	switch {
	case active:
		b.Background(colAccent.Alpha(0.10))
		fg, iconCol = colAccent, colAccent
	case b.Hovered():
		b.Background(colHover)
	}
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).TextColor(iconCol).Children(func() {
			ov.Icon(c, icon).Size(17, 17).Shrink(0)
			txt(c, label, fsBody, active, fg)
		})
	})
	if b.Clicked() {
		a.page = idx
		a.showServices = false
		a.svcManage = false
		a.setCorePage = false
	}
}

// firstQuotaSub is the first subscription that reported a traffic
// quota — the one the dashboard's config card summarises.
func firstQuotaSub(list []subs.Subscription) *subs.Subscription {
	for i := range list {
		if list[i].TotalBytes > 0 {
			return &list[i]
		}
	}
	return nil
}

// pageOverview is the home page as a FlClash-style dashboard:
// a dense status band with the core start/stop button, one strip
// for mode + capture switches, a 2×2 grid of info cards, and the
// tabbed connections card. Everything is on one screen; nothing
// here is a settings form.
func (a *App) pageOverview(c *ui.Context) {
	_, _, coreOK, coreErr, egress, up, down := a.snapshot()
	upRate, downRate := a.trafficRates()
	a.mu.Lock()
	mode := a.mode
	coreSince := a.coreSince
	lan := a.localIP
	conns := a.conns
	recent := a.recent
	svcResults := maps.Clone(a.svcResults)
	upPeak, downPeak := a.maxUpRate, a.maxDownRate
	dnsTotal := a.dnsTotal
	egressOrg := a.egressOrg
	facts := a.overview
	a.mu.Unlock()
	chain := a.egressChain()
	appMem, coreMem := facts.AppMemory, facts.CoreMemory

	// ---- status row: three separate cards — status / upload /
	// download — stretched to one height. ----
	dashboardGrid(c).Children(func() {
		statCard(c, 2, func() {
			ui.Row(c).Gap(14).AlignItems(ui.Center).FillHeight().Children(func() {
				ui.Column(c).Grow(1).Gap(4).FillHeight().Children(func() {
					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						caption(c, "状态").Shrink(0)
						dotCol, state := colDanger, "已停止"
						if coreOK {
							dotCol, state = colOK, "运行中"
						}
						statusPill(c, state, dotCol)
						ui.Box(c).Grow(1)
						txt(c, fmt.Sprintf("mihomo · 127.0.0.1:%d", a.settings().MixedPort), fsMicro, false, colFaint).SingleLine()
					})
					ui.Box(c).Grow(1)
					// The headline names the node traffic is actually leaving
					// through. When the core reports no exit yet, the tail of
					// the resolved chain stands in for it, so the card never
					// shows both a dash and a node name at once.
					head := displayName(egress)
					if head == "" && len(chain) > 1 {
						head = displayName(chain[len(chain)-1])
					}
					if head == "" {
						txt(c, "—", fsMetric, true, colFaint).SingleLine()
					} else {
						txt(c, head, fsDisplay, true, colText).LetterSpacing(-0.6).SingleLine()
					}
					if len(chain) > 1 {
						hops := make([]string, 0, len(chain)-1)
						for _, h := range chain[1:] {
							hops = append(hops, displayName(h))
						}
						if path := strings.Join(hops, " → "); path != head {
							txt(c, path, fsSmall, false, colMuted).SingleLine()
						}
					}
					ui.Box(c).Grow(1)
					ui.Row(c).FillWidth().Height(1).Background(colBorder)
					ui.Box(c).Height(2)
					ui.Row(c).Gap(26).Children(func() {
						uptime := "—"
						if coreOK && !coreSince.IsZero() {
							uptime = uptimeText(coreSince)
						}
						statBlock(c, "运行时长", uptime, 0)
						statBlock(c, "组织", orDash(egressOrg), 1)
					})
				})
				// The big round core button: a brand gradient disc that
				// lifts off the card.
				fabIcon := lucide.Play()
				if coreOK {
					fabIcon = lucide.Pause()
				}
				fab := ui.Column(c).Width(58).Height(58).Radius(29).
					LinearGradient(ui.LinearGradient{From: colAccent, To: colBrand, Angle: 135}).
					Shadow(0, 6, 16, 0, colAccent.Alpha(0.35)).
					AlignItems(ui.Center).Padding(17).TextColor(ui.Hex("#FFFFFF"))
				fab.Children(func() { ov.Icon(c, fabIcon).Size(24, 24) })
				fab.Label(map[bool]string{true: "暂停核心", false: "启动核心"}[coreOK])
				if fab.Clicked() {
					a.toggleCore()
				}
			})
		})
		trafficCard := func(title string, icon *ui.SVG, rate, total, peak int64) {
			statCard(c, 1, func() {
				ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
					ui.Row(c).Width(22).Height(22).Radius(7).Background(colWash).
						AlignItems(ui.Center).Padding(4).TextColor(colAccent).Children(func() {
						ov.Icon(c, icon).Size(14, 14)
					})
					txt(c, title, fsSmall, true, colMuted).Grow(1)
				})
				ui.Box(c).Grow(1)
				ui.Row(c).Gap(3).AlignItems(ui.End).Justify(ui.Center).Children(func() {
					num(c, humanBytes(rate), fsDisplay, true, colText)
					txt(c, "/s", fsSmall, false, colFaint)
				})
				ui.Box(c).Grow(1)
				ui.Row(c).AlignItems(ui.Center).Children(func() {
					caption(c, "累计 "+humanBytes(total)).Grow(1)
					caption(c, "峰值 "+humanBytes(peak)+"/s")
				})
			})
		}
		trafficCard("上传", lucide.ArrowUp(), upRate, up, upPeak)
		trafficCard("下载", lucide.ArrowDown(), downRate, down, downPeak)
	})

	// ---- mode + capture strip ----
	card(c, func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			txt(c, "出站模式", fsSmall, true, colMuted)
			modeIdx := 0
			for i, m := range []string{"rule", "global", "direct"} {
				if mode == m || (mode == "" && m == "rule") {
					modeIdx = i
				}
			}
			segmented(c, []string{modeLabel("rule"), modeLabel("global"), modeLabel("direct")}, modeIdx, func(i int) {
				a.setMode([]string{"rule", "global", "direct"}[i])
			})
			ui.Box(c).Grow(1)
			// Capture is one either/or choice (the user's ruling:
			// system proxy XOR TUN, never both), so it renders as
			// a segmented switch like the mode picker — not two
			// independent toggles.
			txt(c, "接管方式", fsSmall, true, colMuted)
			capIdx := 0
			if a.settings().TUNEnabled {
				capIdx = 1
			}
			segmented(c, []string{"系统代理", "虚拟网卡"}, capIdx, func(i int) {
				if want := i == 1; want != a.settings().TUNEnabled {
					a.setCapture(want)
				}
			})
		})
	})

	// ---- info tiles share the status row's four tracks and height. ----
	dashboardGrid(c).Children(func() {
		miniCard(c, lucide.Network(), "内网 IP", func() {
			if lan == "" {
				txt(c, "—", fsMetric, true, colFaint).SingleLine()
			} else {
				// One step below the other metrics: an IPv4 address is
				// twelve-plus glyphs and has to survive a narrow tile.
				num(c, lan, fsMetric, true, colText).LetterSpacing(-0.3).SingleLine()
			}
		}, func() {
			caption(c, fmt.Sprintf("混合 %d · 控制 %d", a.settings().MixedPort, a.settings().ControllerPort))
			sub, tone := "未接管", colFaint
			if a.settings().TUNEnabled {
				sub, tone = "虚拟网卡接管中", colOK
			} else if facts.CaptureOwned {
				sub, tone = fmt.Sprintf("系统代理 127.0.0.1:%d", a.settings().MixedPort), colOK
			}
			txt(c, sub, fsMicro, false, tone).SingleLine()
		})
		miniCard(c, lucide.ScrollText(), "配置", func() {
			txt(c, profileDisplayName(a.settings().ActiveProfile), fsValue, true, colText).SingleLine()
			rules := fmt.Sprintf("%d 条规则", facts.RuleCount)
			caption(c, fmt.Sprintf("%d 个订阅 · %s", len(a.reg.Snapshot()), rules))
		}, func() {
			if u := firstQuotaSub(a.reg.Snapshot()); u != nil {
				pct := int(u.UsedBytes() * 100 / u.TotalBytes)
				if pct > 100 {
					pct = 100
				}
				ui.Row(c).AlignItems(ui.Center).Gap(6).Children(func() {
					txt(c, u.Name, fsMicro, false, colMuted).Grow(1).SingleLine()
					if u.ExpireUnix > 0 {
						days := int(time.Until(time.Unix(u.ExpireUnix, 0)).Hours() / 24)
						caption(c, fmt.Sprintf("剩余 %d 天", days))
					}
				})
				caption(c, fmt.Sprintf("%d%% · %s / %s", pct, humanBytes(u.UsedBytes()), humanBytes(u.TotalBytes)))
				barTrack(c, 5, func() {
					if pct > 0 {
						ui.Box(c).Grow(float32(pct)).Height(5).Radius(999).Background(colAccent)
					}
					if pct < 100 {
						ui.Box(c).Grow(float32(100 - pct))
					}
				})
			}
		})
		// Service status gets a custom card (not miniCard): a header
		// re-check icon and a footer row opening the full page.
		statCard(c, 1, func() {
			ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
				ui.Row(c).Width(22).Height(22).Radius(7).Background(colWash).
					AlignItems(ui.Center).Padding(4).TextColor(colAccent).Children(func() {
					ov.Icon(c, lucide.ShieldCheck()).Size(14, 14)
				})
				txt(c, "服务状态", fsSmall, true, colMuted).Grow(1)
				if iconButton(c, "重新检测", lucide.Zap()).Clicked() {
					a.operate(a.checkServices)
				}
			})
			ui.Box(c).Grow(1)
			defs := a.enabledServices()
			for i, d := range defs {
				if i >= 3 {
					break
				}
				val, col := svcStatusText(svcResults[d.Name])
				statusRow(c, d.Name, val, col)
			}
			ui.Box(c).Grow(1)
			b := ov.Button(c, "", ov.ButtonGhost)
			b.Label("全部服务")
			b.Padding(5, 0).Radius(8)
			if b.Hovered() {
				b.Background(colHover)
			}
			b.Children(func() {
				ui.Row(c).FillWidth().AlignItems(ui.Center).TextColor(colMuted).Children(func() {
					txt(c, fmt.Sprintf("全部 %d 个服务", len(defs)), fsMicro, false, colMuted).Grow(1)
					ov.Icon(c, lucide.ChevronRight()).Size(13, 13)
				})
			})
			if b.Clicked() {
				a.showServices = true
			}
		})
		miniCard(c, lucide.MemoryStick(), "内存信息", func() {
			num(c, humanBytes(appMem+coreMem), fsMetric, true, colText).LetterSpacing(-0.3)
		}, func() {
			caption(c, fmt.Sprintf("应用 %s · 内核 %s", humanBytes(appMem), humanBytes(coreMem)))
			if total := appMem + coreMem; total > 0 {
				barTrack(c, 5, func() {
					if appMem > 0 {
						ui.Box(c).Grow(float32(appMem)).Height(5).Radius(999).Background(colAccent)
					}
					if coreMem > 0 {
						ui.Box(c).Grow(float32(coreMem)).Height(5).Radius(999).Background(colBrand.Alpha(0.45))
					}
				})
			}
		})
	})

	// ---- connections card: three modules inside ONE card —
	// tabs on top, the active module's table below (live /
	// recent / DNS query log). It takes the leftover height so
	// the dashboard ends at the window edge.
	cardGrow(c, 360, func() {
		segmented(c, []string{
			fmt.Sprintf("实时连接 %d", len(conns)),
			fmt.Sprintf("最近请求 %d", len(recent)),
			fmt.Sprintf("DNS 查询 %d", dnsTotal),
		}, a.ovTab, func(i int) { a.ovTab = i })
		ui.Box(c).Height(2)
		switch a.ovTab {
		case 1:
			a.recentTable(c, recent, 9)
		case 2:
			a.dnsLogView(c)
		default:
			a.liveTable(c, conns, 9, false)
		}
	})

	if coreErr != "" {
		card(c, func() {
			txt(c, "核心日志尾部", fsSmall, true, colDanger)
			txt(c, coreErr, fsMicro, false, colMuted)
		})
	}
}

// actionRow is one full-width list action: icon + label under a
// ghost-button skin. danger paints icon and label in danger color.
func actionRow(c *ui.Context, label string, icon *ui.SVG, danger bool, fn func()) {
	ui.Row(c).FillWidth().Children(func() {
		b := ov.Button(c, "", ov.ButtonGhost)
		b.Label(label)
		b.Grow(1).Padding(0).Radius(0)
		b.Children(func() {
			row := ui.Row(c).FillWidth().Padding(12, 16).Gap(11).AlignItems(ui.Center)
			if b.Hovered() {
				row.Background(colHover)
			}
			row.Children(func() {
				iconCol, labelCol := colMuted, colText
				if danger {
					iconCol, labelCol = colDanger, colDanger
				}
				ui.Row(c).TextColor(iconCol).Children(func() { ov.Icon(c, icon).Size(16, 16) })
				txt(c, label, fsBody, false, labelCol)
			})
		})
		if b.Clicked() {
			fn()
		}
	})
}

// miniCard is one of the dashboard's four compact info tiles.
func miniCard(c *ui.Context, icon *ui.SVG, title string, body, footer func()) {
	statCard(c, 1, func() {
		ui.Row(c).AlignItems(ui.Center).Gap(7).Children(func() {
			ui.Row(c).Width(22).Height(22).Radius(7).Background(colWash).
				AlignItems(ui.Center).Padding(4).TextColor(colAccent).Children(func() {
				ov.Icon(c, icon).Size(14, 14)
			})
			txt(c, title, fsSmall, true, colMuted).Grow(1)
		})
		ui.Column(c).FillWidth().Grow(1).Justify(ui.Center).AlignItems(ui.Center).Gap(6).Children(body)
		ui.Column(c).FillWidth().Height(48).Justify(ui.End).Gap(4).Children(footer)
	})
}

// statusRow is a dotted status line in the service-status card.
func statusRow(c *ui.Context, label, val string, dot ui.Color) {
	ui.Row(c).Gap(7).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(5).Height(5).Radius(2.5).Background(dot)
		txt(c, label, fsSmall, false, colText).Grow(1).SingleLine()
		txt(c, val, fsSmall, true, dot)
	})
}

func uptimeText(since time.Time) string {
	d := time.Since(since)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func egressOr(s string) string {
	if s == "" {
		return "—"
	}
	return s
}
