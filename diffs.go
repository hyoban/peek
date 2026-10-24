package main

import (
	"fmt"
	"path"
	"strings"

	"peek/fileicons"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// fileDiff is the diff of one file of the commit, loaded as its rows come
// into view.
type fileDiff struct {
	change  change
	state   *editor.DiffState
	note    string // why the diff does not show
	loading bool
	stale   bool // the file may have changed since it loaded
}

// The stacked diffs list two rows a file: its header, pinned at the top
// while its diff scrolls under it, then its diff.
func diffFile(row int) int   { return row / 2 }
func headerRow(file int) int { return file * 2 }
func isHeader(row int) bool  { return row%2 == 0 }

// resetDiffs lists the files changed, in the tree's order, their diffs
// not loaded yet, scrolled to the top.
func (a *app) resetDiffs() {
	a.diffs = nil
	a.diffGen++
	for _, p := range a.changeTree.files() {
		a.diffs = append(a.diffs, &fileDiff{change: a.changes[p]})
	}
	a.diffList = ui.ListState{}
	a.scrollTarget, a.settledFirst = -1, -1
}

func (a *app) diffIndex(p string) int {
	for i, d := range a.diffs {
		if d.change.Path == p {
			return i
		}
	}
	return -1
}

// scrollToFile shows the file's diff at the top of the column, as a click
// on it in the tree asks.
func (a *app) scrollToFile(p string) {
	if i := a.diffIndex(p); i >= 0 {
		a.scrollTarget = i
		a.diffList.ScrollTo(headerRow(i), ui.Start)
	}
}

// followScroll chooses in the tree the file at the top of the column as
// the user scrolls, once a scroll the tree asked for has come.
func (a *app) followScroll() {
	if len(a.diffs) == 0 {
		return
	}
	first, last := a.diffList.Visible()
	if a.scrollTarget >= 0 {
		if row := headerRow(a.scrollTarget); row >= first && row <= last {
			a.scrollTarget, a.settledFirst = -1, first
		}
		return
	}
	if first == a.settledFirst {
		return
	}
	a.settledFirst = -1
	i := diffFile(first)
	if i < 0 || i >= len(a.diffs) {
		return
	}
	if p := a.diffs[i].change.Path; p != a.file {
		a.file = p
		a.changeTree.show(p)
	}
}

func (a *app) diffPane(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		columnHeader(c, func() {
			if cm, ok := a.current(); ok {
				ui.Text(c, cm.Subject).FontWeight(600).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			}
			if n := len(a.diffs); n > 0 {
				ui.Text(c, count(n, "file")).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Shrink(0)
			}
		})
		ui.Divider(c)
		if len(a.diffs) == 0 {
			emptyNote(c, "No changes")
			return
		}
		a.diffList.Header = isHeader
		a.diffList.Key = func(row int) any {
			return fmt.Sprintf("%v:%s", isHeader(row), a.diffs[diffFile(row)].change.Path)
		}
		ui.List(c.Key("diffs"), &a.diffList, len(a.diffs)*2, func(row int) {
			d := a.diffs[diffFile(row)]
			if isHeader(row) {
				diffHeader(c, d)
				return
			}
			a.diffBody(c, d)
		}).Grow(1).FillWidth().Label("Diffs")
	})
}

// diffHeader is a file's bar, pinned while its diff scrolls.
func diffHeader(c *ui.Context, d *fileDiff) {
	t := c.Theme()
	ch := d.change
	dir, name := path.Split(ch.Path)
	ui.Row(c).FillWidth().Height(40).AlignItems(ui.Center).Gap(t.Space(2)).Padding(0, t.Space(4)).
		Background(t.Surface).BorderColor(t.Border.Alpha(0.6)).BorderWidth(0, 0, 1).Children(func() {
		fileicons.View(c, ch.Path, 16, t.TextMuted, ui.Color{})
		// The name first, then where it is, as Xcode's jump bar.
		ui.Text(c, name).FontWeight(600).SingleLine().Shrink(0)
		where := strings.TrimSuffix(dir, "/")
		if ch.Old != ch.Path {
			where = "from " + ch.Old
		}
		ui.Text(c, where).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
		if d.state != nil {
			added, deleted := d.state.Stats()
			ui.Textf(c, "+%d", added).FontSize(smallSize).Font("monospace").TextColor(statusColor(t, "A")).Shrink(0)
			ui.Textf(c, "−%d", deleted).FontSize(smallSize).Font("monospace").TextColor(statusColor(t, "D")).Shrink(0)
		}
		statusMark(c, ch.Status, false)
	})
}

func (a *app) diffBody(c *ui.Context, d *fileDiff) {
	t := c.Theme()
	if (d.state == nil && d.note == "" || d.stale) && !d.loading {
		a.loadDiff(d)
	}
	switch {
	case d.note != "":
		ui.Text(c, d.note).TextColor(t.TextMuted).Padding(t.Space(4), t.Space(3))
	case d.state == nil:
		ui.Text(c, "Loading…").TextColor(t.TextMuted).Padding(t.Space(4), t.Space(3))
	default:
		editor.DiffView(c.Key("diff:"+d.change.Path), d.state).FillWidth().Padding(0, 0, t.Space(4))
	}
}

// loadDiff reads the file's texts and diffs them off the UI thread.
func (a *app) loadDiff(d *fileDiff) {
	d.loading, d.stale = true, false
	cm, _ := a.current()
	gen := a.diffGen
	shown := d.state != nil // which a reload updates where it is
	a.background(func() func() {
		before, after := fileTexts(a.repo, cm.Hash, d.change)
		note := textNote(before, after)
		if note == "" && before == after {
			note = "Only the file's mode changed"
		}
		var state *editor.DiffState
		if note == "" && !shown {
			state = &editor.DiffState{
				Old:        codeBuffer(d.change.Path, before),
				New:        codeBuffer(d.change.Path, after),
				Layout:     editor.DiffUnified,
				FitContent: true,
				// Long lines wrap, as Xcode's do: a diff that scrolled
				// sideways would drift with the slant of every vertical
				// swipe on a touchpad, the list taking the swipe's height
				// and the diff under the pointer its width.
				Wrap: true,
				Font: codeFont,
			}
			state.Stats() // diffs the texts here rather than in a frame
		}
		return func() {
			d.loading = false
			if gen != a.diffGen {
				return // another commit was chosen meanwhile
			}
			if note == "" && d.state != nil {
				// The diff shown follows its texts, keeping its place
				// and the lines expanded.
				resetText(d.state.Old, before)
				resetText(d.state.New, after)
				d.note = ""
				return
			}
			if note == "" && state == nil {
				d.stale = true // its diff went meanwhile, and loads anew
				return
			}
			d.note, d.state = note, state
		}
	})
}

// resetText puts the text in the buffer where it differs.
func resetText(b *editor.Buffer, text string) {
	if b.String() != text {
		b.Reset(text)
	}
}

const maxTextSize = 8 << 20

var codeFont = ui.Font{Family: "monospace", Size: codeSize}

// textNote tells why these texts do not show, "" where they do.
func textNote(texts ...string) string {
	for _, text := range texts {
		if isBinary(text) {
			return "Binary file"
		}
	}
	for _, text := range texts {
		if len(text) > maxTextSize {
			return "This file is too large to show"
		}
	}
	return ""
}

func isBinary(text string) bool {
	return strings.IndexByte(text[:min(len(text), 8000)], 0) >= 0
}

// codeBuffer holds a file's text, highlighted as its language is.
func codeBuffer(p, text string) *editor.Buffer {
	buf := editor.NewBuffer(text)
	buf.SetLanguage(editor.LanguageFor(p))
	return buf
}

// count says how many of a noun: "1 file", "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
