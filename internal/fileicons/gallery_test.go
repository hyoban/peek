package fileicons

import (
	"image/png"
	"os"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestGallery draws the icons of common files, light and dark, to check
// their sizes and alignment: PEEK_GALLERY=/tmp/icons go test -run Gallery,
// and PEEK_VSCODE=1 for those of VS Code's icon theme.
func TestGallery(t *testing.T) {
	out := os.Getenv("PEEK_GALLERY")
	if out == "" {
		t.Skip("PEEK_GALLERY not set")
	}
	if os.Getenv("PEEK_VSCODE") != "" {
		FollowVSCode()
		defer vscode.Store(nil)
	}
	files := []string{"main.go", "go.mod", "package.json", "README.md", "LICENSE", "Dockerfile", ".gitignore",
		"App.tsx", "index.ts", "a.test.ts", "index.js", "x.kt", "strings.xml", "style.css", "config.yml",
		"Makefile", "pnpm-lock.yaml", ".env", "notes.txt", "photo.png", "x.rs", "x.swift", "x.py", "x.rb",
		"x.sh", "x.json", "x.html", "x.vue", "x.svelte", "x.java", "x.c", "x.cpp", "x.h", "x.php", "x.sql",
		"x.lua", "x.toml", "x.zip", "x.pdf", "x.svg", "tsconfig.json", "CHANGELOG.md", "x.scss", "x.dart"}
	view := func(c *ui.Context) {
		ui.Grid(c).Columns(4).Gap(6).Padding(12).Fill().Background(c.Theme().Background).Children(func() {
			for _, f := range files {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					View(c, f, 16, c.Theme().TextMuted, ui.Color{})
					ui.Text(c, f).FontSize(15)
				})
			}
			// And folders, closed and open, where VS Code's theme has them.
			for _, d := range []string{"src", "internal", "docs", ".github", "fonts", "lib"} {
				for _, open := range []bool{false, true} {
					if icon := Folder(d, open, c.Theme().Dark); icon != nil {
						ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
							ui.Image(c, icon).Size(16, 16).Shrink(0)
							ui.Text(c, d).FontSize(15)
						})
					}
				}
			}
		})
	}
	tt := ui.NewTester(view, 760, 400)
	tt.SetScale(2)
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		tt.Frame()
		name := out + "-light.png"
		if dark {
			name = out + "-dark.png"
		}
		f, _ := os.Create(name)
		png.Encode(f, tt.Image())
		f.Close()
	}
}
