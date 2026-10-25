// Package app is Peek's window: the history of a repository, the files
// of the chosen commit, and their diffs or texts.
package app

import (
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"peek/internal/fileicons"
	"peek/internal/git"
	"peek/internal/review"

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
	push                     pushState
	canPush                  bool   // work tree changes or commits not yet pushed
	behind                   int    // the upstream's commits, as fetched, the branch has not
	upstream                 string // the upstream's name, where the branch is behind it
	pull                     pullState
	review                   reviewState
	reviewSource             review.Snapshot
	reviewSourceErr          string
	switcher                 switcherState
	// theme is this pass's, listTheme the one of lists with the keyboard
	// focus and inactiveTheme the one of lists without it.
	theme, listTheme, inactiveTheme *ui.Theme
	titleButton                     ui.Handle // the project's name, under which the projects list

	showHistory, showTree   bool
	historyWidth, treeWidth float32
	mode                    int
	modeChoice              int // the mode switch's, which setMode follows

	commits    []git.Commit
	commitList ui.ListState
	commitRow  int
	loadedRow  int // the commit whose changes are loaded, or loading
	// worktreeChanges are the work tree's, read with the history, for
	// its entry's first load.
	worktreeChanges []git.Change
	// refreshing is while the history is read again, and refreshAgain
	// that files changed meanwhile.
	refreshing, refreshAgain bool

	// Changes mode.
	changes    map[string]git.Change // by new path
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
	picture  ui.ImageSource // the file's picture, in place of code
	codeNote string         // why the text does not show
	preview  *mdPreview     // a Markdown file's, beside its code
	// previewed is the Markdown files shown as their previews, their
	// sources showing by default.
	previewed map[string]bool
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

// The title bar is a compact toolbar's, one row tall, the traffic lights
// as far from its top as from its left.
const titleBarHeight = 38

var trafficLightsAt = mygo.Point{X: 13, Y: 12}

// The sizes of text, the system's: 13 for the body, 11 for captions and
// 12 for code, as Xcode's, which the user scales in their preferences.
const (
	fontSize  = 13
	smallSize = 11 // secondary text
	codeSize  = 12
)

// themed sets this pass's theme: the system's, its text at the app's size
// as the user scales it, and the theme of lists without the focus.
func (a *app) themed(c *ui.Context) {
	t := c.Theme() // a copy for this pass
	t.FontSize = fontSize * c.Preferences().TextScale
	c.SetTheme(t)
	// Rows hover fainter than the gray of a choice without the focus, so
	// the row under the pointer never passes for the chosen one.
	list := *t
	list.SurfaceHover = t.Text.Alpha(0.04)
	if t.Dark {
		list.SurfaceHover = t.Text.Alpha(0.06)
	}
	inactive := list
	inactive.Accent = t.Text.Alpha(0.12)
	if t.Dark {
		inactive.Accent = t.Text.Alpha(0.17)
	}
	inactive.AccentText = t.Text
	a.theme, a.listTheme, a.inactiveTheme = t, &list, &inactive
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
	// The shortcuts are the View menu's (viewMenu), as the menu bar holds
	// every command of a Mac app.

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
	a.branchSwitcher(c)
	a.reviewDetails(c)
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

// toolbar is the window's title bar, a toolbar as macOS draws one: the
// sidebar's button beside the traffic lights, then the project, the mode
// in the middle, and the history's button at the end, over its column.
// Its controls take no face until hovered, Tab stops on it once and the
// arrows move among them.
func (a *app) toolbar(c *ui.Context) {
	t := c.Theme()
	bar := c.TitleBar()
	ui.Row(c).Height(titleBarHeight).Shrink(0).FillWidth().AlignItems(ui.Center).
		Padding(0, bar.Right+t.Space(2), 0, bar.Left+t.Space(2)).DragWindow().
		Background(t.Surface).Children(func() {
		ui.Toolbar(c, func() {
			a.paneButton(c, "sidebar", sidebarIcon, &a.showTree, "Sidebar")
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
				a.projectTitle(c)
				a.branchLabel(c)
				a.pullButton(c)
				if a.github != "" {
					a.linkButton(c, "github", githubIcon, "Open on GitHub", a.github)
				}
			})
			a.modeChoice = a.mode
			modeSwitch(c.Key("mode"), &a.modeChoice, "Changes", "Files").Label("Mode").
				OnChange(func() { a.setMode(a.modeChoice) })
			// Keep both sides equally wide as the action labels change.
			ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).
				Justify(ui.End).Gap(t.Space(1)).Children(func() {
				a.reviewButton(c)
				a.pushButton(c)
			})
			a.paneButton(c, "history", historyIcon, &a.showHistory, "History")
		}).Grow(1).MinWidth(0).Padding(0).Label("Toolbar")
	})
}

// modeSwitch is a segmented control as macOS toolbars draw it: a filled
// track without a border, 24 points tall, of segments as wide as one
// another, the chosen one raised from it.
func modeSwitch(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	const pad, inset = 10, 2
	var widest float32
	for _, label := range labels {
		w, _ := c.MeasureText(0, ui.Span{Text: label, Size: t.FontSize})
		widest = max(widest, w)
	}
	track, chosen, hover := ui.RGBA(0, 0, 0, 0.06), ui.Hex("#ffffff"), ui.RGBA(0, 0, 0, 0.04)
	if t.Dark {
		track, chosen, hover = ui.RGBA(255, 255, 255, 0.1), ui.RGBA(255, 255, 255, 0.24), ui.RGBA(255, 255, 255, 0.06)
	}
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Height(24).AlignItems(ui.Stretch).Padding(inset).Gap(inset).Radius(7).Background(track)
	parts.Track.Children(func() {
		for i, label := range labels {
			s := parts.Segment(i).Width(widest + 2*pad).Radius(5).TextColor(t.Text)
			switch {
			case i == *selected:
				s.Background(chosen).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
			case s.Hovered():
				s.Background(hover)
			}
			s.Children(func() { ui.Text(c, label).FontSize(t.FontSize).SingleLine() })
		}
	})
	return parts.Track
}

// paneButton shows or hides a column, as the sidebar buttons of macOS
// toolbars: an icon alone, named for what a click does.
func (a *app) paneButton(c *ui.Context, key string, svg *ui.SVG, on *bool, pane string) {
	action := showOrHide(*on, pane)
	ui.Button(c.Key(key), "").Shrink(0).Label(action).Tooltip(action).
		Children(func() { ui.Icon(c, svg).Size(16, 16) }).OnClick(func() { *on = !*on })
}

// projectTitle is the title of the window, the project's name, which a
// click on switches the project.
func (a *app) projectTitle(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("project")).Bind(&a.titleButton).Shrink(1).MinWidth(0).
		Padding(t.Space(0.5), t.Space(2)).Radius(t.Radius).Label("Switch Project").Tooltip(a.repo)
	flat(b, t, false)
	b.OnClick(func() {
		if a.projects != nil {
			r := a.titleButton.Bounds(c)
			a.projects.switcher(r.X, r.Y+r.H+4)
		}
	})
	b.Children(func() {
		ui.Row(c).MinWidth(0).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Text(c, path.Base(a.repo)).FontWeight(700).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			ui.Icon(c, chevronIcon).Size(9, 9).Shrink(0).TextColor(t.TextMuted)
		})
	})
}

// linkButton opens the page in the browser, its icon alone.
func (a *app) linkButton(c *ui.Context, key string, svg *ui.SVG, label, url string) {
	services := c.Services()
	ui.Button(c.Key(key), "").Shrink(0).Label(label).Tooltip(label + " — " + strings.TrimPrefix(url, "https://")).
		Children(func() { ui.Icon(c, svg).Size(16, 16) }).OnClick(func() { services.OpenURL(url) })
}

// column builds one of the window's columns, all alike: the background,
// a slim header with the title and, muted at the end, a detail such as a
// count, a divider, and the body.
func column(c *ui.Context, title, detail string, body func()) {
	columnWith(c, title, mutedDetail(c, detail), body)
}

// mutedDetail builds the detail of a column's header, muted.
func mutedDetail(c *ui.Context, detail string) func() {
	return func() {
		if detail != "" {
			ui.Text(c, detail).FontSize(smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
		}
	}
}

// columnWith is a column whose header ends with what detail builds.
func columnWith(c *ui.Context, title string, detail, body func()) {
	columnLinked(c, title, "", detail, body)
}

// columnLinked is a column whose title links its references to pull
// requests and issues, as #123, to the GitHub repository at github, where
// there is one.
func columnLinked(c *ui.Context, title, github string, detail, body func()) {
	t := c.Theme()
	ui.Column(c).Fill().Background(t.Background).Children(func() {
		ui.Row(c).Height(30).Shrink(0).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).
			Padding(0, t.Space(3)).Children(func() {
			ui.RichText(c).FontWeight(600).SingleLine().Ellipsis("…").
				Grow(1).MinWidth(0).Tooltip(title).Children(func() {
				linkedText(c, title, github)
			})
			detail()
		})
		ui.Divider(c)
		body()
	})
}

// issueRef finds a reference to a pull request or an issue, #123, not
// within a word or an entity such as &#123;.
var issueRef = regexp.MustCompile(`(^|[^\w&/])#([0-9]+)\b`)

// linkedText builds text, its references to pull requests and issues as
// links to the GitHub repository at github, where there is one.
func linkedText(c *ui.Context, text, github string) {
	if github == "" {
		ui.Text(c, text)
		return
	}
	for _, part := range splitRefs(text) {
		if num, ok := strings.CutPrefix(part, "#"); ok && issueRef.MatchString(" "+part) {
			ui.Link(c, part, github+"/issues/"+num)
		} else {
			ui.Text(c, part)
		}
	}
}

// splitRefs cuts text at its references to pull requests and issues, each
// a part of its own.
func splitRefs(text string) []string {
	var parts []string
	at := 0
	for _, m := range issueRef.FindAllStringSubmatchIndex(text, -1) {
		if start := m[4] - 1; start > at {
			parts = append(parts, text[at:start])
		}
		parts, at = append(parts, text[m[4]-1:m[5]]), m[5]
	}
	if at < len(text) {
		parts = append(parts, text[at:])
	}
	return parts
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

func (a *app) history(c *ui.Context) {
	n := len(a.commits)
	if n > 0 && a.commits[0].Hash == git.Worktree {
		n--
	}
	column(c, "History", count(n, "commit"), func() {
		switch {
		case a.err != "":
			emptyNote(c, a.err)
			return
		case len(a.commits) == 0:
			loading(c)
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
	item := ui.Row(c).FillWidth().Gap(t.Space(2.5)).Padding(t.Space(2), t.Space(2)).AlignItems(ui.Start).Children(func() {
		if cm.Hash == git.Worktree {
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
		// The first co-author over the author's corner, as GitHub
		// stacks them.
		ui.Box(c).Size(30, 30).Shrink(0).Children(func() {
			a.personAvatar(c, cm.Author, cm.Email, 30)
			if len(cm.CoAuthors) > 0 {
				co := cm.CoAuthors[0]
				// A ring of the row's own color parts the two.
				ring := t.Background
				if row.Selected() {
					ring = t.Accent
				}
				ui.Box(c).Size(19, 19).Radius(9.5).Center().Background(ring).
					Absolute().Right(-4).Bottom(-4).Children(func() {
					a.personAvatar(c, co.Name, co.Email, 16)
				})
			}
		})
		// What first, across the whole line, then when and who under it.
		// The hash is in the details.
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, cm.Subject).FontWeight(600).TextColor(text).SingleLine().Ellipsis("…").FillWidth()
			ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, shortDate(cm.Time, time.Now())).FontSize(smallSize).TextColor(muted).SingleLine().Shrink(0)
				ui.Text(c, "·").FontSize(smallSize).TextColor(muted).Shrink(0)
				ui.Text(c, authors(cm)).FontSize(smallSize).TextColor(muted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			})
		})
	})
	if cm.Hash != git.Worktree {
		a.commitDetails(c, item, cm)
	}
}

// commitDetails shows more of the commit as the pointer rests on its row:
// who and when in full, the whole hash and the message. The card
// goes beside the row, toward the window's middle, as the history is at
// its end.
func (a *app) commitDetails(c *ui.Context, item ui.Element, cm git.Commit) {
	t := c.Theme()
	ui.TooltipBase(c, item, func(tip ui.Element) {
		tip.AttachTo(item, ui.AnchorTopStart, ui.AnchorTopEnd).MarginEnd(t.Space(1.5))
		tip.Width(t.Space(80)).Padding(t.Space(3)).Radius(t.Space(2)).
			Background(t.Background).Border(1, t.Border).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.18))
		tip.Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(2)).Children(func() {
				// The pointer goes through the card, so nothing in it can
				// scroll: the message is cut to lines as wrapped, after who
				// and when, which stay in view.
				ui.Text(c, cm.Subject).FontWeight(600).TextColor(t.Text).MaxLines(3)
				ui.Column(c).FillWidth().Gap(t.Space(1.5)).Children(func() {
					a.personLine(c, git.Person{Name: cm.Author, Email: cm.Email}, "")
					for _, co := range cm.CoAuthors {
						a.personLine(c, co, "Co-author")
					}
				})
				ui.Column(c).FillWidth().Gap(t.Space(0.5)).Children(func() {
					ui.Text(c, cm.Time.Format("Monday, January 2, 2006 at 15:04")).FontSize(smallSize).TextColor(t.TextMuted)
					ui.Text(c, cm.Hash).FontSize(smallSize).Font(monoFamily).TextColor(t.TextMuted).SingleLine()
				})
				if cm.Body != "" {
					ui.Text(c, cm.Body).FontSize(smallSize).TextColor(t.Text).MaxLines(10)
				}
			})
		})
	})
}

// personAvatar is the picture of someone, size DIPs across. Avatar lays
// out its picture and initials for its own size, not this one: the
// picture is cut to the circle here, and the initials scaled to it.
func (a *app) personAvatar(c *ui.Context, name, email string, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Role(ui.RoleImage).Label(name)
	if bm := a.avatar(email); bm != nil {
		return box.Clip().Children(func() {
			ui.Image(c, bm).Fit(ui.Cover).Size(size, size)
		})
	}
	// Avatar at its own size, centered on the box out of its layout,
	// and scaled to it about its middle.
	own := c.Theme().FontSize * 2.25
	k, off := size/own, (size-own)/2
	return box.Children(func() { ui.Avatar(c, name, nil).Absolute().Left(off).Top(off).Scale(k, k) })
}

// personLine is someone of a commit in its details: picture, then name,
// with their part where it is not the author's, over email.
func (a *app) personLine(c *ui.Context, p git.Person, role string) {
	t := c.Theme()
	ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		a.personAvatar(c, p.Name, p.Email, 26)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Baseline).Children(func() {
				ui.Text(c, p.Name).FontSize(smallSize).FontWeight(600).TextColor(t.Text).SingleLine().Ellipsis("…").MinWidth(0)
				if role != "" {
					ui.Text(c, role).FontSize(smallSize-1).TextColor(t.TextMuted).SingleLine().Shrink(0).
						Padding(0, t.Space(1)).Radius(t.Space(1)).Background(t.TextMuted.Alpha(0.12))
				}
			})
			ui.Text(c, p.Email).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…")
		})
	})
}

// authors names who made the commit: the author and a co-author, or the
// author and how many others.
func authors(cm git.Commit) string {
	switch len(cm.CoAuthors) {
	case 0:
		return cm.Author
	case 1:
		return cm.Author + " and " + cm.CoAuthors[0].Name
	}
	return cm.Author + " and " + count(len(cm.CoAuthors), "other")
}

func (a *app) treePane(c *ui.Context) {
	title, key, tree, label := "Changed Files", "changes", &a.changeTree, "Changes"
	if a.mode == modeFiles {
		title, key, tree, label = "Files", "all", &a.allTree, "Files"
	}
	// The lines the changes add and delete in all, as GitHub's pull
	// requests total them.
	detail := func() {
		if a.mode == modeChanges && len(a.diffs) > 0 {
			added, deleted := sumLines(a.diffs)
			lineCounts(c, added, deleted, false)
		}
	}
	columnWith(c, title, detail, func() {
		if tree.dirs == nil {
			loading(c)
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
			// The lines its files add and delete, and how many of them are
			// read, as their diffs fold.
			diffs := a.diffsUnder(p)
			added, deleted := sumLines(diffs)
			lineCounts(c, added, deleted, in.onAccent)
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
		} else if icon := fileicons.Folder(p, tree.state.Open.Has(p), t.Dark); icon != nil {
			ui.Image(c, icon).Size(16, 16).Shrink(0)
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

// loading is a column whose content loads, as AppKit's spinning
// indicator in the middle.
func loading(c *ui.Context) {
	ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
		ui.Spinner(c).Label("Loading")
	})
}

func emptyNote(c *ui.Context, note string) {
	ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
		ui.Text(c, note).TextColor(c.Theme().TextMuted)
	})
}

func (a *app) codePane(c *ui.Context) {
	detail := ""
	if a.picture != nil {
		detail = pictureSize(a.picture)
	}
	columnWith(c, a.openFile, func() {
		if detail != "" {
			ui.Text(c, detail).FontSize(smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
		}
		if a.preview != nil && a.codeNote == "" {
			a.previewButton(c)
		}
	}, func() {
		switch {
		case a.codeNote != "":
			emptyNote(c, a.codeNote)
		case a.picture != nil:
			ui.Column(c).Grow(1).FillWidth().Center().Padding(c.Theme().Space(4)).Children(func() {
				ui.Image(c, a.picture).Fit(ui.ScaleDown).MaxWidthPercent(100).MaxHeightPercent(100)
			})
		case a.code == nil:
			emptyNote(c, "No file selected")
		case a.previewing():
			previewView(c, a.preview)
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
		// A file chosen in the tree shows its diff, read or not.
		if p := a.changeTree.chosen(); p != "" && !a.changeTree.isDir(p) && p != a.file && a.commitRow == a.loadedRow {
			a.file = p
			if d := a.diffOf[p]; d != nil {
				d.opened = d.read
			}
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
func (a *app) current() (git.Commit, bool) {
	if a.loadedRow < 0 || a.loadedRow >= len(a.commits) {
		return git.Commit{}, false
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
			a.canPush, a.behind, a.upstream = h.canPush, h.behind, h.upstream
			a.updateReviewSource(h.review, h.reviewErr)
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
		if hash != git.Worktree || changes == nil {
			changes, err = git.LoadChanges(a.repo, hash)
		}
		byPath, paths := changesByPath(changes)
		tree := foldGrouped(paths)
		return func() {
			if cm, ok := a.current(); !ok || cm.Hash != hash {
				return // another commit was chosen meanwhile
			}
			if err != nil {
				a.err = err.Error()
				return
			}
			a.changes = byPath
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
// file open where the commit has it too. The files and the text of the
// commit shown before stay until the new ones come, not to flash empty.
func (a *app) loadFiles(hash string) {
	a.background(func() func() {
		files, err := git.ListFiles(a.repo, hash)
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
			if !a.openInFiles(hash, keep) {
				a.openFile, a.code, a.picture, a.codeNote, a.preview = "", nil, nil, "", nil
			}
		}
	})
}

func (a *app) loadText(hash, p string) {
	a.background(func() func() {
		text, err := git.FileText(a.repo, hash, p)
		picture, note := fileView(p, text, err)
		return func() {
			if p == a.openFile {
				var (
					code    *editor.State
					preview *mdPreview
				)
				if note == "" && picture == nil {
					code = newCode(p, text)
					if isMarkdown(p) {
						preview = a.newPreview(hash, p, text)
					}
				}
				a.code, a.picture, a.codeNote, a.preview = code, picture, note, preview
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

// extendPath adds where Homebrew and users install tools to the PATH, after
// what is there, for git to find what it runs, as git-lfs's filters and
// hooks, and for gh: a Finder-launched app has the system's PATH alone.
func extendPath() {
	home, _ := os.UserHomeDir()
	have := filepath.SplitList(os.Getenv("PATH"))
	dirs := have
	for _, d := range []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", filepath.Join(home, ".local", "bin")} {
		if !slices.Contains(have, d) && exists(d) {
			dirs = append(dirs, d)
		}
	}
	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

// Run opens the window over the repository the folder in args holds, or
// the one the working directory is in, and runs the app until it quits.
func Run(args []string) {
	extendPath()
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	// Opened from Finder, the app starts outside any repository, and
	// opens the last one instead, or asks for one.
	repo, err := git.RepoRoot(dir)
	if err != nil && len(args) > 0 {
		log.Fatal(err)
	}
	p := newProjects()
	fileicons.FollowVSCode()
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
			p.viewMenu(),
			{Role: mygo.RoleWindowMenu},
		}))
		p.win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Peek",
			Width:     1440,
			Height:    900,
			MinWidth:  640,
			MinHeight: 400,
			// Opens where the user left it last time.
			StateKey:             "main",
			TitleBarStyle:        mygo.TitleBarHidden,
			TrafficLightPosition: &trafficLightsAt,
			Content:              ui.View(p.view),
		})
		p.win.OnClosed(func() {
			for _, a := range p.apps {
				if a.push.cancel != nil {
					a.push.cancel()
				}
				if a.review.cancel != nil {
					a.review.cancel()
				}
			}
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
