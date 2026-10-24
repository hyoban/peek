package main

import (
	"os"
	"strconv"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestSnapshot draws the window over a real repository, light and dark,
// with a macOS inset title bar and its traffic lights:
//
//	PEEK_REPO=~/i/hyoban.cc PEEK_OUT=/tmp/peek go test -run Snapshot
//
// PEEK_MODE=files draws the files mode, PEEK_COMMIT chooses the history's
// row, PEEK_FILE the file to show, and PEEK_SCROLL scrolls into the
// second file's diff, under its pinned header. The README's screenshots:
//
//	PEEK_REPO=. PEEK_OUT=docs/changes PEEK_COMMIT=3 go test -run 'Snapshot$'
//	PEEK_REPO=. PEEK_OUT=docs/files PEEK_MODE=files PEEK_COMMIT=1 PEEK_FILE=main.go go test -run 'Snapshot$'
func TestSnapshot(t *testing.T) {
	a, out := realApp(t)
	if os.Getenv("PEEK_MODE") == "files" {
		a.mode, a.modeChoice = modeFiles, modeFiles
	}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetScale(2)
	tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 78})
	settle(tt)
	if row, err := strconv.Atoi(os.Getenv("PEEK_COMMIT")); err == nil {
		a.commitRow = row
		settle(tt)
	}
	if p := os.Getenv("PEEK_FILE"); p != "" {
		if a.mode == modeFiles {
			a.allTree.show(p)
		} else {
			a.changeTree.show(p)
		}
		settle(tt)
	}
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
		save(t, name, trafficLights(tt.Image(), 2))
	}
}
