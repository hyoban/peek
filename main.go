package main

import (
	"log"
	"os"
	"path"
	"strings"
	"time"

	"peek/fileicons"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The modes of the window: the files the chosen commit changes, with
// their diffs, or all of its files, to read.
const (
	modeChanges = iota
	modeFiles
)

// app is the state the window shows: the history, the files of the chosen
// commit, and the diff or the text of the chosen file.
type app struct {
	win      *mygo.Window
	projects *projects // the window's, nil in tests
	avatars  *avatars  // nil shows initials alone
	repo     string
	branch   string
	github   string // the repository's page on GitHub, "" for none
	// inline runs loads on the UI thread, as tests and snapshots want,
	// and redraw asks for the frame a window's Update would draw then.
	// Those a pass starts wait in published for the next, as a window's
	// Update comes between passes, not within one.
	inline, redraw, building bool
	published                []func()
	err                      string
	// theme is this pass's, and inactiveTheme the one of lists without
	// the keyboard focus.
	theme, inactiveTheme *ui.Theme
	titleButton          ui.Handle // the project's name, under which the projects list

	showHistory, showTree   bool
	historyWidth, treeWidth float32
	mode                    int
	modeChoice              int // the mode switch's, which setMode follows

	commits    []commit
	commitList ui.ListState
	commitRow  int
	loadedRow  int // the commit whose changes are loaded, or loading
	// worktreeChanges are the work tree's, read with the history, for
	// its entry's first load.
	worktreeChanges []change
	// refreshing is while the history is read again, and refreshAgain
	// that files changed meanwhile.
	refreshing, refreshAgain bool

	// Changes mode.
	changes    map[string]change // by new path
	changeTree pathTree
	file       string // the file at the top of the diffs
	diffs      []*fileDiff
	diffOf     map[string]*fileDiff // the diffs by path
	diffGen    int                  // counts the lists of diffs, to drop stale loads
	diffList   ui.ListState
	read       map[string]bool // diffs marked read or not, by commit and path
	// scrollTarget is the file the diffs scroll to for the tree, -1 once
	// there; settledFirst the first row then, until the user scrolls.
	scrollTarget, settledFirst int

	// Files mode.
	allTree  pathTree
	filesFor int    // the row of the commit whose files are listed, or load; -1 for none
	openFile string // the file whose text shows, or loads
	code     *editor.State
	codeNote string // why the text does not show
}

func newApp(repo string) *app {
	return &app{
		repo:         repo,
		showHistory:  true,
		showTree:     true,
		historyWidth: 360,
		treeWidth:    320,
		commitRow:    -1,
		loadedRow:    -1,
		scrollTarget: -1,
		settledFirst: -1,
		filesFor:     -1,
		changeTree:   newPathTree(),
		allTree:      newPathTree(),
	}
}

const titleBarHeight = 52 // the height of an inset title bar

// The sizes of text, a little larger than the system's 13.
const (
	fontSize  = 15
	smallSize = 13 // secondary text
	badgeSize = 11 // the letters of the status badges
	codeSize  = 14
)

// themed sets this pass's theme: the system's, its text at the app's size
// as the user scales it, and the theme of lists without the focus.
func (a *app) themed(c *ui.Context) {
	t := c.Theme() // a copy for this pass
	t.FontSize = fontSize * c.Preferences().TextScale
	c.SetTheme(t)
	inactive := *t
	inactive.Accent = t.Text.Alpha(0.1)
	if t.Dark {
		inactive.Accent = t.Text.Alpha(0.14)
	}
	inactive.AccentText = t.Text
	a.theme, a.inactiveTheme = t, &inactive
}

func (a *app) view(c *ui.Context) {
	for len(a.published) > 0 {
		publish := a.published[0]
		a.published = a.published[1:]
		publish()
	}
	a.building = true
	defer func() { a.building = false }()
	a.themed(c)
	c.OnShortcut(ui.Cmd, ui.Key1, func() { a.showHistory = !a.showHistory })
	c.OnShortcut(ui.Cmd, ui.Key2, func() { a.showTree = !a.showTree })
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyE, func() { a.setMode(modeChanges) })
	c.OnShortcut(ui.Cmd|ui.Shift, ui.KeyF, func() { a.setMode(modeFiles) })

	// The toolbar stays as it is whichever columns show, so its controls
	// never move. The tree and the text keep their places whichever mode
	// shows, the history at the end, as an inspector. The toolbar changes
	// the state in actions, after the pass, so that all of the pass sees
	// the same.
	history := a.showHistory
	content := func() {
		if a.mode == modeFiles {
			a.codePane(c)
		} else {
			a.diffPane(c)
		}
	}
	rest := func() {
		if a.showTree {
			ui.Split(c, &a.treeWidth, func() { a.treePane(c) }, content).Fill()
		} else {
			content()
		}
	}
	ui.Column(c).Fill().Children(func() {
		a.toolbar(c)
		ui.Divider(c)
		ui.Row(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			if history {
				endSplit(c, &a.historyWidth, func() { a.history(c) }, rest).Fill()
			} else {
				rest()
			}
		})
	})
	// The material shows through the root, cleared after the build:
	// SetTheme, which the lists call for their focus, makes it opaque.
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	a.sync(c)
	if a.redraw {
		a.redraw = false
		c.Services().Invalidate()
	}
}

// endSplit is Split with end at the end, *size its width, as a window's
// inspector keeps its width while the window resizes: a Split laid out
// the other way round, its panes the window's way.
func endSplit(c *ui.Context, size *float32, end, rest func()) ui.Element {
	dir, flipped := c.Direction(), ui.RightToLeft
	if dir == ui.RightToLeft {
		flipped = ui.LeftToRight
	}
	pane := func(build func()) func() {
		return func() { ui.Column(c).Fill().Direction(dir).Children(build) }
	}
	// The direction is the Split's before it builds its divider, whose
	// drags follow it.
	return ui.Row(c).Direction(flipped).Children(func() {
		ui.Split(c, size, pane(end), pane(rest)).Fill()
	})
}

// toolbar is the window's title bar: the tree's toggle beside the traffic
// lights, then the repository, the mode in the middle, and the version
// shown and the history's toggle at the end, over their columns.
func (a *app) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	row := ui.Row(c).Height(titleBarHeight).Shrink(0).FillWidth().AlignItems(ui.Center).
		Gap(t.Space(2)).Padding(0, bar.Right+t.Space(3), 0, bar.Left+t.Space(3)).DragWindow()
	if !c.Vibrancy() {
		row.Background(t.Surface)
	}
	row.Children(func() {
		a.toggle(c, "tree", filesIcon, &a.showTree, true, "File Tree (⌘2)")
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
			a.projectTitle(c)
			if a.github != "" {
				a.linkButton(c, "github", githubIcon, "Open on GitHub", a.github)
			}
		})
		a.modeSwitch(c)
		ui.Row(c).Grow(1).Basis(0).MinWidth(0).Justify(ui.End).AlignItems(ui.Center).Children(func() {
			if cm, ok := a.current(); ok {
				label, mono := "Working Tree", false
				if cm.Hash != worktree {
					label, mono = cm.Short, true
				}
				chip := ui.Text(c, label).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().
					Padding(2, t.Space(2)).Radius(999).Background(t.Text.Alpha(0.07)).Tooltip("The version shown")
				if mono {
					chip.Font(monoFamily)
				}
			}
		})
		a.toggle(c, "history", historyIcon, &a.showHistory, true, "History (⌘1)")
	})
}

// modeSwitch chooses between the changes and the files, as the segmented
// controls of macOS toolbars: a recessed track and a raised segment.
func (a *app) modeSwitch(c *ui.Context) {
	t := c.Theme()
	a.modeChoice = a.mode
	seg := ui.SegmentedBase(c.Key("mode"), &a.modeChoice, 2)
	seg.Track.Shrink(0).Padding(2).Gap(2).Radius(9).Background(t.Text.Alpha(0.06)).Label("Show").
		OnChange(func() { a.setMode(a.modeChoice) })
	seg.Track.Children(func() {
		for i, svg := range []*ui.SVG{changesIcon, browseIcon} {
			label := []string{"Changes", "Files"}[i]
			chosen := i == a.mode
			s := seg.Segment(i).Height(26).Padding(0, t.Space(3)).Radius(7).Gap(t.Space(1.5)).
				AlignItems(ui.Center).TextColor(t.TextMuted).Label(label)
			switch {
			case chosen && t.Dark:
				s.Background(t.Text.Alpha(0.16)).TextColor(t.Text)
			case chosen:
				s.Background(t.Background).TextColor(t.Text).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.12))
			case s.Hovered():
				s.TextColor(t.Text)
			}
			s.Children(func() {
				ui.Icon(c, svg).Size(15, 15)
				ui.Text(c, label).FontSize(smallSize + 1).FontWeight(500).SingleLine()
				if n := len(a.changes); i == modeChanges && n > 0 {
					ui.Textf(c, "%d", n).FontSize(smallSize-1).FontWeight(600).TextColor(t.TextMuted).
						Padding(0, 6).Radius(999).Background(t.Text.Alpha(0.08))
				}
			})
		}
	})
}

// projectTitle is the title and subtitle of the window, as Xcode's
// toolbar shows them, which a click on switches the project.
func (a *app) projectTitle(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("project")).Bind(&a.titleButton).Shrink(1).MinWidth(0).
		Padding(2, t.Space(2)).Radius(7).Label("Switch Project").Tooltip(a.repo)
	switch {
	case b.Pressed():
		b.Background(t.Text.Alpha(0.12))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.07))
	}
	b.OnClick(func() {
		if a.projects != nil {
			r := a.titleButton.Bounds(c)
			a.projects.switcher(r.X, r.Y+r.H+4)
		}
	})
	b.Children(func() {
		ui.Column(c).MinWidth(0).Children(func() {
			ui.Row(c).MinWidth(0).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
				ui.Text(c, path.Base(a.repo)).FontWeight(700).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
				ui.Icon(c, chevronIcon).Size(11, 11).Shrink(0).TextColor(t.TextMuted)
			})
			if a.branch != "" {
				ui.Text(c, a.branch).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
			}
		})
	})
}

// linkButton opens the page in the browser, its icon alone.
func (a *app) linkButton(c *ui.Context, key string, svg *ui.SVG, label, url string) {
	t := c.Theme()
	services := c.Services()
	b := ui.ButtonBase(c.Key(key)).Size(28, 28).Shrink(0).Radius(7).Center().
		TextColor(t.TextMuted).Label(label).Tooltip(label + " — " + strings.TrimPrefix(url, "https://"))
	switch {
	case b.Pressed():
		b.Background(t.Text.Alpha(0.12)).TextColor(t.Text)
	case b.Hovered():
		b.Background(t.Text.Alpha(0.07)).TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, svg).Size(16, 16) }).OnClick(func() { services.OpenURL(url) })
}

// toggle shows or hides a column, its icon in the text's colour while the
// column shows, as the toolbar buttons of macOS are.
func (a *app) toggle(c *ui.Context, key string, svg *ui.SVG, on *bool, enabled bool, label string) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Size(32, 28).Shrink(0).Radius(7).Center().
		TextColor(t.TextMuted).Label(label).Tooltip(label).Disabled(!enabled)
	switch {
	case !enabled:
		b.Opacity(0.3)
	case b.Pressed():
		b.Background(t.Text.Alpha(0.12))
	case b.Hovered():
		b.Background(t.Text.Alpha(0.07))
	}
	if enabled && *on {
		b.TextColor(t.Text)
	}
	b.Children(func() { ui.Icon(c, svg).Size(17, 17) }).OnClick(func() { *on = !*on })
}

// column builds one of the window's columns, all alike: the background,
// only the toolbar showing the material, a slim header, a divider, and
// the body.
func column(c *ui.Context, header, body func()) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Height(38).Shrink(0).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).
			Padding(0, t.Space(3)).Children(header)
		ui.Divider(c)
		body()
	})
}

// sideList insets a list of a side column, the history or a tree, so
// that its rows' highlights clear the divider and the column's edges.
func sideList(c *ui.Context, list ui.Element) ui.Element {
	t := c.Theme()
	return list.Grow(1).FillWidth().Padding(t.Space(1.5), t.Space(2))
}

// ink is the colors of a row of a side list: the accent's text where it
// is chosen, which shows on the accent where the list has the focus.
type ink struct {
	text, muted ui.Color
	onAccent    bool // chosen in the list with the focus
}

func rowInk(t *ui.Theme, selected, focused bool) ink {
	if !selected {
		return ink{text: t.Text, muted: t.TextMuted}
	}
	return ink{text: t.AccentText, muted: t.AccentText.Alpha(0.72), onAccent: focused}
}

func sectionTitle(c *ui.Context, title string) {
	ui.Text(c, title).FontSize(smallSize).FontWeight(600).TextColor(c.Theme().TextMuted).SingleLine()
}

func (a *app) history(c *ui.Context) {
	t := c.Theme()
	header := func() {
		sectionTitle(c, "History")
		ui.Spacer(c)
		n := len(a.commits)
		if n > 0 && a.commits[0].Hash == worktree {
			n--
		}
		ui.Text(c, count(n, "commit")).FontSize(smallSize).TextColor(t.TextMuted).SingleLine()
	}
	column(c, header, func() {
		switch {
		case a.err != "":
			emptyNote(c, a.err)
			return
		case len(a.commits) == 0:
			emptyNote(c, "Loading…")
			return
		}
		a.commitList.Selected = &a.commitRow
		a.commitList.Key = func(i int) any { return a.commits[i].Hash } // "" the work tree
		a.commitList.Label = func(i int) string { return a.commits[i].Subject }
		a.withFocus(c, a.commitList.FocusWithin(c), func() {
			list := ui.List(c.Key("history"), &a.commitList, len(a.commits)).Gap(1).Label("History")
			sideList(c, list).Rows(func(row ui.ListRow) { a.commitItem(row) })
		})
	})
}

func (a *app) commitItem(row ui.ListRow) {
	c := row.Context
	t := c.Theme()
	cm := a.commits[row.Index]
	in := rowInk(t, row.Selected(), row.ListFocused())
	text, muted := in.text, in.muted
	ui.Row(c).FillWidth().Gap(t.Space(2.5)).Padding(t.Space(2), t.Space(2)).AlignItems(ui.Start).Children(func() {
		if cm.Hash == worktree {
			orange := system(t, systemOrange)
			badge := ui.Box(c).Size(30, 30).Shrink(0).Radius(999).Center().
				Background(orange.Alpha(0.16)).TextColor(orange)
			if in.onAccent {
				badge.Background(t.AccentText.Alpha(0.2)).TextColor(t.AccentText)
			}
			badge.Children(func() { ui.Icon(c, worktreeIcon).Size(15, 15) })
			ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
				ui.Text(c, cm.Subject).FontWeight(600).TextColor(text).SingleLine()
				ui.Text(c, cm.Summary).FontSize(smallSize).TextColor(muted).SingleLine()
			})
			return
		}
		ui.Avatar(c, cm.Author, a.avatar(cm.Email)).Size(30, 30).Shrink(0)
		// As Mail lists messages: who and when on top, then what.
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, cm.Author).FontWeight(600).TextColor(text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				ui.Text(c, shortDate(cm.Time, time.Now())).FontSize(smallSize).TextColor(muted).SingleLine().Shrink(0).
					Tooltip(cm.Time.Format("Monday, January 2, 2006 at 15:04"))
			})
			ui.Text(c, cm.Subject).TextColor(text).SingleLine().Ellipsis("…")
			ui.Text(c, cm.Short).FontSize(smallSize).Font(monoFamily).TextColor(muted).SingleLine()
		})
	})
}

func (a *app) treePane(c *ui.Context) {
	title, key, tree, label := "Changed Files", "changes", &a.changeTree, "Changes"
	if a.mode == modeFiles {
		title, key, tree, label = "Files", "all", &a.allTree, "Files"
	}
	column(c, func() { sectionTitle(c, title) }, func() {
		if tree.dirs == nil {
			emptyNote(c, "Loading…")
			return
		}
		a.treeView(c.Key(key), tree, label)
	})
}

func (a *app) treeView(c *ui.Context, tree *pathTree, label string) {
	focused := tree.state.List.FocusWithin(c)
	a.withFocus(c, focused, func() {
		sideList(c, tree.view(c, label, func(p string, selected bool) {
			a.treeItem(c, tree, p, selected, focused)
		}))
	})
}

func (a *app) treeItem(c *ui.Context, tree *pathTree, p string, selected, focused bool) {
	t := c.Theme()
	in := rowInk(t, selected, focused)
	text, muted := in.text, in.muted
	isFile := !tree.isDir(p)
	ch, changed := a.changes[p]
	if isFile && tree == &a.changeTree && a.readFile(p) && !selected {
		text = muted // read, as the diffs fold it away
	}
	row := ui.Row(c).FillWidth().Gap(t.Space(1.5)).PaddingY(1).AlignItems(ui.Center)
	if !isFile {
		// A click on a folder opens or closes it as its arrow does, which
		// sync makes choose it too, as Finder's and Xcode's rows do. It
		// acts once the outline is built, as it changes the outline's rows.
		row.KeepFocus().OnClick(func() { tree.flip(p) })
	}
	if tree == &a.changeTree {
		row.ContextMenu(func(m *ui.Menu) { a.readMenu(m, p) })
	}
	if isGroup(p) {
		row.Children(func() {
			ui.Text(c, tree.labels[p]).FontSize(smallSize).FontWeight(600).TextColor(muted).SingleLine().Grow(1)
			// How many of its files are read, as their diffs fold.
			diffs := a.diffsUnder(p)
			ui.Textf(c, "%d/%d", countRead(diffs), len(diffs)).FontSize(smallSize).TextColor(muted).Shrink(0).Padding(0, t.Space(1))
		})
		return
	}
	row.Children(func() {
		if isFile {
			var tint ui.Color
			if in.onAccent {
				tint = t.AccentText
			}
			fileicons.View(c, p, 16, muted, tint)
		} else {
			color := a.theme.Accent // folders in the accent, as Finder's sidebar
			if in.onAccent {
				color = muted
			}
			ui.Icon(c, folderIcon).Size(15, 15).Shrink(0).TextColor(color)
		}
		ui.Text(c, tree.labels[p]).TextColor(text).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Tooltip(itemPath(p))
		if isFile && changed {
			statusMark(c, ch.Status, in.onAccent)
		}
	})
}

// statusMark is the letter of a change, as Xcode's navigator shows it.
func statusMark(c *ui.Context, status string, onAccent bool) {
	t := c.Theme()
	color := statusColor(t, status)
	if onAccent {
		color = t.AccentText
	}
	ui.Text(c, status).FontSize(smallSize).FontWeight(600).TextColor(color).
		TextAlign(ui.Center).Width(16).Shrink(0)
}

// pathTitle shows a path in a column's header: its directory muted, its
// name bold.
func pathTitle(c *ui.Context, p string) {
	t := c.Theme()
	dir, name := path.Split(p)
	ui.Row(c).Grow(1).MinWidth(0).Children(func() {
		ui.Text(c, dir).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		ui.Text(c, name).FontWeight(600).SingleLine().Shrink(0)
	})
}

func emptyNote(c *ui.Context, note string) {
	ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
		ui.Text(c, note).TextColor(c.Theme().TextMuted)
	})
}

func (a *app) codePane(c *ui.Context) {
	t := c.Theme()
	header := func() {
		if a.openFile == "" {
			return
		}
		pathTitle(c, a.openFile)
		ui.Text(c, "Read only").FontSize(smallSize).TextColor(t.TextMuted).SingleLine()
	}
	column(c, header, func() {
		switch {
		case a.codeNote != "":
			emptyNote(c, a.codeNote)
		case a.code == nil:
			emptyNote(c, "No file selected")
		default:
			editor.View(c.Key("code:"+a.openFile), a.code).Grow(1).FillWidth()
		}
	})
}

// sync starts loading what the choices in the history, the trees and the
// mode ask for, once for each new choice.
func (a *app) sync(c *ui.Context) {
	if a.changeTree.sync() || a.allTree.sync() {
		c.Services().Invalidate()
	}
	// A folder opened or closed by a click, on its arrow or its row,
	// is chosen too, and the tree takes the keys.
	for _, tree := range []*pathTree{&a.changeTree, &a.allTree} {
		if dir := tree.arrowed(); dir != "" {
			if i := tree.rowOf(dir); i >= 0 {
				tree.row = i
			}
			tree.state.List.Handle.Focus(c)
			c.Services().Invalidate()
		}
	}
	if a.commitRow >= 0 && a.commitRow < len(a.commits) && a.commitRow != a.loadedRow {
		a.loadedRow = a.commitRow
		a.loadCommit(a.commits[a.commitRow].Hash)
	}
	cm, ok := a.current()
	if !ok {
		return
	}
	switch a.mode {
	case modeChanges:
		if p := a.changeTree.chosen(); p != "" && !a.changeTree.isDir(p) && p != a.file && a.commitRow == a.loadedRow {
			a.file = p
			a.scrollToFile(p)
		} else {
			a.followScroll(c)
		}
	case modeFiles:
		if a.filesFor != a.loadedRow {
			a.filesFor = a.loadedRow
			a.loadFiles(cm.Hash)
		}
		if p := a.allTree.chosen(); p != "" && !a.allTree.isDir(p) && p != a.openFile {
			a.openFile = p
			a.loadText(cm.Hash, p)
		}
	}
}

// current returns the commit whose changes are loaded, or loading.
func (a *app) current() (commit, bool) {
	if a.loadedRow < 0 || a.loadedRow >= len(a.commits) {
		return commit{}, false
	}
	return a.commits[a.loadedRow], true
}

// setMode switches the mode, showing in it the file the other showed.
func (a *app) setMode(mode int) {
	if mode == a.mode {
		return
	}
	a.mode, a.modeChoice = mode, mode
	cm, ok := a.current()
	if !ok {
		return
	}
	switch mode {
	case modeFiles:
		if a.filesFor == a.loadedRow && a.file != a.openFile {
			a.openInFiles(cm.Hash, a.file)
		}
	case modeChanges:
		if _, ok := a.changes[a.openFile]; ok && a.openFile != a.file {
			a.changeTree.show(a.openFile) // which the diffs scroll to
		}
	}
}

// openInFiles shows the file in the files mode where the commit has it.
func (a *app) openInFiles(hash, p string) bool {
	if p == "" || !a.allTree.has(p) {
		return false
	}
	a.allTree.show(p)
	a.openFile = p
	a.loadText(hash, p)
	return true
}

// background runs work off the UI thread, then the function it returns
// on it, to publish the result.
func (a *app) background(work func() func()) {
	if a.inline {
		if publish := work(); a.building {
			a.published = append(a.published, publish)
		} else {
			publish()
		}
		a.redraw = true
		return
	}
	go func() { a.win.Update(work()) }()
}

func (a *app) loadCommits() {
	a.background(func() func() {
		h := readHistory(a.repo)
		return func() {
			if h.err != nil {
				a.err = h.err.Error()
				return
			}
			a.commits, a.worktreeChanges, a.branch, a.github = h.commits, h.changes, h.branch, h.github
			if len(h.commits) > 0 {
				a.commitRow = 0
			}
		}
	})
}

func (a *app) loadCommit(hash string) {
	seeded := a.worktreeChanges
	a.worktreeChanges = nil
	a.background(func() func() {
		changes, err := seeded, error(nil)
		if hash != worktree || changes == nil {
			changes, err = loadChanges(a.repo, hash)
		}
		paths := make([]string, len(changes))
		for i, ch := range changes {
			paths[i] = ch.Path
		}
		tree := foldGrouped(paths)
		return func() {
			if cm, ok := a.current(); !ok || cm.Hash != hash {
				return // another commit was chosen meanwhile
			}
			if err != nil {
				a.err = err.Error()
				return
			}
			a.changes = map[string]change{}
			for _, ch := range changes {
				a.changes[ch.Path] = ch
			}
			a.changeTree.set(tree)
			a.openChanges()
			a.resetDiffs()
			a.file = ""
			if len(a.diffs) > 0 {
				a.file = a.diffs[0].change.Path
				a.changeTree.show(a.file)
			}
		}
	})
}

// loadFiles lists the files of the commit for the files mode, keeping the
// file open where the commit has it too.
func (a *app) loadFiles(hash string) {
	a.allTree.dirs = nil
	a.background(func() func() {
		files, err := listFiles(a.repo, hash)
		tree := fold(files)
		return func() {
			if cm, ok := a.current(); !ok || cm.Hash != hash || a.filesFor != a.loadedRow {
				return
			}
			if err != nil {
				a.err = err.Error()
				return
			}
			a.allTree.set(tree)
			keep := a.openFile
			if keep == "" {
				keep = a.file
			}
			a.openFile, a.code, a.codeNote = "", nil, ""
			a.openInFiles(hash, keep)
		}
	})
}

func (a *app) loadText(hash, p string) {
	a.background(func() func() {
		text, err := fileText(a.repo, hash, p)
		note := textNote(text)
		if err != nil {
			note = "Could not read this file"
		}
		var code *editor.State
		if note == "" {
			code = &editor.State{Buffer: codeBuffer(p, text), ReadOnly: true, SoftWrap: true, Font: codeFont, Theme: codeTheme}
		}
		return func() {
			if p == a.openFile {
				a.code, a.codeNote = code, note
			}
		}
	})
}

// openApp returns the app of the repository, with the authors' pictures.
func openApp(repo string) *app {
	a := newApp(repo)
	a.avatars = newAvatars()
	return a
}

func main() {
	dir := "."
	if len(os.Args) > 1 {
		dir = os.Args[1]
	}
	// Opened from Finder, the app starts outside any repository, and
	// opens the last one instead, or asks for one.
	repo, err := repoRoot(dir)
	if err != nil && len(os.Args) > 1 {
		log.Fatal(err)
	}
	p := newProjects()
	fileicons.Warm()
	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleFileMenu, Submenu: []*mygo.MenuItem{
				{Label: "Open Folder…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() }},
				mygo.Separator(),
				{Role: mygo.RoleClose},
			}},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		p.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "peek",
			Width:     1440,
			Height:    900,
			MinWidth:  640,
			MinHeight: 400,
			// Opens where the user left it last time.
			StateKey:      "main",
			TitleBarStyle: mygo.TitleBarHiddenInset,
			Vibrancy:      mygo.VibrancySidebar,
			Content:       ui.View(p.view),
		})
		switch {
		case err == nil:
			p.open(repo)
		case len(p.recent) > 0:
			p.open(p.recent[0])
		default:
			p.choose()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
