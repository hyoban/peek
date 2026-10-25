package app

import (
	"fmt"
	"path"
	"strings"

	"peek/internal/fileicons"
	"peek/internal/filekind"
	"peek/internal/git"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// fileDiff is the diff of one file of the commit, loaded as its rows come
// into view.
type fileDiff struct {
	change  git.Change
	state   *editor.DiffState
	picture *pictureDiff // the diff of a changed picture, in place of state
	note    string       // why the diff does not show
	loading bool
	stale   bool // the file may have changed since it loaded
	read    bool // marked read: its diff folds away under its header
	opened  bool // read, its diff shown again by a click on its header
}

// folded reports whether the diff folds away under its header.
func (d *fileDiff) folded() bool { return d.read && !d.opened }

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
		a.diffs = append(a.diffs, a.newDiff(a.changes[p]))
	}
	a.indexDiffs()
	a.diffList = ui.ListState{}
	a.scrollTarget, a.settledFirst = -1, -1
}

// newDiff is the diff of a changed file of the commit, read where it was
// marked so, or else where it is a test, docs or generated.
func (a *app) newDiff(ch git.Change) *fileDiff {
	read, ok := a.read[a.readKey(ch.Path)]
	if !ok {
		read = filekind.Of(ch.Path) != filekind.Code
	}
	return &fileDiff{change: ch, read: read}
}

// openChanges opens the change tree, but for the groups of tests, docs
// and generated files, which start closed as their diffs start read.
func (a *app) openChanges() {
	t := &a.changeTree
	t.openAll()
	for _, item := range t.dirs[""] {
		if isGroup(item) && item != groupPrefix+filekind.Names[filekind.Code] {
			t.state.Open.Remove(item)
		}
	}
	t.remember()
}

func (a *app) indexDiffs() {
	a.diffOf = make(map[string]*fileDiff, len(a.diffs))
	for _, d := range a.diffs {
		a.diffOf[d.change.Path] = d
	}
}

// readKey is the key of the file's mark in the commit shown.
func (a *app) readKey(p string) string {
	cm, _ := a.current()
	return cm.Hash + "\x00" + p
}

// setRead marks the file's diff read or not, for as long as the app runs.
func (a *app) setRead(d *fileDiff, read bool) {
	d.read, d.opened = read, false
	if a.read == nil {
		a.read = map[string]bool{}
	}
	a.read[a.readKey(d.change.Path)] = read
}

// diffsUnder returns the diffs of the files the item of the change tree
// holds: a group's or a folder's, or a file's own.
func (a *app) diffsUnder(item string) []*fileDiff {
	t := &a.changeTree
	if !t.isDir(item) {
		if d := a.diffOf[item]; d != nil {
			return []*fileDiff{d}
		}
		return nil
	}
	var under []*fileDiff
	for _, p := range t.filesUnder(item) {
		if d := a.diffOf[p]; d != nil {
			under = append(under, d)
		}
	}
	return under
}

// lineCounts shows the lines added and deleted, green and red as GitHub's.
func lineCounts(c *ui.Context, added, deleted int, onAccent bool) {
	t := c.Theme()
	plus, minus := statusColor(t, "A"), statusColor(t, "D")
	if onAccent {
		plus, minus = t.AccentText, t.AccentText
	}
	ui.Textf(c, "+%d", added).FontSize(smallSize).Font(monoFamily).TextColor(plus).SingleLine().Shrink(0)
	ui.Textf(c, "−%d", deleted).FontSize(smallSize).Font(monoFamily).TextColor(minus).SingleLine().Shrink(0)
}

// sumLines adds up the lines the changes of these diffs add and delete.
func sumLines(diffs []*fileDiff) (added, deleted int) {
	for _, d := range diffs {
		added += d.change.Added
		deleted += d.change.Deleted
	}
	return added, deleted
}

// countRead counts the diffs read among these.
func countRead(diffs []*fileDiff) int {
	n := 0
	for _, d := range diffs {
		if d.read {
			n++
		}
	}
	return n
}

// readMenu marks the files of the item of the change tree read, or not,
// all at once.
func (a *app) readMenu(m *ui.Menu, item string) {
	diffs := a.diffsUnder(item)
	n := countRead(diffs)
	if m.Item("Mark as Viewed").Disabled(n == len(diffs)).Chosen() {
		for _, d := range diffs {
			a.setRead(d, true)
		}
	}
	if m.Item("Mark as Not Viewed").Disabled(n == 0).Chosen() {
		for _, d := range diffs {
			a.setRead(d, false)
		}
	}
}

// readFile reports whether the file's diff is marked read.
func (a *app) readFile(p string) bool {
	d := a.diffOf[p]
	return d != nil && d.read
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
func (a *app) followScroll(c *ui.Context) {
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
		c.Services().Invalidate() // for the tree to choose it
	}
}

func (a *app) diffPane(c *ui.Context) {
	title, detail := "", ""
	if cm, ok := a.current(); ok {
		title = cm.Subject
	}
	if n := len(a.diffs); n > 0 {
		detail = count(n, "file")
	}
	columnLinked(c, title, a.github, mutedDetail(c, detail), func() {
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
				a.diffHeader(c, d)
				return
			}
			a.diffBody(c, d)
		}).Grow(1).FillWidth().Label("Diffs")
	})
}

// diffHeader is a file's bar, pinned while its diff scrolls.
func (a *app) diffHeader(c *ui.Context, d *fileDiff) {
	t := c.Theme()
	ch := d.change
	dir, name := path.Split(ch.Path)
	bar := ui.Row(c).FillWidth().Height(32).AlignItems(ui.Center).Gap(t.Space(2)).Padding(0, t.Space(4)).
		Background(t.Surface).BorderColor(t.Border.Alpha(0.6)).BorderWidth(0, 0, 1)
	if d.read {
		// A click on a read file's bar shows its diff again, or folds it,
		// as GitHub's Viewed files open: still marked read. The arrow,
		// not a pointing hand, tells it, as the hand is for links on a Mac.
		bar.KeepFocus().OnClick(func() { d.opened = !d.opened })
	}
	bar.Children(func() {
		if d.read {
			chevron := ui.Icon(c, chevronIcon).Size(10, 10).Shrink(0).TextColor(t.TextMuted)
			if d.folded() {
				chevron.Rotate(-90)
			}
		}
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
			lineCounts(c, added, deleted, false)
		}
		statusMark(c, ch.Status, false)
		// Marked read, as GitHub's Viewed, which folds the diff away.
		ui.Checkbox(c.Key("read:"+ch.Path), &d.read, "Viewed").FontSize(smallSize).Shrink(0).
			OnClick(func() { a.setRead(d, d.read) })
	})
}

func (a *app) diffBody(c *ui.Context, d *fileDiff) {
	t := c.Theme()
	if (d.state == nil && d.picture == nil && d.note == "" || d.stale) && !d.loading {
		a.loadDiff(d) // for its counts too, where it is read
	}
	switch {
	case d.folded():
		ui.Row(c).FillWidth().Height(0)
	case d.note != "":
		ui.Text(c, d.note).TextColor(t.TextMuted).Padding(t.Space(4), t.Space(3))
	case d.picture != nil:
		pictureDiffView(c, d.picture)
	case d.state == nil:
		ui.Row(c).FillWidth().Padding(t.Space(4), t.Space(3)).Children(func() { ui.Spinner(c).Label("Loading") })
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
	ch := d.change          // a refresh may change its counts meanwhile
	a.background(func() func() {
		before, after := git.FileTexts(a.repo, cm.Hash, ch)
		if isBitmap(ch.Path) && before != after {
			picture, note := decodePictures(ch.Path, before, after)
			return func() {
				d.loading = false
				if gen == a.diffGen {
					d.note, d.picture = note, picture
				}
			}
		}
		note := textNote(before, after)
		if note == "" && before == after {
			note = "Only the file's mode changed"
		}
		var state *editor.DiffState
		if note == "" && !shown {
			state = &editor.DiffState{
				Old:        codeBuffer(ch.Path, before),
				New:        codeBuffer(ch.Path, after),
				Layout:     editor.DiffUnified,
				FitContent: true,
				// Long lines wrap, as Xcode's do: a diff that scrolled
				// sideways would drift with the slant of every vertical
				// swipe on a touchpad, the list taking the swipe's height
				// and the diff under the pointer its width.
				Wrap:  true,
				Font:  codeFont,
				Theme: codeTheme,
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

// monoFamily is the font of code: MonoLisa where it is installed, else
// the system's monospaced font.
const monoFamily = "MonoLisaCode, MonoLisa Variable, monospace"

var codeFont = ui.Font{Family: monoFamily, Size: codeSize}

// codeTheme is the default's, light or dark, with comments in italics.
var codeTheme = func() *editor.Theme {
	ct := &editor.Theme{} // the default's colors fill those left zero
	ct.Tokens[editor.TokenComment].Italic = true
	ct.Tokens[editor.TokenEmphasis].Italic = true
	ct.Tokens[editor.TokenHeading].Bold = true
	ct.Tokens[editor.TokenStrong].Bold = true
	return ct
}()

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

// newCode shows a file's text, its imports folded away, as they are
// seldom what a file is read for.
func newCode(p, text string) *editor.State {
	st := &editor.State{Buffer: codeBuffer(p, text), ReadOnly: true, SoftWrap: true, Font: codeFont, Theme: codeTheme}
	st.FoldImports()
	return st
}

// count says how many of a noun: "1 file", "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
