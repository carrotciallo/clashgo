package main

import (
	"github.com/egoist/mygo/ui"
	ov "github.com/oneo-me/onevra/source/components"
	ovtheme "github.com/oneo-me/onevra/source/theme"
)

// Lavender-blue identity (user-set at project start): #8B9CF9 is the
// brand color (fills, washes, tray disc); a deepened sibling serves as
// the interactive accent so white labels stay legible on buttons and
// switches. The whole UI derives from the theme installed per frame in
// view(), so Onevra components pick the colors up automatically — no
// per-widget hardcoding.
var (
	colBrand  = ui.Hex("#8B9CF9")
	colAccent = ui.Hex("#5B67D8")
	// colAmber marks the default profile (Shadowrocket's orange dot).
	colAmber = ui.Hex("#F0A23C")
)

// Palette slots refreshed every frame from the installed theme.
var (
	colBg        ui.Color
	colPanel     ui.Color
	colRail      ui.Color // nav rail: window color washed toward the brand
	colInset     ui.Color // recessed track/band (segmented controls, table head)
	colHover     ui.Color // neutral hover wash for text-like controls
	colText      ui.Color
	colMuted     ui.Color
	colFaint     ui.Color // third-level text: units, captions, empty states
	colBorder    ui.Color
	colDanger    ui.Color
	colWarn      ui.Color
	colOK        ui.Color
	colWash      ui.Color // accent at low alpha, for selected washes/pills
	colZebra     ui.Color // even-row tint inside tables
	colShadowCol ui.Color // ink used for card/segment shadows
)

// Type scale. One ladder, used everywhere, so the pages read as one
// document instead of a pile of ad-hoc sizes. Tuned for a wide desktop
// window: the old 11–13px body read as a shrunken web page there.
const (
	fsDisplay = 30 // the one big number in a card
	fsMetric  = 24 // secondary hero figure
	fsPage    = 22 // page title
	fsValue   = 16 // row title, card value
	fsBody    = 14 // default body text
	fsSmall   = 13 // secondary text, table cells
	fsMicro   = 12 // captions, table head, units
)

// lavenderTheme builds the frame theme: Onevra's palette (which keeps
// its spacing/radius system and preference adjustments) with the
// surfaces cooled toward lavender-white and the accent replaced.
func lavenderTheme(c *ui.Context) *ui.Theme {
	dark := c.Theme().Dark
	t := ovtheme.Palette(dark)
	black, white := ui.RGB(0, 0, 0), ui.RGB(255, 255, 255)
	if dark {
		t.Background = ui.Hex("#16161D")
		t.Surface = ui.Hex("#20202A")
		t.SurfaceHover = ui.Hex("#2B2B38")
		t.SurfacePressed = ui.Hex("#353545")
		t.Border = ui.Hex("#2F2F3C")
		t.Text = ui.Hex("#EDEDF6")
		t.TextMuted = ui.Hex("#9B9BB3")
		t.Accent = colBrand
		t.AccentHover = colBrand.Mix(white, 0.10)
		t.AccentPressed = colBrand.Mix(black, 0.10)
		t.AccentText = ui.Hex("#1A1730")
	} else {
		t.Background = ui.Hex("#F5F5FA")
		t.Surface = ui.Hex("#FFFFFF")
		t.SurfaceHover = ui.Hex("#EDEEF8")
		t.SurfacePressed = ui.Hex("#DFE1F4")
		t.Border = ui.Hex("#E5E6F1")
		t.Text = ui.Hex("#23253A")
		t.TextMuted = ui.Hex("#6E7190")
		t.Accent = colAccent
		t.AccentHover = colAccent.Mix(black, 0.08)
		t.AccentPressed = colAccent.Mix(black, 0.18)
		t.AccentText = white
	}
	t.Selection = t.Accent.Alpha(0.20)
	t.Focus = t.Accent.Alpha(0.6)
	t.Radius = 10
	// Same preference adjustments Onevra's own Apply performs.
	p := c.Preferences()
	if p.HighContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
		t.Focus.A = 255
		t.Scrollbar = t.Scrollbar.Alpha(1.6)
	}
	if s := p.TextScale; s > 0 && s != 1 {
		t.FontSize *= s
	}
	return t
}

func syncPalette(t *ui.Theme) {
	colBg, colPanel, colText, colMuted, colBorder = t.Background, t.Surface, t.Text, t.TextMuted, t.Border
	colDanger, colWarn, colOK = t.Danger, t.Warning, t.Success
	colRail = t.Background.Mix(t.Accent, 0.035)
	colInset = t.Text.Alpha(0.05)
	colHover = t.Text.Alpha(0.045)
	colWash = t.Accent.Alpha(0.10)
	colFaint = t.TextMuted.Mix(t.Background, 0.30)
	colZebra = t.Text.Alpha(0.022)
	colShadowCol = ui.RGBA(0, 0, 0, 0.10)
	if t.Dark {
		colShadowCol = ui.RGBA(0, 0, 0, 0.45)
	}
}

// ---------- surfaces ----------

// card is the shared content surface: panel face, hairline border,
// generous radius and padding.
func card(c *ui.Context, children func()) {
	ui.Column(c).FillWidth().Background(colPanel).Border(1, colBorder).
		Radius(16).Padding(16).Gap(12).Children(children)
}

// cardGrow is card() for the last module on a page: it claims any
// leftover viewport height (Grow) and never shrinks below minH, so a
// dashboard with three table rows still closes on a full-height
// surface instead of a thin strip above a dead band.
func cardGrow(c *ui.Context, minH float32, children func()) {
	e := ui.Column(c).FillWidth().Grow(1).Background(colPanel).Border(1, colBorder).
		Radius(16).Padding(16).Gap(12)
	if minH > 0 {
		e = e.MinHeight(minH)
	}
	e.Children(children)
}

// listCard is the grouped-list surface: one rounded card per group
// whose rows are separated by hairlines, instead of one card per
// row. Rows bring their own padding; the card brings none.
func listCard(c *ui.Context, children func()) {
	ui.Column(c).FillWidth().Background(colPanel).Border(1, colBorder).
		Radius(16).Children(children)
}

// statCard is a dashboard tile: bigger radius and padding than a list,
// because it holds a single number rather than a column of rows.
const dashboardTileHeight = 184

// Both dashboard rows use the same fractional tracks, never intrinsic flex
// widths. Spanning two columns includes exactly one shared gutter.
func dashboardGrid(c *ui.Context) ui.Element {
	return ui.Grid(c).FillWidth().Columns(4).RowTracks(ui.Fixed(dashboardTileHeight)).Gap(14)
}

func statCard(c *ui.Context, span float32, children func()) ui.Element {
	return ui.Column(c).ColumnSpan(int(span)).MinWidth(0).Height(dashboardTileHeight).
		Background(colPanel).Border(1, colBorder).
		Radius(18).Padding(16, 17).Gap(8).Children(children)
}

// hairline is the inset divider between list rows.
func hairline(c *ui.Context) {
	ui.Row(c).FillWidth().Children(func() {
		ui.Box(c).Width(16).Height(1)
		ui.Box(c).Grow(1).Height(1).Background(colBorder)
		ui.Box(c).Width(16).Height(1)
	})
}

// barTrack is a recessed rounded track; callers fill it with
// ui.Box(...).Grow(part).Background(color) children.
func barTrack(c *ui.Context, height float32, build func()) {
	ui.Row(c).FillWidth().Height(height).Radius(height / 2).Background(colInset).Children(build)
}

// ---------- type ----------

func txt(c *ui.Context, s string, size float32, bold bool, col ui.Color) ui.Element {
	e := ov.Text(c, s).FontSize(size).TextColor(col)
	if bold {
		e = e.Bold()
	}
	return e
}

// num is a figure that must not jitter as it counts: tabular digits.
func num(c *ui.Context, s string, size float32, bold bool, col ui.Color) ui.Element {
	return txt(c, s, size, bold, col).FontFeatures("tnum")
}

// caption is the muted label that sits above a value. Always one line:
// a caption that wraps turns a two-line card into a four-line one at
// narrow widths, and every caption in the app is a short label.
func caption(c *ui.Context, s string) ui.Element {
	return txt(c, s, fsMicro, false, colFaint).SingleLine()
}

func sectionTitle(c *ui.Context, s string) {
	txt(c, s, fsBody, true, colMuted)
}

// sectionHeader is the grouped-list caption that sits above a list
// card (Shadowrocket's section rows): the title on the left, a muted
// hint — usually a count — on the right.
func sectionHeader(c *ui.Context, title, hint string) {
	ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(8).Children(func() {
		txt(c, title, fsSmall, true, colMuted).Grow(1)
		if hint != "" {
			txt(c, hint, fsMicro, false, colFaint)
		}
	})
}

// statBlock is a labelled figure (运行时长 / 组织): caption over a
// value, never a label beside one. Pass grow to let a long value
// take the remaining width and clip instead of pushing the row wide.
func statBlock(c *ui.Context, label, value string, grow float32) {
	col := ui.Column(c).Gap(3)
	if grow > 0 {
		col = col.Grow(grow)
	}
	col.Children(func() {
		caption(c, label)
		txt(c, value, fsBody, true, colText).SingleLine()
	})
}

// statusPill is a live-state chip: a tinted wash carrying a dot and
// the state word, so state is a badge rather than a sentence.
func statusPill(c *ui.Context, label string, tone ui.Color) {
	// Shrink(0): the pill is the state itself — it must never be the
	// thing a squeezed header row gives up, or "运行中" folds to two
	// lines and the card header jumps.
	ui.Row(c).Background(tone.Alpha(0.12)).Radius(999).Padding(3, 9).Gap(5).
		Shrink(0).AlignItems(ui.Center).Children(func() {
		ui.Row(c).Width(6).Height(6).Radius(3).Background(tone)
		txt(c, label, fsMicro, true, tone.Mix(colText, 0.25)).SingleLine()
	})
}

// tag is the small neutral chip used for kinds (整包配置, 规则类型).
func tag(c *ui.Context, label string, col ui.Color) {
	ui.Row(c).Background(col.Alpha(0.12)).Radius(6).Padding(2, 7).Children(func() {
		txt(c, label, fsMicro, true, col)
	})
}

// ---------- controls ----------

// segmented is the recessed segmented control: a muted track holding
// one raised, accent-labelled segment. onPick fires only when the
// choice actually changes, so callers can wire it straight to a setter.
func segmented(c *ui.Context, labels []string, selected int, onPick func(int)) {
	ui.Row(c).Gap(2).Background(colInset).Radius(11).Padding(3).AlignSelf(ui.Start).Children(func() {
		for i, label := range labels {
			i, label := i, label
			active := i == selected
			b := ov.Button(c, "", ov.ButtonGhost)
			b.Label(label)
			b.Padding(5, 12).Radius(8)
			if active {
				b.Background(colPanel).Shadow(0, 1, 3, 0, colShadowCol)
				b.Children(func() { txt(c, label, fsSmall, true, colAccent) })
			} else {
				b.Children(func() { txt(c, label, fsSmall, false, colMuted) })
			}
			if b.Clicked() && !active && onPick != nil {
				onPick(i)
			}
		}
	})
}

// delayColor grades a measured delay: green < 200 ms, amber < 600,
// danger above; untested/timeout handled by the caller.
func delayColor(ms int) ui.Color {
	switch {
	case ms < 200:
		return colOK
	case ms < 600:
		return colWarn
	default:
		return colDanger
	}
}

// pageAvailH is the height a page column may claim: the window's content
// height minus the chrome view() puts above the scroll viewport (padding,
// title row, and the gap under it). Feedback text lives inside the title
// row, so this never changes with application state.
func pageAvailH(c *ui.Context) float32 {
	_, h := c.Size()
	above := float32(20 + 40 + 8 + 20) // padding top/bottom, title row, gap
	if avail := h - above; avail > 0 {
		return avail
	}
	return 0
}
