package main

import (
	"fmt"
	"image"
	"image/color"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestToggleMatrix draws the window in every combination of the mode and
// the column toggles, with a macOS inset title bar and its traffic lights:
//
//	PEEK_REPO=~/i/hyoban.cc PEEK_OUT=/tmp/peek go test -run ToggleMatrix
func TestToggleMatrix(t *testing.T) {
	a, out := realApp(t)
	tt := ui.NewTester(a.view, 1100, 640)
	tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 78})
	for _, mode := range []int{modeChanges, modeFiles} {
		for _, history := range []bool{true, false} {
			for _, tree := range []bool{true, false} {
				if mode == modeFiles && !history {
					continue // the history never shows in files mode
				}
				a.mode, a.showHistory, a.showTree = mode, history, tree
				settle(tt)
				name := fmt.Sprintf("%s-mode%d-history%v-tree%v.png", out, mode, history, tree)
				save(t, name, trafficLights(tt.Image(), 1))
			}
		}
	}
}

// trafficLights paints the window controls where macOS puts them, on an
// image drawn at the scale.
func trafficLights(img *image.RGBA, scale int) *image.RGBA {
	colors := []color.RGBA{{0xff, 0x5f, 0x57, 0xff}, {0xfe, 0xbc, 0x2e, 0xff}, {0x28, 0xc8, 0x40, 0xff}}
	for i, col := range colors {
		cx, cy, r := (20+6+i*20)*scale, titleBarHeight/2*scale, 6*scale
		for y := cy - r; y <= cy+r; y++ {
			for x := cx - r; x <= cx+r; x++ {
				if (x-cx)*(x-cx)+(y-cy)*(y-cy) <= r*r {
					img.Set(x, y, col)
				}
			}
		}
	}
	return img
}
