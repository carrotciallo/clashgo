package main

import (
	"github.com/carrotciallo/clashgo/internal/config"
	"github.com/carrotciallo/clashgo/internal/subs"
	"github.com/egoist/mygo/ui"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

// Rendering with no store, supervisor or proxy manager proves frames don't
// read master, /proc or launch gsettings. All status is synthetic and cached.
func TestOverviewRendersWithoutIO(t *testing.T) {
	a := &App{reg: &subs.Registry{}, set: config.Settings{MixedPort: 17897, ControllerPort: 19097, Services: []string{}}, overview: overviewFacts{RuleCount: 124, AppMemory: 16 << 20, CoreMemory: 32 << 20}}
	for _, dark := range []bool{false, true} {
		tt := ui.NewTesterWith(func(c *ui.Context) {
			theme := lavenderTheme(c)
			syncPalette(theme)
			c.SetTheme(theme)
			ui.Column(c).FillWidth().Padding(20).Gap(14).Background(colBg).Children(func() { a.pageOverview(c) })
		}, 1120, 900, ui.TesterOptions{Dark: dark})
		if !tt.HasText("0 个订阅 · 124 条规则") {
			t.Fatal("cached overview not rendered")
		}
		if dir := os.Getenv("CLASHGO_TEST_ARTIFACTS"); dir != "" {
			name := "overview-light.png"
			if dark {
				name = "overview-dark.png"
			}
			f, err := os.Create(filepath.Join(dir, name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
}
