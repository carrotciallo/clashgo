package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	"github.com/oneo-me/onevra/source/icons/lucide"
)

// The config page in Shadowrocket's own shape: an actions card
// （恢复默认配置 / 导入 / 模块 / 测试规则）, then the 本地文件
// list — orange dot for the default, check for the one in use,
// one tap switches, ⓘ opens the file's detail page with its
// section counts and file operations.

// navRow is one grouped-list navigation row: accent icon, label,
// optional right-hand value, chevron; the whole row clicks.
func navRow(c *ui.Context, icon *ui.SVG, label, value string, danger bool, onClick func()) {
	ui.Row(c).FillWidth().Children(func() {
		b := ov.Button(c, "", ov.ButtonGhost)
		b.Label(label)
		b.Grow(1).Padding(0).Radius(0)
		b.Children(func() {
			row := ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Padding(12, 16)
			if b.Hovered() {
				row.Background(colHover)
			}
			row.Children(func() {
				lc := colText
				if danger {
					lc = colDanger
				}
				if icon != nil {
					ic := colMuted
					if danger {
						ic = colDanger
					}
					ui.Row(c).TextColor(ic).Children(func() { ov.Icon(c, icon).Size(17, 17) })
				} else {
					ui.Box(c).Width(17)
				}
				txt(c, label, fsBody, false, lc).Grow(1).SingleLine()
				if value != "" {
					num(c, value, fsSmall, false, colFaint)
				}
				ui.Row(c).TextColor(colFaint).Children(func() { ov.Icon(c, lucide.ChevronRight()).Size(15, 15) })
			})
		})
		if b.Clicked() && onClick != nil {
			onClick()
		}
	})
}

// plainRow is a non-interactive label/value row inside a listCard.
func plainRow(c *ui.Context, label, value string) {
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Padding(12, 16).Children(func() {
		txt(c, label, fsBody, false, colText).Grow(1).SingleLine()
		txt(c, value, fsSmall, false, colMuted).SingleLine()
	})
}

func backRow(c *ui.Context, title string, onBack func()) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		if iconButton(c, "返回", lucide.ChevronLeft()).Clicked() {
			onBack()
		}
		txt(c, title, fsValue, true, colText).SingleLine()
	})
}

func (a *App) pageProfiles(c *ui.Context) {
	if a.cfgProviders {
		a.pageRuleProviders(c)
		return
	}
	if a.cfgOpen && a.cfgSection != "" {
		a.pageProfileSection(c)
		return
	}
	if a.cfgOpen {
		a.pageProfileDetail(c)
		return
	}

	// ---- actions card ----
	listCard(c, func() {
		navRow(c, lucide.RotateCcw(), "恢复默认配置", "", false, func() { a.dlgRestore = true })
		hairline(c)
		navRow(c, lucide.Download(), "导入 Shadowrocket 配置", "", false, func() {
			a.impReport = nil
			a.dlgImport = true
		})
		hairline(c)
		navRow(c, lucide.Blocks(), "模块", "", false, func() { a.cfgProviders = true })
		hairline(c)
		navRow(c, lucide.FlaskConical(), "测试规则", "", false, func() {
			a.testRuleOut = ""
			a.dlgTestRule = true
		})
	})
	ui.Box(c).Height(14)

	// ---- local files ----
	profiles := a.store.Profiles()
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		txt(c, "本地文件", fsSmall, true, colMuted)
		txt(c, fmt.Sprintf("%d 个", len(profiles)), fsMicro, false, colFaint).Grow(1)
		if iconButton(c, "新建配置", lucide.CirclePlus()).Clicked() {
			a.dlgNewProfile = true
		}
	})
	listCard(c, func() {
		for i, p := range profiles {
			p := p
			if i > 0 {
				hairline(c)
			}
			active := p.Name == a.set.ActiveProfile
			isDefault := p.Name == a.set.DefaultProfile
			ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
				// The switch area is a plain row with its own
				// Clicked state, not a Button wrapping a
				// FillWidth row — that hit rect ran under the
				// info button and swallowed its clicks.
				main := ui.Row(c).Grow(1).Gap(11).AlignItems(ui.Center).Padding(11, 16)
				main.Label("切换到 " + profileDisplayName(p.Name))
				main.Children(func() {
					if isDefault {
						ui.Row(c).Width(7).Height(7).Radius(3.5).Background(colAmber)
					} else {
						ui.Box(c).Width(10)
					}
					ui.Column(c).Grow(1).Gap(2).Children(func() {
						txt(c, profileDisplayName(p.Name), fsValue, active, colText).SingleLine()
						num(c, p.ModTime.Format("2006-01-02 15:04")+" · "+humanBytes(p.Size), fsMicro, false, colFaint)
					})
					if active {
						ui.Row(c).TextColor(colAccent).Children(func() { ov.Icon(c, lucide.Check()).Size(17, 17) })
					}
				})
				if main.Clicked() && !active {
					a.switchProfile(p.Name)
				}
				if iconButton(c, "配置详情 "+profileDisplayName(p.Name), lucide.Info()).Clicked() {
					a.cfgDetail = p.Name
					a.cfgOpen = true
					a.cfgSection = ""
				}
			})
		}
	})
	txt(c, "圆点代表默认配置，复选标记代表正在使用的配置。", fsMicro, false, colFaint)
}

// pageProfileDetail is one file's detail page: its sections with
// counts, then the file operations （编辑纯文本 / 导出 / 复制 /
// 替换策略 / 测试规则 / 设默认 / 删除）.
func (a *App) pageProfileDetail(c *ui.Context) {
	name := a.cfgDetail
	backRow(c, profileDisplayName(name), func() { a.cfgDetail = ""; a.cfgOpen = false; a.cfgSection = "" })
	ui.Box(c).Height(8)
	content, err := a.profileContent(name)
	if err != nil {
		card(c, func() { txt(c, "读配置失败："+err.Error(), fsSmall, false, colDanger) })
		return
	}
	rules := config.ExtractRules(content)
	providers := ruleProvidersOf(content)
	hosts := hostsOf(content)
	general := generalOf(content)
	cached := 0
	for _, rp := range providers {
		if _, err := os.Stat(filepath.Join(a.store.RuntimeDir(), "rules", rp.Name+".yaml")); err == nil {
			cached++
		}
	}
	listCard(c, func() {
		navRow(c, nil, "通用", fmt.Sprintf("%d", len(general)), false, func() { a.cfgSection = "general" })
		hairline(c)
		navRow(c, nil, "规则", fmt.Sprintf("%d", len(rules)), false, func() { a.cfgSection = "rules" })
		hairline(c)
		navRow(c, nil, "Hosts", fmt.Sprintf("%d", len(hosts)), false, func() { a.cfgSection = "hosts" })
		hairline(c)
		navRow(c, nil, "规则集 URL", fmt.Sprintf("%d/%d", cached, len(providers)), false, func() { a.cfgProviders = true })
	})
	ui.Box(c).Height(12)
	listCard(c, func() {
		navRow(c, lucide.FileText(), "编辑纯文本", "", false, func() { a.openProfileText(name) })
		hairline(c)
		navRow(c, lucide.Upload(), "导出配置", "", false, func() { a.exportProfile(name) })
		hairline(c)
		navRow(c, lucide.Copy(), "复制", "", false, func() { a.duplicateProfile(name) })
	})
	ui.Box(c).Height(12)
	listCard(c, func() {
		navRow(c, lucide.Replace(), "替换策略", "", false, func() {
			a.repFrom, a.repTo = "", ""
			a.dlgReplace = true
		})
		hairline(c)
		navRow(c, lucide.FlaskConical(), "测试规则", "", false, func() {
			a.testRuleOut = ""
			a.dlgTestRule = true
		})
		if name != a.set.DefaultProfile {
			hairline(c)
			navRow(c, lucide.Check(), "设为默认配置", "", false, func() { a.setDefaultProfile(name) })
		}
		if name != a.set.ActiveProfile {
			hairline(c)
			navRow(c, lucide.Trash(), "删除配置", "", true, func() {
				a.deleteProfile(name)
				if !a.store.ProfileExists(name) {
					a.cfgDetail = ""
					a.cfgOpen = false
				}
			})
		}
	})
}

// pageProfileSection renders one detail section: 通用 / Hosts as
// plain lists, 规则 as the editable rule list (editing applies to
// the active profile; other files show their rules read-only
// until switched to).
func (a *App) pageProfileSection(c *ui.Context) {
	name := a.cfgDetail
	section := a.cfgSection
	titles := map[string]string{"general": "通用", "hosts": "Hosts", "rules": "规则"}
	backRow(c, titles[section], func() { a.cfgSection = "" })
	ui.Box(c).Height(8)
	content, err := a.profileContent(name)
	if err != nil {
		card(c, func() { txt(c, "读配置失败："+err.Error(), fsSmall, false, colDanger) })
		return
	}
	switch section {
	case "general":
		listCard(c, func() {
			for i, kv := range generalOf(content) {
				if i > 0 {
					hairline(c)
				}
				plainRow(c, kv.Key, kv.Val)
			}
		})
	case "hosts":
		hosts := hostsOf(content)
		if len(hosts) == 0 {
			card(c, func() { txt(c, "没有 Hosts 条目", fsSmall, false, colFaint) })
			return
		}
		listCard(c, func() {
			for i, kv := range hosts {
				if i > 0 {
					hairline(c)
				}
				plainRow(c, kv.Key, kv.Val)
			}
		})
	case "rules":
		a.rulesList(c, content, name == a.set.ActiveProfile)
	}
}

// rulesList is the rule list for a profile's content. Editing
// (add/delete) is offered only for the active profile, whose
// writes flow through SaveMaster + apply like always.
func (a *App) rulesList(c *ui.Context, master string, editable bool) {
	rules := config.ExtractRules(master)
	ui.Row(c).FillWidth().AlignItems(ui.Center).Children(func() {
		txt(c, fmt.Sprintf("共 %d 条", len(rules)), fsMicro, false, colFaint).Grow(1)
		if editable {
			if iconButton(c, "添加规则", lucide.CirclePlus()).Clicked() {
				a.dlgAddRule = true
			}
		}
	})
	listCard(c, func() {
		for i, r := range rules {
			i, r := i, r
			if i > 0 {
				hairline(c)
			}
			typ, value, policy := ruleParts(r)
			ui.Row(c).FillWidth().Padding(10, 16).Gap(10).AlignItems(ui.Center).Children(func() {
				num(c, fmt.Sprintf("%d", i+1), fsMicro, false, colFaint).Width(26)
				tag(c, ruleTypeLabel(typ), colMuted)
				if value != "" {
					txt(c, value, fsSmall, false, colText).Grow(1).SingleLine()
				} else {
					ui.Box(c).Grow(1)
				}
				if policy != "" {
					txt(c, policy, fsSmall, true, policyColor(policy))
				}
				if editable {
					if iconButton(c, "删除规则 "+r, lucide.Trash()).Clicked() {
						a.deleteRule(i)
					}
				}
			})
		}
	})
}

// pageRuleProviders lists rule-providers of the profile in
// context (the detail target, or the active profile from the
// 模块 row): name, URL, and whether the set is already cached.
func (a *App) pageRuleProviders(c *ui.Context) {
	name := a.cfgDetail
	title := "模块"
	if a.cfgOpen {
		title = "规则集 URL"
	}
	backRow(c, title, func() { a.cfgProviders = false })
	ui.Box(c).Height(8)
	content, err := a.profileContent(name)
	if err != nil {
		card(c, func() { txt(c, "读配置失败："+err.Error(), fsSmall, false, colDanger) })
		return
	}
	providers := ruleProvidersOf(content)
	if len(providers) == 0 {
		card(c, func() { txt(c, "没有规则集", fsSmall, false, colFaint) })
		return
	}
	listCard(c, func() {
		for i, rp := range providers {
			if i > 0 {
				hairline(c)
			}
			cached := false
			if _, err := os.Stat(filepath.Join(a.store.RuntimeDir(), "rules", rp.Name+".yaml")); err == nil {
				cached = true
			}
			ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Padding(11, 16).Children(func() {
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					txt(c, rp.Name, fsBody, true, colText).SingleLine()
					txt(c, rp.URL, fsMicro, false, colFaint).SingleLine()
				})
				if cached {
					tag(c, "已下载", colOK)
				} else {
					tag(c, "未下载", colAmber)
				}
			})
		}
	})
}

// ruleParts splits a raw rule line into its display parts. Rules are
// TYPE,value,…,policy[,no-resolve]; MATCH carries no value.
func ruleParts(r string) (typ, value, policy string) {
	parts := strings.Split(r, ",")
	if len(parts) == 1 {
		return parts[0], "", ""
	}
	policy = parts[len(parts)-1]
	rest := parts[:len(parts)-1]
	if policy == "no-resolve" && len(rest) > 1 {
		policy = rest[len(rest)-1]
		rest = rest[:len(rest)-1]
	}
	if len(rest) == 1 {
		return rest[0], "", policy
	}
	return rest[0], strings.Join(rest[1:], ","), policy
}

func ruleTypeLabel(t string) string {
	switch t {
	case "DOMAIN":
		return "域名"
	case "DOMAIN-SUFFIX":
		return "域名后缀"
	case "DOMAIN-KEYWORD":
		return "关键词"
	case "DOMAIN-WILDCARD":
		return "通配域名"
	case "IP-CIDR", "IP-CIDR6":
		return "IP 段"
	case "GEOIP":
		return "GEOIP"
	case "GEOSITE":
		return "GEOSITE"
	case "RULE-SET":
		return "规则集"
	case "PROCESS-NAME":
		return "进程"
	case "DST-PORT":
		return "目标端口"
	case "SRC-PORT":
		return "源端口"
	case "MATCH":
		return "最终"
	}
	return t
}

func policyColor(policy string) ui.Color {
	switch policy {
	case "DIRECT":
		return colMuted
	case "REJECT", "REJECT-DROP":
		return colDanger
	}
	return colAccent
}
