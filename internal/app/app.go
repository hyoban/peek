// Package app is Peek's window: the history of a repository, the files
// of the chosen commit, and their diffs or texts.
package app

import (
	"fmt"
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
	unwatch  func()    // stops watching the project, as its window closes
	projects *projects // the windows', nil in tests
	avatars  *avatars  // nil shows initials alone
	repo     string
	branch   string
	github   string // the repository's page on GitHub, "" for none
	// branchURL is the branch's page on GitHub, which the button opens
	// over the repository's where there is one.
	branchURL string
	// pullURL is the page of the branch's pull request, which the button
	// opens over the branch's, as read for the branch's page pullURLFor.
	pullURL, pullURLFor string
	// inline runs loads on the UI thread, as tests and snapshots want,
	// and redraw asks for the frame a window's Update would draw then.
	// Those a pass starts wait in published for the next, as a window's
	// Update comes between passes, not within one.
	inline, redraw, building bool
	published                []func()
	err                      string
	push                     pushState
	canPush                  bool   // work tree changes or commits not yet pushed
	dirty                    int    // the files the work tree changes, as last read
	behind                   int    // the upstream's commits, as fetched, the branch has not
	upstream                 string // the upstream's name, where the branch is behind it
	pull                     pullState
	review                   reviewState
	answer                   answerState
	ask                      askState
	lastCodex                codexResult // which result came last
	// conversations are those with Codex about the repository, newest
	// first, kept in convFile; convRow is the one chosen, and navigator
	// which the sidebar lists, the files or the conversations.
	conversations []*conversation
	convList      ui.ListState
	convRow       int
	convFile      string
	navigator     int
	reply         string    // the request to follow up with
	replyField    ui.Handle // its field
	// reviewSource is what a review is of where the work tree or a commit
	// shows: the uncommitted changes, else the commits not pushed yet.
	reviewSource    review.Snapshot
	reviewSourceErr string
	// branchReview is what it is of where the branch's changes show.
	branchReview review.Snapshot
	switcher     switcherState
	// theme is this pass's, listTheme the one of lists with the keyboard
	// focus and inactiveTheme the one of lists without it.
	theme, listTheme, inactiveTheme *ui.Theme
	titleButton                     ui.Handle // the project's name, under which the projects list
	endTools                        ui.Handle // the controls at the end of the text's toolbar
	endWidth                        float32   // their width, as last laid out
	alert                           alertState
	// shot is the view the app is to open at, for screenshots, nil for
	// none or once it is shown.
	shot *shot
	// nativeBar tells that AppKit's toolbar shows this pass (nativeToolbar),
	// over the room the columns leave it.
	nativeBar bool
	// The windows of AppKit's own the sheets and the switcher show in.
	switcherPopover child

	showHistory, showTree   bool
	historyWidth, treeWidth float32
	// collapsedHistory and collapsedTree are the columns the window's
	// narrowing hid, which its widening shows again.
	collapsedHistory, collapsedTree bool
	// historyShown and treeShown are how far the columns show, from 0 to
	// 1, as they slide in and out (reveal).
	historyShown, treeShown float32
	fitWidth                float32 // the window's width fit last
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
	// landOnBranch is that a branch was just checked out, for the history
	// read for it to show the branch's changes.
	landOnBranch bool

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
	split                      bool                  // the diffs side by side
	change                     changeMark            // the change Next and Previous Change went to last
	comments                   map[string][]*comment // the user's comments on lines, by commit and path
	diffWidth                  float32               // the diffs' column's, for their annotations
	frames                     int                   // counts the frames, for what waits for the next

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
		ask:          askState{template: -1},
		convRow:      -1,
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
	unhovered(t)
	c.SetTheme(t)
	// Rows show nothing under the pointer, as AppKit's tables and outlines.
	list := *t
	list.SurfaceHover = ui.Transparent
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
	a.frames++
	a.themed(c)
	// The shortcuts are the menu bar's (viewMenu, repositoryMenu), as it
	// holds every command of a Mac app.

	// Each column has its part of the toolbar, which the dividers between
	// them run through, as AppKit splits a toolbar along a split view. The
	// tree and the text keep their places whichever mode shows, the
	// history at the end, as an inspector. The toolbar changes the state
	// in actions, after the pass, so that all of the pass sees the same.
	a.fit(c.Size())
	a.treeShown = a.reveal(c, "tree", a.showTree)
	a.historyShown = a.reveal(c, "history", a.showHistory)
	a.nativeBar = a.nativeToolbar(c)
	tree, history := a.treeShown > 0, a.historyShown > 0
	treeWidth, historyWidth := sliding(&a.treeWidth, a.treeShown), sliding(&a.historyWidth, a.historyShown)
	a.diffWidth, _ = c.Size()
	if tree {
		a.diffWidth -= *treeWidth
	}
	if history {
		a.diffWidth -= *historyWidth
	}
	content := func() {
		ui.Column(c).Fill().Background(a.theme.Background).Children(func() {
			a.toolbar(c, !tree, !history)
			switch conv := a.shownConversation(); {
			case conv != nil:
				a.conversationPane(c, conv)
			case a.mode == modeFiles:
				a.codePane(c)
			default:
				a.diffPane(c)
			}
		})
	}
	rest := func() {
		if tree {
			a.lineless(c, func() {
				ui.Split(c, treeWidth, slide(c, a.treeWidth, a.themedPane(c, func() { a.treePane(c) })), a.themedPane(c, content)).Fill()
			})
		} else {
			content()
		}
	}
	if history {
		a.lineless(c, func() {
			endSplit(c, historyWidth, a.historyWidth, a.themedPane(c, func() { a.history(c) }), a.themedPane(c, rest)).Fill()
		})
	} else {
		rest()
	}
	a.branchSwitcher(c)
	a.alerts(c)
	a.sync(c)
	// Over the window's material the sidebar shows it, and the other
	// columns draw their own backgrounds, as AppKit's source lists do: the
	// root clear once the pass is built, as SetTheme, which the lists'
	// themes call (withFocus), gives it the theme's background again.
	if c.Vibrancy() {
		c.Root().Background(ui.Transparent)
	}
	if a.redraw {
		a.redraw = false
		c.Services().Invalidate()
	}
}

// minContent is the narrowest the text gets beside the side columns,
// room for the toolbar's controls over it.
const minContent = 480

// fit hides the history, then the sidebar, where the window is too narrow
// for them beside the text, and shows them again as it widens, as AppKit's
// split views collapse their sidebars as the window narrows. It acts as
// the width changes alone, so that a column shown or widened stays.
func (a *app) fit(width, _ float32) {
	if width == a.fitWidth {
		return
	}
	a.fitWidth = width
	need := func() float32 {
		w := float32(minContent)
		if a.showTree {
			w += a.treeWidth
		}
		if a.showHistory {
			w += a.historyWidth
		}
		return w
	}
	if a.showHistory && need() > width {
		a.showHistory, a.collapsedHistory = false, true
	}
	if a.showTree && need() > width {
		a.showTree, a.collapsedTree = false, true
	}
	if a.collapsedTree && need()+a.treeWidth <= width {
		a.showTree, a.collapsedTree = true, false
	}
	if a.collapsedHistory && need()+a.historyWidth <= width {
		a.showHistory, a.collapsedHistory = true, false
	}
}

// reveal is how far the column on shows, which slides in and out as it
// shows and hides, as Xcode's navigator and inspector do, at once where
// the desktop asks for less motion. A project the window shows anew
// starts with its columns as they are.
func (a *app) reveal(c *ui.Context, column string, on bool) float32 {
	var to float32
	if on {
		to = 1
	}
	return c.Root().AnimateWith(columnKey{a, column}, to, 250*time.Millisecond, ui.EaseInOut)
}

// columnKey tells apart the columns' animations, by project.
type columnKey struct {
	a      *app
	column string
}

// sliding is the width a column shows at, of *width while it shows in
// full, for its divider's drags to change, and a part of it while it
// slides.
func sliding(width *float32, shown float32) *float32 {
	if shown >= 1 {
		return width
	}
	w := *width * shown
	return &w
}

// slide builds a column's pane at its full width, against the divider,
// so that it slides past the window's edge as its split narrows it,
// rather than squeezing.
func slide(c *ui.Context, width float32, build func()) func() {
	return func() {
		ui.Column(c).Absolute().Top(0).Bottom(0).End(0).Width(width).Children(build)
	}
}

// toggle shows or hides a column as the user asks, which the window's
// resizing then leaves as it is.
func (a *app) toggle(on, collapsed *bool) {
	*on, *collapsed = !*on, false
}

// lineless builds splits whose dividers draw no line under AppKit's
// toolbar, as Xcode's navigator and inspector meet the editor: the
// columns' colors part them. Their panes take the window's theme again
// (themedPane).
func (a *app) lineless(c *ui.Context, build func()) {
	if !a.nativeBar {
		build()
		return
	}
	bare := *a.theme
	bare.Border = ui.Transparent
	c.SetTheme(&bare)
	build()
	c.SetTheme(a.theme)
}

// themedPane builds a pane of a split in the window's theme.
func (a *app) themedPane(c *ui.Context, build func()) func() {
	return func() {
		c.SetTheme(a.theme)
		build()
	}
}

// endSplit is Split with end at the end, *size its width, as a window's
// inspector keeps its width while the window resizes: a Split laid out
// the other way round, its panes the window's way, end built full wide
// (slide).
func endSplit(c *ui.Context, size *float32, full float32, end, rest func()) ui.Element {
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
		ui.Split(c, size, slide(c, full, pane(end)), pane(rest)).Fill()
	})
}

// toolbarPart is the part of the window's toolbar over a column: a row of
// the title bar's height, which drags the window, leaving the traffic
// lights their room at the window's start and the window's edge at its
// end. Its groups of controls are ui.Toolbars, which take no face, Tab
// stopping on each once and the arrows moving among their controls; the
// names, which their toolbar would put in its overflow menu for want of
// room, are out of them, to be cut short. Under AppKit's toolbar, it is
// the room the toolbar takes, which shows the column's background.
func (a *app) toolbarPart(c *ui.Context, start, end bool, build func()) {
	t := c.Theme()
	bar := c.TitleBar()
	if a.nativeBar {
		// Its room drags the window, and a double click zooms it, as the
		// toolbar's own does, where no item is.
		ui.Box(c).Height(max(bar.Height, titleBarHeight)).Shrink(0).FillWidth().DragWindow()
		return
	}
	left, right := t.Space(2), t.Space(2)
	if start {
		left += bar.Left
	}
	if end {
		right += bar.Right
	}
	ui.Row(c).Height(titleBarHeight).Shrink(0).FillWidth().AlignItems(ui.Center).
		Padding(0, right, 0, left).Gap(t.Space(1)).DragWindow().Children(build)
}

// toolGroup is a group of the toolbar's controls, built by build, which
// show nothing under the pointer, as the items of AppKit's toolbars.
func toolGroup(c *ui.Context, label string, build func()) ui.Element {
	t := c.Theme()
	bare := *t
	bare.SurfaceHover = ui.Transparent
	return ui.Toolbar(c, func() {
		c.SetTheme(&bare)
		build()
		c.SetTheme(t)
	}).Shrink(0).Padding(0).Label(label)
}

// actions are the toolbar's work on the commits: shipping them, and
// asking Codex about the changes, which it explains and reviews, over the history, which they add to, or at the end
// of the text's part while the history hides.
func (a *app) actions(c *ui.Context) {
	a.pushButton(c)
	a.codexButton(c)
}

// repository is the toolbar's controls of the repository: the project,
// its branch and the commits to pull, over the sidebar, which lists its
// files, or at the start of the text's part while the sidebar hides, as
// Finder's title moves. The branch gives way first where room is short.
func (a *app) repository(c *ui.Context) ui.Element {
	t := c.Theme()
	return ui.Row(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(1)).Children(func() {
		a.projectTitle(c)
		a.branchLabel(c)
		a.pullButton(c)
	})
}

// toolbar is the part of the toolbar over the text: the mode in the middle
// and the page on GitHub at the end, with the buttons and the controls of
// the columns hidden, which their own parts hold while they show.
func (a *app) toolbar(c *ui.Context, start, end bool) {
	t := c.Theme()
	a.toolbarPart(c, start, end, func() {
		if start {
			toolGroup(c, "Sidebar", func() {
				a.paneButton(c, "sidebar", sidebarIcon, &a.showTree, &a.collapsedTree, "Sidebar")
			})
			a.repository(c)
		} else {
			ui.Row(c).Grow(1).Basis(0).MinWidth(0)
		}
		a.modeChoice = a.mode
		toolGroup(c, "Mode", func() {
			modeSwitch(c.Key("mode"), &a.modeChoice, "Changes", "Files").Label("Mode").
				OnChange(func() { a.setMode(a.modeChoice) })
		})
		// Both sides as wide, so that the mode stays in the middle, where
		// the controls at the end have the room, as they had last: else the
		// mode gives way, as an NSToolbar's centered item does.
		if w := a.endTools.Bounds(c).W; w != a.endWidth {
			a.endWidth, a.redraw = w, true
		}
		ui.Row(c).Grow(1).Basis(0).MinWidth(a.endWidth).AlignItems(ui.Center).Justify(ui.End).Gap(t.Space(1)).Children(func() {
			toolGroup(c, "Toolbar", func() {
				if page := a.githubPage(); page != "" {
					a.linkButton(c, "github", githubIcon, "Open on GitHub", page)
				}
				if end {
					a.actions(c)
					a.paneButton(c, "history", historyIcon, &a.showHistory, &a.collapsedHistory, "History")
				}
			}).Bind(&a.endTools)
		})
	})
}

// modeSwitch is a segmented control as macOS toolbars draw it: a filled
// track, 24 points tall, of segments as wide as one another, the chosen
// one raised from it. With Increase Contrast, the track is outlined.
func modeSwitch(c *ui.Context, selected *int, labels ...string) ui.Element {
	t := c.Theme()
	const pad, inset = 10, 2
	var widest float32
	for _, label := range labels {
		w, _ := c.MeasureText(0, ui.Span{Text: label, Size: t.FontSize})
		widest = max(widest, w)
	}
	track, chosen := t.Text.Alpha(0.06), t.Background
	if t.Dark {
		track, chosen = t.Text.Alpha(0.1), t.Text.Alpha(0.24)
	}
	parts := ui.SegmentedBase(c, selected, len(labels))
	parts.Track.Height(24).AlignItems(ui.Stretch).Padding(inset).Gap(inset).Radius(7).Background(track)
	if c.Preferences().HighContrast {
		parts.Track.Border(1, t.Border)
	}
	parts.Track.Children(func() {
		for i, label := range labels {
			// Nothing under the pointer, as NSSegmentedControl.
			s := parts.Segment(i).Width(widest + 2*pad).Radius(5).TextColor(t.Text)
			if i == *selected {
				s.Background(chosen).Shadow(0, 1, 2, 0, ui.RGBA(0, 0, 0, 0.15))
			}
			s.Children(func() { ui.Text(c, label).FontSize(t.FontSize).SingleLine() })
		}
	})
	return parts.Track
}

// paneButton shows or hides a column, as the sidebar buttons of macOS
// toolbars: an icon alone, named for what a click does.
func (a *app) paneButton(c *ui.Context, key string, svg *ui.SVG, on, collapsed *bool, pane string) {
	action := showOrHide(*on, pane)
	ui.Button(c.Key(key), "").Shrink(0).Label(action).Tooltip(action).
		Children(func() { ui.Icon(c, svg).Size(16, 16) }).OnClick(func() { a.toggle(on, collapsed) })
}

// projectTitle is the title of the window, the project's name, which a
// click on switches the project.
func (a *app) projectTitle(c *ui.Context) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("project")).Bind(&a.titleButton).Shrink(0).MaxWidth(160).
		Padding(t.Space(0.5), t.Space(2)).Radius(t.Radius).Label("Switch Project").Tooltip(a.repo)
	flat(b, t, false)
	b.OnClick(func() {
		if a.projects != nil {
			r := a.titleButton.Bounds(c)
			a.projects.switcher(a.win, r.X, r.Y+r.H+4)
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

// sideColumn builds a side column, the sidebar or the history: over bg,
// its part of the toolbar holding its button, then its title as a source
// list's section heading, small and muted, without a bar or a divider,
// with a detail at its end such as a count, and the body.
func sideColumn(c *ui.Context, bg ui.Color, toolbar func(), title string, detail, body func()) {
	t := c.Theme()
	ui.Column(c).Fill().Background(bg).Children(func() {
		toolbar()
		ui.Row(c).Shrink(0).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).
			Padding(t.Space(1), t.Space(4), 0, t.Space(4)).Children(func() {
			ui.Text(c, title).FontSize(smallSize).FontWeight(600).TextColor(t.TextMuted).SingleLine().
				Ellipsis("…").Grow(1).MinWidth(0)
			detail()
		})
		body()
	})
}

// sidebarBackground is the sidebar's: none over the window's material,
// which shows through, and the gray of an opaque source list elsewhere,
// as with Reduce Transparency.
func sidebarBackground(c *ui.Context) ui.Color {
	if c.Vibrancy() {
		return ui.Transparent
	}
	return c.Theme().Surface
}

// mutedDetail builds the detail of a heading, muted.
func mutedDetail(c *ui.Context, detail string) func() {
	return func() {
		if detail != "" {
			ui.Text(c, detail).FontSize(smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
		}
	}
}

// jumpBar is the bar over the text, as Xcode's jump bar: the title, its
// references to pull requests and issues, as #123, linked to the GitHub
// repository at github where there is one, what detail builds at its end,
// a divider, and the body.
func jumpBar(c *ui.Context, title, github string, detail, body func()) {
	t := c.Theme()
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
	n := 0
	for _, cm := range a.commits {
		if cm.Hash != git.Worktree && !git.IsRange(cm.Hash) {
			n++
		}
	}
	bg := a.theme.Background
	if a.nativeBar {
		bg = ui.Transparent // over the window's color, as an inspector
	}
	sideColumn(c, bg, func() {
		a.toolbarPart(c, false, true, func() {
			ui.Spacer(c)
			toolGroup(c, "History Toolbar", func() {
				a.actions(c)
				a.paneButton(c, "history", historyIcon, &a.showHistory, &a.collapsedHistory, "History")
			})
		})
	}, "History", mutedDetail(c, count(n, "commit")), func() {
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
			sideList(c, list).Rows(func(row ui.ListRow) { a.commitItem(row, row.Index) })
		})
	})
}

// commitItem is the row of the commit at index i of the history.
func (a *app) commitItem(row ui.ListRow, i int) {
	c := row.Context
	t := c.Theme()
	cm := a.commits[i]
	in := rowInk(t, row.Selected(), row.ListFocused())
	text, muted := in.text, in.muted
	item := ui.Row(c).FillWidth().Gap(t.Space(2.5)).Padding(t.Space(2), t.Space(2)).AlignItems(ui.Start).Children(func() {
		if cm.Hash == git.Worktree || git.IsRange(cm.Hash) {
			tint, glyph := system(c, systemOrange), worktreeIcon
			if cm.Hash != git.Worktree {
				tint, glyph = system(c, systemPurple), branchIcon
			}
			badge := ui.Box(c).Size(30, 30).Shrink(0).Radius(999).Center().
				Background(tint.Alpha(0.16)).TextColor(tint)
			if in.onAccent {
				badge.Background(t.AccentText.Alpha(0.2)).TextColor(t.AccentText)
			}
			badge.Children(func() { ui.Icon(c, glyph).Size(15, 15) })
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
	switch {
	case git.IsRange(cm.Hash):
		item.Tooltip(cm.Body)
	case cm.Hash != git.Worktree:
		item.Tooltip(commitMessage(cm))
		a.commitMenu(c, item, i, cm)
	}
}

// commitMessage is a commit's whole message, its subject and body.
func commitMessage(cm git.Commit) string {
	return strings.TrimSpace(cm.Subject + "\n\n" + cm.Body)
}

// commitMenu is the context menu of a commit's row: copying its hash or
// message, and its page on GitHub.
func (a *app) commitMenu(c *ui.Context, item ui.Element, i int, cm git.Commit) {
	services := c.Services()
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy Commit Hash").Chosen() {
			services.WriteClipboard(cm.Hash)
			c.Toast("Commit Hash Copied")
		}
		if m.Item("Copy Commit Message").Chosen() {
			services.WriteClipboard(commitMessage(cm))
			c.Toast("Commit Message Copied")
		}
		if a.github != "" {
			m.Separator()
			if m.Item("Open on GitHub").Chosen() {
				services.OpenURL(a.github + "/commit/" + cm.Hash)
			}
		}
	})
}

// personAvatar is the picture of someone, size DIPs across. Avatar lays
// out its picture and initials for its own size, not this one: the
// picture is cut to the circle here, and the initials scaled to it.
func (a *app) personAvatar(c *ui.Context, name, email string, size float32) ui.Element {
	box := ui.Box(c).Size(size, size).Shrink(0).Radius(size / 2).Role(ui.RoleImage).Label(name)
	if bm := a.avatar(email); bm != nil {
		a.avatars.show(bm)
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
	toolbar := func() {
		a.toolbarPart(c, true, false, func() {
			a.repository(c)
			toolGroup(c, "Sidebar Toolbar", func() {
				a.paneButton(c, "sidebar", sidebarIcon, &a.showTree, &a.collapsedTree, "Sidebar")
			})
		})
		a.navigatorBar(c)
	}
	if a.navigator == navConversations {
		sideColumn(c, sidebarBackground(c), toolbar, "Conversations", mutedDetail(c, ""), func() { a.conversationsPane(c) })
		return
	}
	title, key, tree, label := "Changed Files", "changes", &a.changeTree, "Changes"
	if a.mode == modeFiles {
		title, key, tree, label = "Files", "all", &a.allTree, "Files"
	}
	tree.applyFilter()
	// The lines the changes add and delete in all, as GitHub's pull
	// requests total them, or how many files the filter keeps.
	detail := func() {
		switch {
		case tree.keep != nil:
			kept, all := tree.filterCount()
			mutedDetail(c, fmt.Sprintf("%d of %s", kept, count(all, "file")))()
		case a.mode == modeChanges && len(a.diffs) > 0:
			added, deleted := sumLines(a.diffs)
			lineCounts(c, added, deleted, false)
		}
	}
	sideColumn(c, sidebarBackground(c), toolbar, title, detail, func() {
		switch {
		case tree.dirs == nil:
			loading(c)
		case tree.keep != nil && len(tree.entries("")) == 0:
			emptyNote(c, "No Results")
		default:
			a.treeView(c.Key(key), tree, label)
		}
		a.filterBar(c, key, &tree.filterState)
	})
}

// filterBar is the field at the bottom of the sidebar that filters its
// tree, as Xcode's navigator has: Down moves to the tree, Escape clears it.
func (a *app) filterBar(c *ui.Context, key string, f *filterState) {
	t := c.Theme()
	if !a.nativeBar {
		ui.Divider(c)
	}
	ui.Row(c).Shrink(0).FillWidth().Padding(t.Space(2)).Children(func() {
		field := searchField(c, "filter:"+key, &f.filter, "Filter", "Filter Files", &f.field, &f.focus,
			func() { f.toList = true }).FillWidth()
		if a.nativeBar {
			// A capsule over the sidebar's color, as Xcode's filter.
			field.Radius(t.Space(4)).Background(t.Background.Alpha(0.7)).Border(1, t.Border.Alpha(0.6))
			if f.field.FocusWithin(c) {
				field.Border(1, t.Accent)
			}
		}
	})
}

// filterFiles puts the keys in the sidebar's filter field, showing the
// sidebar where it is hidden, as Filter Files asks.
func (a *app) filterFiles() {
	if !a.showTree {
		a.toggle(&a.showTree, &a.collapsedTree)
	}
	tree := &a.changeTree
	if a.mode == modeFiles {
		tree = &a.allTree
	}
	tree.focus = true
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
	services := c.Services()
	row.ContextMenu(func(m *ui.Menu) {
		if tree == &a.changeTree {
			a.readMenu(m, p)
			if isGroup(p) {
				return
			}
			m.Separator()
		}
		a.fileMenu(services, m, itemPath(p), !isFile)
	})
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

// fileMenu is the part of a file's or a folder's context menu about where
// it is: its path to copy, it in Finder or in its editor, as the work tree
// has it, and its page on GitHub, as the commit shown has it.
func (a *app) fileMenu(services ui.Services, m *ui.Menu, p string, isDir bool) {
	local := filepath.Join(a.repo, filepath.FromSlash(p))
	there := exists(local)
	if m.Item("Copy Path").Chosen() {
		services.WriteClipboard(p)
	}
	if m.Item("Show in Finder").Disabled(!there).Chosen() {
		mygo.Shell.ShowItemInFolder(local)
	}
	if !isDir && m.Item("Open with External Editor").Disabled(!there).Chosen() {
		mygo.Shell.OpenPath(local)
	}
	cm, ok := a.current()
	if a.github == "" || !ok || cm.Hash == git.Worktree {
		return
	}
	kind := "blob/"
	if isDir {
		kind = "tree/"
	}
	m.Separator()
	gone := a.mode == modeChanges && a.changes[p].Status == "D"
	if m.Item("Open on GitHub").Disabled(gone).Chosen() {
		services.OpenURL(a.github + "/" + kind + git.Tip(cm.Hash) + "/" + p)
	}
}

// statusMark is the letter of a change, as Xcode's navigator shows it.
func statusMark(c *ui.Context, status string, onAccent bool) {
	t := c.Theme()
	color := statusColor(c, status)
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
	jumpBar(c, a.openFile, "", func() {
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
	a.applyShot(c)
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
		if a.loadedRow >= 0 {
			a.navigator = navFiles // the entry chosen shows its changes
		}
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
			a.commits, a.worktreeChanges, a.branch, a.github, a.branchURL = h.commits, h.changes, h.branch, h.github, h.branchURL
			a.canPush, a.behind, a.upstream, a.dirty = h.canPush, h.behind, h.upstream, len(h.changes)
			a.updateReviewSource(h.review, h.reviewErr)
			a.updateBranchReview(h.branchReview)
			a.readPullURL(false)
			if len(h.commits) > 0 {
				a.commitRow = landingRow(h)
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
			a.openChanges(nil)
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
			a.allTree.replace(tree)
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
	// Folders opened from Finder, the Dock or File ▸ Open Recent.
	mygo.App.OnOpenFile(func(path string) {
		go p.openFolder(path, nil)
	})
	// A Mac app stays open without windows, and opens one again as its
	// Dock icon is clicked.
	mygo.App.OnWindowAllClosed(func() {})
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			mygo.App.Show() // where it hid as inactive
			p.showWindow()
		}
	})
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) { p.quitting = true })
	p.followApp()
	if p.menuBarOn {
		useDock(false) // from the start, its icon never showing
	}
	mygo.App.WhenReady(func() {
		p.setMenu()
		p.setDockMenu()
		p.showMenuBar(p.menuBarOn)
		p.followNetwork()
		if err == nil {
			p.open(repo)
		} else {
			p.restore()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
