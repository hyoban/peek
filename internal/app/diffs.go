package app

import (
	"errors"
	"fmt"
	"path"
	"slices"
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
	// patch marks a diff of the patch's lines alone, as of a file too
	// large to show whole, whose lines are not numbered as the file's.
	patch bool
	deps  dependencyNotes // what npmx found of a manifest's new text
	read  bool            // marked read: its diff folds away under its header
	// flipped marks a diff shown the other way from its mark by a click
	// on its header: a read one shown, or one not read folded.
	flipped bool
}

// folded reports whether the diff folds away under its header.
func (d *fileDiff) folded() bool { return d.read != d.flipped }

// diffRowKey keys a file's row of the stacked diffs.
type diffRowKey struct {
	header bool
	path   string
}

// The stacked diffs list the commit's message first, then two rows a
// file: its header, pinned at the top while its diff scrolls under it,
// then its diff.
func diffFile(row int) int   { return max(row-1, 0) / 2 }
func headerRow(file int) int { return file*2 + 1 }
func isHeader(row int) bool  { return row%2 == 1 }

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
// marked so, or else where it is a test, docs or generated, as the user
// chose.
func (a *app) newDiff(ch git.Change) *fileDiff {
	read, ok := a.read[a.readKey(ch.Path)]
	if !ok {
		read = a.prefs().FoldKinds && filekind.Of(ch.Path) != filekind.Code
	}
	return &fileDiff{change: ch, read: read}
}

// openChanges opens the change tree's folders but those it had before,
// known, which stay as they were, and the groups of tests, docs and
// generated files, which start closed as their diffs start read.
func (a *app) openChanges(known map[string][]string) {
	t := &a.changeTree
	for dir := range t.dirs {
		if _, ok := known[dir]; ok || dir == "" {
			continue
		}
		if !isGroup(dir) || dir == groupPrefix+filekind.Names[filekind.Code] || !a.prefs().FoldKinds {
			t.state.Open.Add(dir)
		}
	}
	if t.reveal != "" {
		t.choose(t.reveal) // whose row the folders opened move
	}
}

func (a *app) indexDiffs() {
	a.diffOf = make(map[string]*fileDiff, len(a.diffs))
	for _, d := range a.diffs {
		a.diffOf[d.change.Path] = d
	}
	a.groupDiffs = map[string][]*fileDiff{}
	for _, item := range a.changeTree.dirs[""] {
		if isGroup(item) {
			a.groupDiffs[item] = a.filesDiffs(a.changeTree.filesUnder(item))
		}
	}
}

// readKey is the key of the file's mark in the commit shown, and of its
// comments.
func (a *app) readKey(p string) string {
	cm, _ := a.current()
	return a.entryKey(cm.Hash) + "\x00" + p
}

// entryKey is the key of the marks and comments of the commit: for the
// branch's changes, the branch's and where it left, which its new commits
// keep.
func (a *app) entryKey(hash string) string {
	if base, _, ok := git.SplitRange(hash); ok {
		return base + ".." + a.branch
	}
	return hash
}

// setRead marks the file's diff read or not, for as long as the app runs.
func (a *app) setRead(d *fileDiff, read bool) {
	d.read, d.flipped = read, false
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
		return a.filesDiffs([]string{item})
	}
	if diffs, ok := a.groupDiffs[item]; ok {
		return diffs
	}
	return a.filesDiffs(t.filesUnder(item))
}

// filesDiffs returns the diffs of these files.
func (a *app) filesDiffs(files []string) []*fileDiff {
	var diffs []*fileDiff
	for _, p := range files {
		if d := a.diffOf[p]; d != nil {
			diffs = append(diffs, d)
		}
	}
	return diffs
}

// lineCounts shows the lines added and deleted, green and red as GitHub's.
func lineCounts(c *ui.Context, added, deleted int, onAccent bool) {
	t := c.Theme()
	plus, minus := statusColor(c, "A"), statusColor(c, "D")
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

// heldLines is how many lines the diffs keep loaded at most, both sides
// counted: past it, those farthest from the view let theirs go, to load
// again as they come back into view, as a change of thousands of long
// files would else keep them all once scrolled through.
var heldLines = 1_000_000

// releaseFarDiffs lets the diffs farthest from the view go, past
// heldLines, but those near it or with what a reload would lose: lines
// selected, a comment being written, or the change gone to.
func (a *app) releaseFarDiffs() {
	lines := func(d *fileDiff) int { return d.state.Old.LineCount() + d.state.New.LineCount() }
	total := 0
	var loaded []int
	for i, d := range a.diffs {
		if d.state != nil {
			total += lines(d)
			loaded = append(loaded, i)
		}
	}
	if total <= heldLines {
		return
	}
	first, last := a.diffList.Visible()
	lo, hi := diffFile(first)-2, diffFile(last)+2
	distance := func(i int) int { return max(lo-i, i-hi, 0) }
	slices.SortFunc(loaded, func(i, j int) int { return distance(j) - distance(i) })
	for _, i := range loaded {
		d := a.diffs[i]
		if total <= heldLines || distance(i) == 0 {
			return
		}
		if _, selected := d.state.SelectedLines(); selected || d.loading || a.change.file == d.change.Path ||
			slices.ContainsFunc(a.commentsOf(d), func(cm *comment) bool { return cm.editing }) {
			continue
		}
		total -= lines(d)
		d.state, d.stale = nil, false
	}
}

// scrollToFile shows the file's diff at the top of the column, as a click
// on it in the tree asks: the first's with the message over it, the top
// of the column, as the message, a pull request's read later, would
// otherwise grow above it, out of sight.
func (a *app) scrollToFile(p string) {
	if i := a.diffIndex(p); i >= 0 {
		a.scrollTarget = i
		row := headerRow(i)
		if i == 0 {
			row = 0
		}
		a.diffList.ScrollTo(row, ui.Start)
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
	jumpBar(c, title, a.github, func() {
		mutedDetail(c, detail)()
		if len(a.diffs) > 0 {
			a.commentsButton(c)
			toolGroup(c, "Diff Toolbar", func() {
				a.changeButtons(c)
				a.layoutButton(c)
			})
		}
	}, func() {
		if len(a.diffs) == 0 {
			emptyNote(c, "No changes")
			return
		}
		a.diffList.Header = isHeader
		a.diffList.Key = func(row int) any {
			if row == 0 {
				return "message"
			}
			return diffRowKey{isHeader(row), a.diffs[diffFile(row)].change.Path}
		}
		ui.List(c.Key("diffs"), &a.diffList, len(a.diffs)*2+1, func(row int) {
			if row == 0 {
				a.commitMessage(c)
				return
			}
			d := a.diffs[diffFile(row)]
			if isHeader(row) {
				a.diffHeader(c, d)
				return
			}
			a.diffBody(c, d)
		}).Grow(1).FillWidth().Label("Diffs")
	})
}

// commitMessage is the first row of the diffs, as Mail shows a message's
// header over it: who made the commit and when, its hash, and its message,
// to read at a glance, its subject being the jump bar's. The work tree has
// none, and the branch's changes their pull request's, where there is one.
func (a *app) commitMessage(c *ui.Context) {
	if pr := a.shownPull(); pr != nil {
		a.pullHeader(c, pr)
		return
	}
	cm, ok := a.current()
	if !ok || cm.Hash == git.Worktree || git.IsRange(cm.Hash) {
		// A line, not nothing, for the list to keep its place by it at the
		// top: a pull request's header, read later, then grows down from
		// it, not up out of sight.
		ui.Row(c).FillWidth().Height(1)
		return
	}
	t := c.Theme()
	services := c.Services()
	ui.Column(c).FillWidth().Gap(t.Space(3)).Padding(t.Space(3), t.Space(4), t.Space(4), t.Space(4)).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(4)).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Grow(1).MinWidth(0).Gap(t.Space(1.5)).Children(func() {
				a.personLine(c, git.Person{Name: cm.Author, Email: cm.Email}, "")
				for _, co := range cm.CoAuthors {
					a.personLine(c, co, "Co-author")
				}
			})
			ui.Column(c).Shrink(0).AlignItems(ui.End).Gap(t.Space(0.5)).Children(func() {
				ui.Text(c, cm.Time.Format("Monday, January 2, 2006 at 15:04")).FontSize(smallSize).TextColor(t.TextMuted).SingleLine()
				if len(cm.Tags) > 0 {
					a.commitTags(c, cm.Tags)
				}
				ui.Text(c, cm.Short).FontSize(smallSize).Font(monoFamily).TextColor(t.TextMuted).SingleLine().
					Tooltip(cm.Hash).ContextMenu(func(m *ui.Menu) {
					if m.Item("Copy Commit Hash").Chosen() {
						services.WriteClipboard(cm.Hash)
						c.Toast("Commit Hash Copied")
					}
				})
			})
		})
		if body := strings.TrimSpace(cm.Body); body != "" {
			ui.Text(c, body).TextColor(t.Text).Selectable().FillWidth()
		}
	})
}

// commitTags are the tags on a commit, each in a capsule of its own.
func (a *app) commitTags(c *ui.Context, tags []string) {
	t := c.Theme()
	services := c.Services()
	ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
		for _, tag := range tags {
			ui.Row(c.Key("tag:"+tag)).Gap(t.Space(1)).AlignItems(ui.Center).Padding(0, t.Space(1.5)).
				Radius(999).Background(t.TextMuted.Alpha(0.12)).TextColor(t.TextMuted).Tooltip("Tag " + tag).
				ContextMenu(func(m *ui.Menu) {
					if m.Item("Copy Tag Name").Chosen() {
						services.WriteClipboard(tag)
						c.Toast("Tag Name Copied")
					}
				}).Children(func() {
				ui.Icon(c, tagIcon).Size(10, 10).Shrink(0)
				ui.Text(c, tag).FontSize(smallSize).FontWeight(500).SingleLine()
			})
		}
	})
}

// diffHeader is a file's bar, pinned while its diff scrolls.
func (a *app) diffHeader(c *ui.Context, d *fileDiff) {
	t := c.Theme()
	ch := d.change
	dir, name := path.Split(ch.Path)
	bar := ui.Row(c).FillWidth().Height(32).AlignItems(ui.Center).Gap(t.Space(2)).Padding(0, t.Space(4)).
		Background(t.Surface).BorderColor(t.Border.Alpha(0.6)).BorderWidth(0, 0, 1)
	services := c.Services()
	bar.ContextMenu(func(m *ui.Menu) {
		a.readMenu(m, ch.Path)
		m.Separator()
		a.linesMenu(c, m, d)
		m.Separator()
		a.fileMenu(services, m, ch.Path, false)
	})
	// A click on a file's bar folds its diff or shows it again, as
	// GitHub's do, leaving its mark as it was. The arrow, not a pointing
	// hand, tells it, as the hand is for links on a Mac.
	bar.KeepFocus().OnClick(func() { d.flipped = !d.flipped })
	bar.Children(func() {
		chevron := ui.Icon(c, chevronIcon).Size(10, 10).Shrink(0).TextColor(t.TextMuted)
		if d.folded() {
			chevron.Rotate(-90)
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
			if r, ok := d.state.SelectedLines(); ok && !d.patch {
				a.linesButton(c, d, r)
			}
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
		a.configure(c, d)
		editor.DiffView(c.Key("diff:"+d.change.Path), d.state).FillWidth().Padding(0, 0, t.Space(4))
	}
}

// loadDiff reads the file's texts and diffs them off the UI thread.
func (a *app) loadDiff(d *fileDiff) {
	d.loading, d.stale = true, false
	cm, _ := a.current()
	gen := a.diffGen
	shown := d.state // which a reload updates where it is
	ch := d.change   // a refresh may change its counts meanwhile
	prefs := a.prefs()
	a.background(func() func() {
		// A text too large to show is not read whole: its patch shows.
		limit := maxTextSize
		if isBitmap(ch.Path) {
			limit = 0
		}
		before, after, err := git.FileTextsAtMost(a.repo, cm.Hash, ch, limit)
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
		if errors.Is(err, git.ErrTooLarge) {
			note = tooLarge
		}
		if note == "" && before == after {
			note = "Only the file's mode changed"
		}
		// A file too large to show whole shows its patch's lines.
		var patch *editor.FilePatch
		if note == tooLarge {
			patch = filePatch(a.repo, cm.Hash, ch)
		}
		// The texts shown are compared here, a long file's being long to
		// read back.
		oldSame, newSame := false, false
		if shown != nil && note == "" && patch == nil {
			oldSame, newSame = shown.Old.String() == before, shown.New.String() == after
		}
		var state *editor.DiffState
		if patch != nil || note == "" && shown == nil {
			if patch != nil {
				before, after, note = "", "", ""
			}
			state = &editor.DiffState{
				Old:        codeBuffer(ch.Path, before),
				New:        codeBuffer(ch.Path, after),
				Layout:     a.diffLayout(),
				FitContent: true,
				// Long lines wrap, as Xcode's do: a diff that scrolled
				// sideways would drift with the slant of every vertical
				// swipe on a touchpad, the list taking the swipe's height
				// and the diff under the pointer its width.
				Wrap: true,
				// A click expands 20 unchanged lines, as GitHub's: 100 ran
				// past the view, and the change with them.
				ExpansionLines:   20,
				IgnoreWhitespace: prefs.IgnoreWhitespace,
				Font:             prefs.codeFont(),
				Theme:            codeTheme(prefs.CodeTheme, false), // the window's appearance's as it shows
			}
			if patch != nil {
				state.SetPatch(*patch)
			}
			state.Stats() // diffs the texts here rather than in a frame
		}
		return func() {
			d.loading = false
			if gen != a.diffGen {
				return // another commit was chosen meanwhile
			}
			d.patch = patch != nil
			if d.patch {
				d.note, d.state = "", state
				return
			}
			if note == "" && d.state != nil {
				// The diff shown follows its texts, keeping its place
				// and the lines expanded.
				if d.state != shown {
					oldSame, newSame = false, false
				}
				if !oldSame {
					resetText(d.state.Old, before)
				}
				if !newSame {
					resetText(d.state.New, after)
				}
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

const (
	maxTextSize  = 8 << 20
	maxPatchSize = 4 << 20
)

const tooLarge = "This file is too large to show"

// filePatch reads the patch of the change to a file, nil where it has
// none of lines to show or is too large too.
func filePatch(repo, hash string, ch git.Change) *editor.FilePatch {
	out, err := git.Patch(repo, hash, ch, maxPatchSize)
	if err != nil {
		return nil
	}
	files, err := editor.ParsePatch(out)
	if err != nil || len(files) != 1 || files[0].Binary || len(files[0].Hunks) == 0 {
		return nil
	}
	return &files[0]
}

// monoFamily is the font of code: MonoLisa where it is installed, else
// the system's monospaced font.
const monoFamily = "MonoLisaCode, MonoLisa Variable, monospace"

// textNote tells why these texts do not show, "" where they do.
func textNote(texts ...string) string {
	for _, text := range texts {
		if isBinary(text) {
			return "Binary file"
		}
	}
	for _, text := range texts {
		if len(text) > maxTextSize {
			return tooLarge
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
func newCode(p, text string) *editor.State { return codeOf(codeBuffer(p, text), defaultSettings) }

// codeOf shows the text of a buffer made off the UI thread, as a long
// file's takes a while to, wrapped and its imports folded as the user
// chose.
func codeOf(buf *editor.Buffer, s settings) *editor.State {
	st := &editor.State{Buffer: buf, ReadOnly: true, SoftWrap: s.WrapLines, Font: s.codeFont(), Theme: codeTheme(s.CodeTheme, false)}
	if s.FoldImports {
		st.FoldImports()
	}
	return st
}

// count says how many of a noun: "1 file", "3 files".
func count(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}
