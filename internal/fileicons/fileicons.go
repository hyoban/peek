// Package fileicons gives files the icons of file-icons
// (https://github.com/file-icons/atom): a glyph of an icon font, tinted
// with a colour of the file's kind, picked by the same rules, in the same
// order, as file-icons picks them by a file's path and name; or those of
// the icon theme VS Code is set to, where FollowVSCode finds it.
//
// gen/update.sh rebuilds tables.go and fonts from the latest file-icons.
package fileicons

import (
	"embed"
	"image"
	stddraw "image/draw"
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
// of its kind, zero for none; or an SVG in its own colours, of VS Code's
// icon theme.
type Icon struct {
	Name  string
	Font  string
	Char  string
	Scale float32 // of the row's size
	Color ui.Color
	SVG   *ui.SVG
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
	if t := vscode.Load(); t != nil {
		id := t.icon(p, dark)
		if s := t.svg(id); s != nil {
			return Icon{Name: id, Scale: 1, SVG: s}
		}
	}
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

// Folder returns the icon of the folder at p, a path within the
// repository, open or closed, in VS Code's icon theme: nil where Peek does
// not follow one, or its icon cannot be read.
func Folder(p string, open, dark bool) *ui.SVG {
	t := vscode.Load()
	if t == nil {
		return nil
	}
	return t.svg(t.folder(p, open, dark))
}

// View draws the icon of the file at p in a square of size, in its colour,
// or tint where that is not zero, as on a row chosen; muted is the colour
// of an icon without one. An SVG keeps its colours, as VS Code keeps them.
func View(c *ui.Context, p string, size float32, muted, tint ui.Color) ui.Element {
	icon := For(p, c.Theme().Dark)
	if icon.SVG != nil {
		return ui.Image(c, icon.SVG).Size(size, size).Shrink(0)
	}
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

// pictures are the icons drawn as pictures, by name, size and theme.
var pictures = map[pictureKey]*image.RGBA{}

type pictureKey struct {
	name       string
	size       int
	dark, open bool
}

// Picture returns the icon of the file at p drawn as a picture size
// points square, at twice that in pixels, for a list of the system's to
// show: in its colours, or black where it has none, which mask tells the
// list to colour as it colours text. It draws each icon once.
func Picture(p string, size int, dark bool) (img image.Image, mask bool) {
	icon := For(p, dark)
	mask = icon.SVG == nil && icon.Color == (ui.Color{})
	k := pictureKey{name: icon.Name, size: size, dark: dark}
	if pic, ok := pictures[k]; ok {
		return pic, mask
	}
	pic := draw(size, func(c *ui.Context) {
		if icon.SVG != nil {
			ui.Image(c, icon.SVG).Size(float32(size), float32(size)).Shrink(0)
			return
		}
		color := icon.Color
		if mask {
			color = ui.Hex("#000000")
		}
		// A glyph wider than the square overflows it on both sides, as
		// the rows of the drawn tree show it.
		ui.Text(c, icon.Char).Font(icon.Font).FontSize(float32(size) * icon.Scale).TextColor(color).SingleLine().Shrink(0)
	})
	pictures[k] = pic
	return pic, mask
}

// FolderPicture returns the icon of the folder at p, open or closed, in
// VS Code's icon theme, as Picture does a file's: nil where Peek follows
// none.
func FolderPicture(p string, open bool, size int, dark bool) image.Image {
	t := vscode.Load()
	if t == nil {
		return nil
	}
	name := t.folder(p, open, dark)
	svg := t.svg(name)
	if svg == nil {
		return nil
	}
	k := pictureKey{name: "folder:" + name, size: size, dark: dark, open: open}
	if pic, ok := pictures[k]; ok {
		return pic
	}
	pic := draw(size, func(c *ui.Context) {
		ui.Image(c, svg).Size(float32(size), float32(size))
	})
	pictures[k] = pic
	return pic
}

// draw draws view in a square of size points, at twice that, over
// nothing: in the middle of a larger one, then cut to what it drew, so
// that a glyph wider than the square shows whole.
func draw(size int, view func(c *ui.Context)) *image.RGBA {
	room := size * 2
	pic := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Row(c).Size(float32(room), float32(room)).Center().Children(func() { view(c) })
	}, room, room, 2)
	drawn := image.Rectangle{}
	for y := pic.Rect.Min.Y; y < pic.Rect.Max.Y; y++ {
		for x := pic.Rect.Min.X; x < pic.Rect.Max.X; x++ {
			if pic.RGBAAt(x, y).A != 0 {
				drawn = drawn.Union(image.Rect(x, y, x+1, y+1))
			}
		}
	}
	side := max(size*2, drawn.Dx(), drawn.Dy())
	mid := pic.Rect.Size().Div(2)
	if !drawn.Empty() {
		mid = drawn.Min.Add(drawn.Size().Div(2))
	}
	square := image.Rect(0, 0, side, side).Add(mid.Sub(image.Pt(side/2, side/2)))
	out := image.NewRGBA(image.Rect(0, 0, side, side))
	stddraw.Draw(out, out.Rect, pic, square.Min, stddraw.Src)
	return out
}
