package main

import (
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSnapshot draws the window over a real repository, light and dark:
//
//	PEEK_REPO=~/i/hyoban.cc PEEK_OUT=/tmp/peek go test -run Snapshot
//
// PEEK_MODE=files draws the files mode, and PEEK_SCROLL scrolls into the
// second file's diff, under its pinned header.
func TestSnapshot(t *testing.T) {
	a, out := realApp(t)
	if os.Getenv("PEEK_MODE") == "files" {
		a.mode = modeFiles
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetScale(2)
	if os.Getenv("PEEK_SCROLL") != "" {
		a.diffList.ScrollTo(3, ui.Start)
		settle(tt)
	}
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		settle(tt)
		name := out + "-light.png"
		if dark {
			name = out + "-dark.png"
		}
		save(t, name, tt.Image())
	}
}
