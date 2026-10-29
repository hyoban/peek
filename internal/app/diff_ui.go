package app

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"

	"peek/internal/git"
	"peek/internal/review"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The diffs only show: no change is reverted, kept or staged from them.

// diffLayout is how the diffs lay out their texts: side by side, or the
// new one with the lines deleted above where they were.
func (a *app) diffLayout() editor.DiffLayout {
	if a.split {
		return editor.DiffSplit
	}
	return editor.DiffUnified
}

// layoutButton switches the diffs between side by side and one over the
// other, its icon the layout a click shows.
func (a *app) layoutButton(c *ui.Context) {
	label, svg := "Show Side by Side", splitIcon
	if a.split {
		label, svg = "Show Unified", unifiedIcon
	}
	ui.Button(c.Key("layout"), "").Shrink(0).Label(label).Tooltip(label).
		Children(func() { ui.Icon(c, svg).Size(16, 16) }).OnClick(func() { a.split = !a.split })
}

// configure sets the options of a file's diff that follow the window: its
// layout, the lines selected by their numbers, the comments asked for at
// the gutter, and what shows under lines.
func (a *app) configure(c *ui.Context, d *fileDiff) {
	st := d.state
	st.Layout = a.diffLayout()
	st.Theme = codeTheme(a.prefs().CodeTheme, c.Theme().Dark)
	// A patch's lines are not numbered as the file's, for references.
	st.LineSelection = !d.patch
	st.OnGutterButton = nil
	if !d.patch {
		st.OnGutterButton = func(r editor.DiffLineRange) { a.addComment(d, r) }
	}
	if st.RenderAnnotation == nil {
		st.RenderAnnotation = func(c *ui.Context, an editor.DiffAnnotation) { a.annotation(c, d, an) }
	}
	a.checkDiffDependencies(d)
	a.annotate(c, d)
}

// changeMark is a change of the diffs, the hunk of a file's, as Next and
// Previous Change step through them.
type changeMark struct {
	file string
	hunk int // in the file's Hunks; lastHunk for its last, once it loads
	// reveal scrolls it into view once its diff shows, not before the
	// frame after, so that a scroll to its file comes first.
	reveal bool
	after  int
}

const lastHunk = -2

// hunkCount counts the changes of a file's diff that Next Change stops
// at: its hunks, or the file itself where it shows no lines; -1 where it
// has not loaded yet.
func hunkCount(d *fileDiff) int {
	switch {
	case d.state != nil:
		return len(d.state.Hunks())
	case d.note != "" || d.picture != nil:
		return 1
	}
	return -1
}

// goChange goes to the next change of the diffs, or the previous with
// step -1, from the one gone to last, or from the top of the file at the
// top where the user scrolled away: the next file's first where the file
// has no more, skipping those folded.
func (a *app) goChange(step int) {
	if len(a.diffs) == 0 {
		return
	}
	i, h := a.diffIndex(a.change.file), a.change.hunk
	if i < 0 || a.change.file != a.file {
		i, h = max(a.diffIndex(a.file), 0), -1
		if step < 0 {
			h = 0
		}
	}
	if h == lastHunk {
		h = max(hunkCount(a.diffs[i]), 0)
	}
	for {
		d := a.diffs[i]
		if !d.folded() {
			n := hunkCount(d)
			if n < 0 {
				a.markChange(i, h+step)
				return
			}
			if next := h + step; next >= 0 && next < n {
				a.markChange(i, next)
				return
			}
		}
		i += step
		if i < 0 || i >= len(a.diffs) {
			return // none further, as at the last change
		}
		h = -1
		if step < 0 {
			h = lastHunk
			if n := hunkCount(a.diffs[i]); n >= 0 {
				h = n
			}
		}
	}
}

// markChange goes to hunk h of the i'th diff, the file's first or its
// last where it has not loaded yet: its file at the top, and the hunk in
// view once its diff shows.
func (a *app) markChange(i, h int) {
	d := a.diffs[i]
	if hunkCount(d) < 0 {
		h = max(h, 0)
		if h > 0 {
			h = lastHunk
		}
	}
	a.change = changeMark{file: d.change.Path, hunk: h, reveal: true, after: a.frames + 1}
	a.file = d.change.Path
	a.changeTree.show(d.change.Path)
	first, last := a.diffList.Visible()
	if d.state == nil || headerRow(i) < first || headerRow(i)+1 > last {
		a.scrollTarget = i
		a.diffList.ScrollTo(headerRow(i), ui.Start)
	}
	if d.state == nil && hunkCount(d) >= 0 {
		a.change.reveal = false // a file without lines: its header is the change
	}
}

// revealKey keys the annotation that scrolls the change gone to into
// view, findingKey a finding of the review's, commentKey a comment and
// dependencyKey what npmx found of a dependency.
type (
	revealKey     struct{}
	findingKey    int
	commentKey    int
	dependencyKey int
)

// revealAnnotation is the annotation over the change gone to, in the
// frame it comes into view, where its diff shows: under the line before
// it, the change at the top of the file being under the file's header.
func (a *app) revealAnnotation(d *fileDiff) (editor.DiffAnnotation, bool) {
	m := &a.change
	if !m.reveal || m.file != d.change.Path || a.frames < m.after || d.state == nil {
		return editor.DiffAnnotation{}, false
	}
	hunks := d.state.Hunks()
	if m.hunk == lastHunk || m.hunk >= len(hunks) {
		m.hunk = len(hunks) - 1
	}
	if m.hunk < 0 || hunks[m.hunk].NewStart == 0 {
		m.reveal = false
		if i := a.diffIndex(d.change.Path); i >= 0 {
			a.diffList.ScrollTo(headerRow(i), ui.Start)
		}
		return editor.DiffAnnotation{}, false
	}
	return editor.DiffAnnotation{Side: editor.DiffNew, Line: hunks[m.hunk].NewStart - 1, Key: revealKey{}}, true
}

// changeButtons go to the previous and the next change, as Xcode's
// arrows of its comparison do.
func (a *app) changeButtons(c *ui.Context) {
	for _, b := range []struct {
		key, label string
		step       int
		angle      float32
	}{{"previous-change", "Previous Change", -1, 180}, {"next-change", "Next Change", 1, 0}} {
		ui.Button(c.Key(b.key), "").Shrink(0).Label(b.label).Tooltip(b.label).Children(func() {
			ui.Icon(c, chevronIcon).Size(12, 12).Rotate(b.angle)
		}).OnClick(func() { a.goChange(b.step) })
	}
}

// lineReference names lines of a file's diff as an editor or an agent
// takes them: path:10-20, a deleted line's path as it was.
func lineReference(ch git.Change, r editor.DiffLineRange) string {
	p, lines := ch.Path, fmt.Sprint(r.Start+1)
	if r.End > r.Start {
		lines += fmt.Sprintf("-%d", r.End+1)
	}
	if r.Side == editor.DiffOld {
		return ch.Old + ":" + lines + " (before)"
	}
	return p + ":" + lines
}

// lineLink is the address of lines of a commit's diff on GitHub, as its
// commit page anchors them, or the branch's comparing page for its
// changes: L for the old side's, R for the new one's.
func lineLink(github, hash string, ch git.Change, r editor.DiffLineRange) string {
	page := "commit/" + hash
	if base, head, ok := git.SplitRange(hash); ok {
		page = "compare/" + base + "..." + head
	}
	side := "R"
	if r.Side == editor.DiffOld {
		side = "L"
	}
	anchor := fmt.Sprintf("%s%d", side, r.Start+1)
	if r.End > r.Start {
		anchor += fmt.Sprintf("-%s%d", side, r.End+1)
	}
	return fmt.Sprintf("%s/%s#diff-%x%s", github, page, sha256.Sum256([]byte(ch.Path)), anchor)
}

// linesButton is the lines selected by their numbers in the file's
// header, which a click copies a reference to.
func (a *app) linesButton(c *ui.Context, d *fileDiff, r editor.DiffLineRange) {
	t := c.Theme()
	label := fmt.Sprintf("Line %d", r.Start+1)
	if r.End > r.Start {
		label = fmt.Sprintf("Lines %d–%d", r.Start+1, r.End+1)
	}
	b := ui.ButtonBase(c.Key("lines:"+d.change.Path)).Shrink(0).Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Radius).
		TextColor(t.Accent).Label("Copy Reference to " + label).Tooltip("Copy Reference to " + label)
	flat(b, t, false)
	if b.Clicked() {
		c.Services().WriteClipboard(lineReference(d.change, r))
		c.Toast("Reference Copied")
	}
	b.ContextMenu(func(m *ui.Menu) { a.linesMenu(c, m, d) })
	b.Children(func() { ui.Text(c, label).FontSize(smallSize).FontWeight(600).SingleLine() })
}

// linesMenu is the part of a file's menu about the lines selected by
// their numbers: a reference to copy, their place on GitHub, a comment.
func (a *app) linesMenu(c *ui.Context, m *ui.Menu, d *fileDiff) {
	if d.state == nil || d.patch {
		return
	}
	r, ok := d.state.SelectedLines()
	services := c.Services()
	if m.Item("Copy Reference to Lines").Disabled(!ok).Chosen() {
		services.WriteClipboard(lineReference(d.change, r))
		c.Toast("Reference Copied")
	}
	cm, _ := a.current()
	online := ok && a.github != "" && cm.Hash != git.Worktree
	if m.Item("Copy GitHub Link to Lines").Disabled(!online).Chosen() {
		services.WriteClipboard(lineLink(a.github, cm.Hash, d.change, r))
		c.Toast("Link Copied")
	}
	if m.Item("Open Lines on GitHub").Disabled(!online).Chosen() {
		services.OpenURL(lineLink(a.github, cm.Hash, d.change, r))
	}
	if m.Item("Comment on Lines").Disabled(!ok).Chosen() {
		a.addComment(d, r)
	}
}

// comment is the user's note on lines of a diff, kept while the app runs,
// to copy as a prompt for an agent with what it is about.
type comment struct {
	lines   editor.DiffLineRange
	text    string
	draft   string
	editing bool
}

func (a *app) commentsOf(d *fileDiff) []*comment { return a.comments[a.readKey(d.change.Path)] }

// addComment starts a comment under the lines, or edits the one there.
func (a *app) addComment(d *fileDiff, r editor.DiffLineRange) {
	key := a.readKey(d.change.Path)
	for _, cm := range a.comments[key] {
		if cm.lines == r {
			cm.editing, cm.draft = true, cm.text
			return
		}
	}
	if a.comments == nil {
		a.comments = map[string][]*comment{}
	}
	a.comments[key] = append(a.comments[key], &comment{lines: r, editing: true})
}

func (a *app) deleteComment(d *fileDiff, cm *comment) {
	key := a.readKey(d.change.Path)
	a.comments[key] = slices.DeleteFunc(a.comments[key], func(o *comment) bool { return o == cm })
}

// hasComments reports whether the commit shown has comments written.
func (a *app) hasComments() bool {
	cm, _ := a.current()
	prefix := a.entryKey(cm.Hash) + "\x00"
	for key, cms := range a.comments {
		if strings.HasPrefix(key, prefix) && slices.ContainsFunc(cms, func(c *comment) bool { return c.text != "" }) {
			return true
		}
	}
	return false
}

// shownComments lists the comments on the diffs shown, in their order and
// their lines', those still drafted left out.
func (a *app) shownComments() (diffs []*fileDiff, comments []*comment) {
	if !a.hasComments() {
		return nil, nil // each frame asks, which a walk of every diff would slow
	}
	for _, d := range a.diffs {
		cms := slices.Clone(a.commentsOf(d))
		slices.SortStableFunc(cms, func(x, y *comment) int { return x.lines.Start - y.lines.Start })
		for _, cm := range cms {
			if cm.text != "" {
				diffs, comments = append(diffs, d), append(comments, cm)
			}
		}
	}
	return diffs, comments
}

// commentsPrompt is the comments on the diffs shown as a prompt: each
// comment under the reference to its lines and their text, quoted.
func (a *app) commentsPrompt() string {
	diffs, comments := a.shownComments()
	var b strings.Builder
	for i, cm := range comments {
		d := diffs[i]
		if i > 0 {
			b.WriteString("\n\n")
		}
		b.WriteString(lineReference(d.change, cm.lines) + "\n")
		if d.state != nil {
			buf := d.state.New
			if cm.lines.Side == editor.DiffOld {
				buf = d.state.Old
			}
			for l := cm.lines.Start; l <= cm.lines.End && l < buf.LineCount(); l++ {
				b.WriteString("> " + strings.TrimRight(buf.Line(l), "\r\n") + "\n")
			}
		}
		b.WriteString(cm.text)
	}
	return b.String()
}

// commentsButton tells how many comments the diffs shown have, and copies
// them as a prompt.
func (a *app) commentsButton(c *ui.Context) {
	_, comments := a.shownComments()
	if len(comments) == 0 {
		return
	}
	t := c.Theme()
	label := count(len(comments), "comment")
	b := ui.ButtonBase(c.Key("comments")).Shrink(0).Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Radius).
		TextColor(t.TextMuted).Label(label).Tooltip("Copy the comments as a prompt")
	flat(b, t, false)
	b.Menu(func(m *ui.Menu) {
		if m.Item("Copy Comments").Chosen() {
			c.Services().WriteClipboard(a.commentsPrompt())
			c.Toast("Comments Copied")
		}
		m.Separator()
		if m.Item("Delete Comments").Chosen() {
			cm, _ := a.current()
			for key := range a.comments {
				if strings.HasPrefix(key, a.entryKey(cm.Hash)+"\x00") {
					delete(a.comments, key)
				}
			}
		}
	})
	b.Children(func() {
		ui.Row(c).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, commentIcon).Size(13, 13).Shrink(0)
			ui.Text(c, label).FontSize(smallSize).SingleLine()
		})
	})
}

// findings are those of the review shown that are about the file's diff:
// the uncommitted changes' where the review was of them, the branch's
// changes' where it was of those, else the commits' at the head it was of.
func (a *app) findings(d *fileDiff) []review.Finding {
	r := &a.review
	cm, ok := a.current()
	if r.result == nil || r.stale || !ok || d.patch {
		return nil
	}
	if r.base == "" && cm.Hash != git.Worktree || r.base != "" && git.Tip(cm.Hash) != r.head ||
		(r.scope == review.BranchScope) != git.IsRange(cm.Hash) && r.base != "" {
		return nil
	}
	var fs []review.Finding
	for _, f := range r.result.Findings {
		if f.Path == d.change.Path {
			fs = append(fs, f)
		}
	}
	return fs
}

// Annotations sit a little apart from the lines around them, a card of
// smallSize text in each.
const (
	annotationMargin = 4
	annotationPad    = 8
	annotationGap    = 4
	commentBarHeight = 18
	commentInput     = 72
	commentButtons   = 24
)

// annotationWidth is the width of the text of the annotations of a diff,
// at most: the diffs' column, less a gutter and the card's insets, half of
// it side by side.
func (a *app) annotationWidth() float32 {
	w := a.diffWidth - 72
	if a.split {
		w /= 2
	}
	return max(w-2*annotationPad-24, 120)
}

// annotate lists what shows under the lines of the diff: the review's
// findings, what npmx found of the dependencies added, the comments, and
// what scrolls the change gone to into view.
func (a *app) annotate(c *ui.Context, d *fileDiff) {
	st := d.state
	st.Annotations = st.Annotations[:0]
	w := a.annotationWidth()
	text := func(s string, weight int) float32 {
		_, h := c.MeasureText(w, ui.Span{Text: s, Size: smallSize, Weight: weight})
		return h
	}
	card := func(h float32) float32 { return h + 2*annotationMargin + 2*annotationPad }
	for i, f := range a.findings(d) {
		line := f.Line - 1
		if line < 0 || line >= st.New.LineCount() {
			continue
		}
		h := card(text(findingTitle(f), 600) + annotationGap + text(f.Body, 0))
		st.Annotations = append(st.Annotations, editor.DiffAnnotation{Side: editor.DiffNew, Line: line, Height: h, Key: findingKey(i)})
	}
	for i, f := range dependencyFindings(d) {
		h := card(text(f.Message, 0))
		st.Annotations = append(st.Annotations, editor.DiffAnnotation{Side: editor.DiffNew, Line: f.Range.Start.Line, Height: h, Key: dependencyKey(i)})
	}
	for i, cm := range a.commentsOf(d) {
		h := card(commentBarHeight + annotationGap + text(cm.text, 0))
		if cm.editing {
			h = card(commentInput + annotationPad + commentButtons)
		}
		st.Annotations = append(st.Annotations, editor.DiffAnnotation{Side: cm.lines.Side, Line: cm.lines.End, Height: h, Key: commentKey(i)})
	}
	if an, ok := a.revealAnnotation(d); ok {
		st.Annotations = append(st.Annotations, an)
	}
}

func findingTitle(f review.Finding) string { return fmt.Sprintf("[P%d] %s", f.Priority, f.Title) }

// annotation builds what shows under a line of the diff.
func (a *app) annotation(c *ui.Context, d *fileDiff, an editor.DiffAnnotation) {
	switch k := an.Key.(type) {
	case revealKey:
		// A box from a little over the change down well past the view,
		// which scrolling into view puts at the top, under the pinned
		// header, as a box taller than the view shows from its start.
		a.change.reveal = false
		ui.Box(c.Key("reveal")).Absolute().Left(0).Top(-56).Width(1).Height(4000).ScrollIntoView()
	case findingKey:
		if fs := a.findings(d); int(k) < len(fs) {
			findingCard(c, fs[k])
		}
	case dependencyKey:
		if fs := dependencyFindings(d); int(k) < len(fs) {
			dependencyCard(c, fs[k])
		}
	case commentKey:
		if cms := a.commentsOf(d); int(k) < len(cms) {
			a.commentCard(c, d, cms[k])
		}
	}
}

// annotationCard is the card of an annotation, a bar of color at its
// start, as GitHub's review comments sit under their lines.
func annotationCard(c *ui.Context, bar ui.Color, build func()) {
	t := c.Theme()
	ui.Column(c).Fill().Padding(annotationMargin, t.Space(4), annotationMargin, annotationMargin).Children(func() {
		ui.Column(c).Fill().Gap(annotationGap).Padding(annotationPad, annotationPad, annotationPad, annotationPad+3).
			Radius(t.Radius).Background(t.Surface).Border(1, t.Border).Children(func() {
			ui.Box(c).Absolute().Left(0).Top(0).Bottom(0).Width(3).Background(bar)
			build()
		})
	})
}

// findingCard is a finding of the review under its line, red where it
// fails the review, orange for a minor one.
func findingCard(c *ui.Context, f review.Finding) {
	t := c.Theme()
	color := system(c, systemOrange)
	if f.Priority < 3 {
		color = system(c, systemRed)
	}
	annotationCard(c, color, func() {
		ui.Text(c, findingTitle(f)).FontSize(smallSize).FontWeight(600).TextColor(t.Text).Selectable().FillWidth()
		ui.Text(c, f.Body).FontSize(smallSize).TextColor(t.Text).Selectable().FillWidth()
	})
}

// commentCard is a comment under its lines: its text, or a field to write
// it in, Return with Command saving it and Escape leaving it as it was.
func (a *app) commentCard(c *ui.Context, d *fileDiff, cm *comment) {
	t := c.Theme()
	annotationCard(c, t.Accent, func() {
		if !cm.editing {
			ui.Row(c).FillWidth().Height(commentBarHeight).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
				ui.Text(c, "Comment").FontSize(smallSize).FontWeight(600).TextColor(t.TextMuted).Grow(1)
				if ui.Button(c.Key("edit"), "Edit").FontSize(smallSize).Clicked() {
					cm.editing, cm.draft = true, cm.text
				}
				if ui.Button(c.Key("delete"), "Delete").FontSize(smallSize).Clicked() {
					a.deleteComment(d, cm)
				}
			})
			ui.Text(c, cm.text).FontSize(smallSize).TextColor(t.Text).Selectable().FillWidth()
			return
		}
		save := func(text string) {
			cm.text, cm.editing = strings.TrimSpace(text), false
			if cm.text == "" {
				a.deleteComment(d, cm)
			}
		}
		cancel := func() {
			cm.editing = false
			if cm.text == "" {
				a.deleteComment(d, cm)
			}
		}
		ui.TextArea(c.Key("draft"), &cm.draft).FontSize(smallSize).FillWidth().Height(commentInput).
			Placeholder("Leave a comment").Label("Comment Text").AutoFocus().OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Key == ui.KeyEnter && k.Mods == ui.Cmd:
				save(k.Text())
				return true
			case k.Key == ui.KeyEscape && k.Mods == 0:
				cancel()
				return true
			}
			return false
		})
		ui.Row(c).FillWidth().Height(commentButtons).Gap(t.Space(2)).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if ui.Button(c.Key("cancel"), "Cancel").Clicked() {
				cancel()
			}
			if ui.PrimaryButton(c.Key("save"), "Comment").Disabled(strings.TrimSpace(cm.draft) == "").Clicked() {
				save(cm.draft)
			}
		})
	})
}
