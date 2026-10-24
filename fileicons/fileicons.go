// Package fileicons gives files the icons of file-icons
// (https://github.com/file-icons/atom): a glyph of an icon font, tinted
// with a colour of the file's kind, picked by the same rules, in the same
// order, as file-icons picks them by a file's path and name.
//
// gen/update.sh rebuilds tables.go and fonts from the latest file-icons.
package fileicons

import (
	"embed"
	"path"
	"regexp"
	"strings"
	"sync"

	"github.com/egoist/mygo/ui"
)

type rule struct {
	icon, dark, light string // the colours, for dark and light themes
	pattern           string
	priority          float64
	matchPath         bool // matches the path rather than the name
}

type glyph struct {
	font, char string
	size       float64 // in CSS pixels, of a 16 pixel row
}

// Icon is the icon of a file: a character of an icon font, and the colour
// of its kind, zero for none.
type Icon struct {
	Name  string
	Font  string
	Char  string
	Scale float32 // of the row's size
	Color ui.Color
}

//go:embed fonts/*.ttf
var fontFiles embed.FS

var (
	setup    sync.Once
	compiled []*regexp.Regexp
	mu       sync.Mutex
	cache    = map[string]int{} // a path's rule, -1 for none
)

func load() {
	files, _ := fontFiles.ReadDir("fonts")
	for _, f := range files {
		data, err := fontFiles.ReadFile("fonts/" + f.Name())
		if err == nil {
			ui.RegisterFont(data, family(strings.TrimSuffix(f.Name(), ".ttf")))
		}
	}
	compiled = make([]*regexp.Regexp, len(rules))
	for i, r := range rules {
		compiled[i] = regexp.MustCompile(r.pattern)
	}
}

// Warm registers the fonts and compiles the rules ahead of the first
// icon, which would otherwise do it in a frame.
func Warm() { setup.Do(load) }

func family(font string) string { return "fileicons-" + font }

// The suffixes file-icons looks past: a backup's, and a template's.
var (
	backup   = regexp.MustCompile(`~(?:orig|previous)$`)
	template = regexp.MustCompile(`(?i)^([^.]*\.[^.]+)\.(?:inc?|dist|tm?pl|te?mp|ti?dy)$`)
)

// match returns the rule of the file at p, a path within the repository,
// as file-icons' path strategy finds it: by the path, then by the name
// without a backup's or a template's suffix, then by the whole name
// where that matched nothing of priority.
func match(p string) int {
	mu.Lock()
	if i, ok := cache[p]; ok {
		mu.Unlock()
		return i
	}
	mu.Unlock()
	setup.Do(load)
	name := path.Base(p)
	filtered := template.ReplaceAllString(backup.ReplaceAllString(name, ""), "$1")
	find := func(s string, byPath bool) int {
		for i, r := range rules {
			if r.matchPath == byPath && compiled[i].MatchString(s) {
				return i
			}
		}
		return -1
	}
	i := find("/"+p, true)
	if i < 0 {
		i = find(filtered, false)
	}
	if filtered != name && (i < 0 || rules[i].priority < 1) {
		i = find(name, false)
	}
	mu.Lock()
	cache[p] = i
	mu.Unlock()
	return i
}

// For returns the icon of the file at p, a path within the repository,
// coloured for a dark theme or a light one: a page of text where no rule
// matches, as file-icons falls back to.
func For(p string, dark bool) Icon {
	name, colour := "text", ""
	if i := match(p); i >= 0 {
		r := rules[i]
		name, colour = r.icon, r.light
		if dark {
			colour = r.dark
		}
	}
	g := glyphs[name]
	icon := Icon{Name: name, Font: family(g.font), Char: g.char, Scale: float32(g.size / 16)}
	if c, ok := colours[colour]; ok {
		if dark {
			icon.Color = ui.Hex(c[0])
		} else {
			icon.Color = ui.Hex(c[1])
		}
	}
	return icon
}

// View draws the icon of the file at p in a square of size, in its colour,
// or tint where that is not zero, as on a row chosen; muted is the colour
// of an icon without one.
func View(c *ui.Context, p string, size float32, muted, tint ui.Color) ui.Element {
	icon := For(p, c.Theme().Dark)
	color := icon.Color
	switch {
	case tint != (ui.Color{}):
		color = tint
	case color == (ui.Color{}):
		color = muted
	}
	box := ui.Box(c).Size(size, size).Shrink(0).Center()
	box.Children(func() {
		ui.Text(c, icon.Char).Font(icon.Font).FontSize(size * icon.Scale).TextColor(color).SingleLine()
	})
	return box
}
