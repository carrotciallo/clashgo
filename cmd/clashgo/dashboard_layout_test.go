package main

import (
	"fmt"
	"github.com/egoist/mygo/ui"
	"math"
	"testing"
)

func TestDashboardSharedGrid(t *testing.T) {
	for _, width := range []int{660, 980, 1400} {
		t.Run(fmt.Sprint(width), func(t *testing.T) {
			tt := ui.NewTester(func(c *ui.Context) {
				ui.Column(c).FillWidth().Gap(14).Children(func() {
					dashboardGrid(c).Children(func() {
						statCard(c, 2, func() { ui.Text(c, "a long status value that must not resize the shared columns") }).Label("status")
						statCard(c, 1, func() { ui.Text(c, "1 MB") }).Label("upload")
						statCard(c, 1, func() { ui.Text(c, "10 GB") }).Label("download")
					})
					dashboardGrid(c).Children(func() {
						for i := 0; i < 4; i++ {
							statCard(c, 1, func() {}).Label(fmt.Sprint("lower", i))
						}
					})
				})
			}, width, 420)
			box := func(name string) ui.Rect {
				r, ok := tt.Find(name)
				if !ok {
					t.Fatalf("missing %s", name)
				}
				return r
			}
			near := func(a, b float32) {
				if math.Abs(float64(a-b)) > 1 {
					t.Errorf("grid mismatch %.2f != %.2f", a, b)
				}
			}
			status, up, down := box("status"), box("upload"), box("download")
			l0, l1, l2, l3 := box("lower0"), box("lower1"), box("lower2"), box("lower3")
			near(status.X, l0.X)
			near(status.X+status.W, l1.X+l1.W)
			near(up.X, l2.X)
			near(up.W, l2.W)
			near(down.X, l3.X)
			near(down.X+down.W, l3.X+l3.W)
			for _, r := range []ui.Rect{status, up, down, l0, l1, l2, l3} {
				near(r.H, dashboardTileHeight)
			}
		})
	}
}
