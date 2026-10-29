// Package app is Peek's window: the history of a repository, the files
// of the chosen commit, and their diffs or texts.
package app

import (
	"fmt"
	"image"
	"log"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"peek/internal/fileicons"
	"peek/internal/git"
	"peek/internal/review"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
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
	// pullReq is the branch's pull request, nil for none, as read for the
	// branch's page pullFor: the button opens its page over the branch's,
	// and the branch's changes show it over their diffs.
	pullReq     *git.BranchPull
	pullFor     string
	pullReading bool // gh is asked for it
	// pullMarkdown shows its description, and checksOpen tells, by its
	// page, whether its checks list where the user chose.
	pullMarkdown *markdown.Options
	checksOpen   map[string]bool
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
	fetchedAt                time.Time
	pull                     pullState
	review                   reviewState
	answer                   answerState
	ask                      askState    // the popover's
	compose                  askState    // a new conversation's field's
	lastCodex                codexResult // which result came last
	// conversations are those with Codex about the repository, newest
	// first, kept in convFile; convRow is the one chosen, and navigator
	// which the sidebar lists, the files or the conversations.
	conversations []*conversation
	convList      ui.OutlineState[*conversation]
	convRow       int
	convFile      string
	navigator     int
	reply         string    // the request to follow up with
	streamed      int       // how much of the answer streaming the transcript followed
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
	switcherPopover, askPopoverWin child

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
	commitList ui.OutlineState[string] // by hash, "" the work tree
	// pictures are the avatars and badges of the history's rows, drawn
	// for its list, by what they show.
	pictures  map[string]*image.RGBA
	commitRow int
	loadedRow int // the commit whose changes are loaded, or loading
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
	// groupDiffs are the diffs of each group of the change tree, which its
	// row's badges count each time the sidebar asks for it.
	groupDiffs map[string][]*fileDiff
	diffGen    int // counts the lists of diffs, to drop stale loads
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
	allTree            pathTree
	filesFor           int    // the row of the commit whose files are listed, or load; -1 for none
	openFile           string // the file whose text shows, or loads
	code               *editor.State
	codePath, codeHash string // the source actually displayed, while another may load
	textGen            int    // rejects older file loads, including the same path in another commit
	closed             bool
	navigation         navigationState
	packages           packagesState
	openURL            func(string)   // opens pages, as the window's services do
	picture            ui.ImageSource // the file's picture, in place of code
	codeNote           string         // why the text does not show
	preview            *mdPreview     // a Markdown file's, beside its code
	// previewed is the Markdown files shown as their previews, their
	// sources showing by default.
	previewed map[string]bool
	// restoring is how the project showed as it last closed, until the
	// file it showed shows again (restoreView).
	restoring *repoView

	terminal terminalState
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
		compose:      askState{template: -1},
		convRow:      -1,
		terminal:     terminalState{height: 240},
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
	xcodeGrays(t, c.Preferences().HighContrast)
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
	// Under AppKit's toolbar, the columns are its split view's, which the
	// user drags and which slides them in and out: they show at its
	// widths.
	split := a.columnWidths()
	if split != nil {
		a.treeShown, a.historyShown = shown(split[0], a.treeWidth), shown(split[2], a.historyWidth)
	}
	tree, history := a.treeShown > 0, a.historyShown > 0
	treeWidth, historyWidth := sliding(&a.treeWidth, a.treeShown), sliding(&a.historyWidth, a.historyShown)
	if split != nil {
		tw, hw := float32(split[0]), float32(split[2])
		treeWidth, historyWidth = &tw, &hw
	}
	a.diffWidth, _ = c.Size()
	if tree {
		a.diffWidth -= *treeWidth
	}
	if history {
		a.diffWidth -= *historyWidth
	}
	content := func() {
		ui.Column(c).Fill().Background(a.theme.Background).Children(func() {
			// The toolbar's room over the text in its band, as the jump
			// bar under it, as Xcode's editor's.
			ui.Column(c).Shrink(0).FillWidth().Background(chromeColor(a.theme)).Children(func() { a.toolbar(c, !tree, !history) })
			a.withTerminal(c, func() {
				switch conv := a.shownConversation(); {
				case conv != nil:
					a.conversationPane(c, conv)
				case a.mode == modeFiles:
					a.codePane(c)
				default:
					a.diffPane(c)
				}
			})
		})
	}
	if split != nil {
		ui.Row(c).Fill().Children(func() {
			if tree {
				ui.Column(c).Width(*treeWidth).Shrink(0).FillHeight().Clip().
					Children(slide(c, a.treeWidth, a.themedPane(c, func() { a.treePane(c) })))
			}
			ui.Column(c).Grow(1).MinWidth(0).FillHeight().Children(a.themedPane(c, content))
			if history {
				ui.Column(c).Width(*historyWidth).Shrink(0).FillHeight().Clip().
					Children(slideStart(c, a.historyWidth, a.themedPane(c, func() { a.history(c) })))
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
	switch {
	case split != nil:
	case history:
		a.lineless(c, func() {
			endSplit(c, historyWidth, a.historyWidth, a.themedPane(c, func() { a.history(c) }), a.themedPane(c, rest)).Fill()
		})
	default:
		rest()
	}
	a.askPopover(c)
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

// columnWidths returns the widths the columns of AppKit's split view
// show at, the sidebar's, the text's and the history's: nil without it.
func (a *app) columnWidths() []float64 {
	if !a.nativeBar {
		return nil
	}
	widths := a.win.ToolbarColumnWidths()
	if len(widths) != 3 {
		return nil
	}
	return widths
}

// shown is how far a column of the split view shows, of its width.
func shown(at float64, width float32) float32 {
	if width <= 0 {
		return 0
	}
	return min(float32(at)/width, 1)
}

// slideStart is slide for a column at the end, its pane against the
// divider at its start.
func slideStart(c *ui.Context, width float32, build func()) func() {
	return func() {
		ui.Column(c).Absolute().Top(0).Bottom(0).Start(0).Width(width).Children(build)
	}
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
				a.terminalButton(c)
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

// terminalButton shows or hides the terminal under the text.
func (a *app) terminalButton(c *ui.Context) {
	ui.Button(c.Key("terminal"), "").Shrink(0).Label(showOrHide(a.terminal.shown, "Terminal")).Tooltip(terminalTip(a.terminal.shown)).
		Children(func() { ui.Icon(c, terminalIcon).Size(16, 16) }).OnClick(a.toggleTerminal)
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
		Padding(0, t.Space(3)).Background(chromeColor(t)).Children(func() {
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

// sideList gives a list of the sidebar the column's width, to its edges:
// AppKit's source list insets its rows and their highlights itself, and
// its headings line up with the column's title then, as a sidebar's.
func sideList(c *ui.Context, list ui.Element) ui.Element {
	return list.Grow(1).FillWidth()
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
		a.commitList.List.Selected = &a.commitRow
		// An inspector's list, of rows of two lines under their avatars.
		a.commitList.Inset, a.commitList.RowHeight = true, 46
		hashes := make([]string, len(a.commits))
		index := make(map[string]int, len(a.commits))
		for i, cm := range a.commits {
			hashes[i], index[cm.Hash] = cm.Hash, i
		}
		item := a.commitItem(c)
		list := ui.SourceList(c.Key("history"), &a.commitList, hashes, func(string) []string { return nil },
			func(hash string) ui.SourceListItem { return item(a.commits[index[hash]]) })
		list.Grow(1).FillWidth().Label("History")
	})
}

// commitItem returns what describes the row of a commit of the history:
// its subject over when and who, after its author's avatar with the
// first co-author's over its corner, as GitHub stacks them; the work
// tree's and a range's with their badges, over their summaries.
func (a *app) commitItem(c *ui.Context) func(cm git.Commit) ui.SourceListItem {
	dark := c.Theme().Dark
	orange, purple, blue := system(c, systemOrange), system(c, systemPurple), system(c, systemBlue)
	ring := c.Theme().Background
	services := c.Services()
	return func(cm git.Commit) ui.SourceListItem {
		it := ui.SourceListItem{Title: cm.Subject, Bold: true, ImageSize: 34}
		if cm.Hash == git.Worktree || git.IsRange(cm.Hash) {
			tint, glyph, name := orange, worktreeIcon, "worktree"
			if cm.Hash != git.Worktree {
				tint, glyph, name = purple, branchIcon, "range"
				it.Tooltip = cm.Body
				if pr := a.pullReq; pr != nil && a.pullFor == a.branchURL {
					if b, ok := checksBadge(c, pr); ok {
						it.Badges = []ui.SourceListBadge{b}
						it.Tooltip += "\n\n#" + strconv.Itoa(pr.Number) + ": " + checksSummary(pr)
					}
				}
			}
			it.Subtitle = cm.Summary
			it.Image = a.drawn(fmt.Sprint(name, dark), 30, func(c *ui.Context) {
				ui.Box(c).Size(30, 30).Radius(999).Center().Background(tint.Alpha(0.16)).TextColor(tint).
					Children(func() { ui.Icon(c, glyph).Size(15, 15) })
			})
			return it
		}
		// The remote's branch and the tags lead the line under the subject,
		// which they leave whole.
		var refs []string
		if cm.Remote != "" {
			refs = append(refs, cm.Remote)
		}
		it.Subtitle = strings.Join(append(append(refs, cm.Tags...), shortDate(cm.Time, time.Now()), authors(cm)), " · ")
		it.Tooltip = commitMessage(cm)
		if cm.Unpushed {
			// An arrow up, as Xcode marks the commits to push.
			it.Badges = []ui.SourceListBadge{{Text: "↑", Color: blue, Bold: true}}
			it.Tooltip += "\n\nNot pushed yet"
		}
		var co *git.Person
		key := fmt.Sprintf("avatar%v%s%s%p", dark, cm.Author, cm.Email, a.avatar(cm.Email))
		if len(cm.CoAuthors) > 0 {
			co = &cm.CoAuthors[0]
			key += fmt.Sprintf("%s%s%p", co.Name, co.Email, a.avatar(co.Email))
		}
		it.Image = a.drawn(key, 30, func(c *ui.Context) {
			ui.Box(c).Size(30, 30).Children(func() {
				a.personAvatar(c, cm.Author, cm.Email, 30)
				if co != nil {
					// A ring of the window's color parts the two.
					ui.Box(c).Size(19, 19).Radius(9.5).Center().Background(ring).
						Absolute().Right(-4).Bottom(-4).Children(func() {
						a.personAvatar(c, co.Name, co.Email, 16)
					})
				}
			})
		})
		it.Menu = func(m *ui.Menu) { a.commitMenu(c, services, m, cm) }
		return it
	}
}

// drawn draws view, size DIPs square, as a picture for a list of the
// system's, once for each key.
func (a *app) drawn(key string, size int, view func(c *ui.Context)) *image.RGBA {
	if pic, ok := a.pictures[key]; ok {
		return pic
	}
	if a.pictures == nil {
		a.pictures = map[string]*image.RGBA{}
	}
	// Room for what spills past the square, as a co-author's avatar.
	room := size + 4
	pic := ui.Render(func(c *ui.Context) {
		c.Root().Background(ui.Transparent)
		ui.Box(c).Size(float32(room), float32(room)).Children(func() { view(c) })
	}, room, room, 2)
	a.pictures[key] = pic
	return pic
}

// commitMessage is a commit's whole message, its subject and body.
func commitMessage(cm git.Commit) string {
	return strings.TrimSpace(cm.Subject + "\n\n" + cm.Body)
}

// commitMenu is the context menu of a commit's row: copying its hash or
// message, and its page on GitHub.
func (a *app) commitMenu(c *ui.Context, services ui.Services, m *ui.Menu, cm git.Commit) {
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
	members := a.together()
	// The lines the changes add and delete in all, as GitHub's pull
	// requests total them, or how many files the filter keeps; each
	// repository's own where the sidebar lists several.
	detail := func() {
		switch {
		case len(members) > 1:
		case tree.keep != nil:
			kept, all := tree.filterCount()
			mutedDetail(c, fmt.Sprintf("%d of %s", kept, count(all, "file")))()
		case a.mode == modeChanges && len(a.diffs) > 0:
			added, deleted := sumLines(a.diffs)
			lineCounts(c, added, deleted, false)
		}
	}
	sideColumn(c, sidebarBackground(c), toolbar, title, detail, func() {
		if len(members) > 1 {
			a.memberTrees(c, members, key, label)
		} else {
			a.treeBody(c, tree, key, label, nil)
		}
		a.filterBar(c, key, &tree.filterState)
	})
}

// treeBody is the sidebar's tree, or what shows while it has no rows;
// pick, where not nil, runs as a file's row is clicked.
func (a *app) treeBody(c *ui.Context, tree *pathTree, key, label string, pick func()) {
	switch {
	case tree.dirs == nil:
		loading(c)
	case tree.keep != nil && len(tree.entries("")) == 0:
		emptyNote(c, "No Results")
	default:
		a.treeView(c.Key(key), tree, label, pick)
	}
}

// sidebarTree is the tree the sidebar lists in the mode.
func (a *app) sidebarTree() *pathTree {
	if a.mode == modeFiles {
		return &a.allTree
	}
	return &a.changeTree
}

// filterBar is the field at the bottom of the sidebar that filters its
// tree, as Xcode's navigator has: Down moves to the tree, Escape clears it.
func (a *app) filterBar(c *ui.Context, key string, f *filterState) {
	t := c.Theme()
	if !a.nativeBar {
		ui.Divider(c)
	}
	ui.Row(c).Shrink(0).FillWidth().Padding(t.Space(2)).Children(func() {
		// The system's search field, as Xcode's navigator filters with.
		f.field.Placeholder, f.field.Label = "Filter", "Filter Files"
		f.field.OnKey = func(k ui.Key) bool {
			if k != ui.KeyDown {
				return false
			}
			f.toList = true
			return true
		}
		ui.NativeSearchField(c.Key("filter:"+key), &f.field, &f.filter).FillWidth()
		if f.focus {
			f.focus = false
			f.field.Focus(c)
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

func (a *app) treeView(c *ui.Context, tree *pathTree, label string, pick func()) {
	item := a.treeItem(c, tree)
	if pick != nil {
		inner := item
		item = func(p string) ui.SourceListItem {
			it := inner(p)
			if !tree.isDir(p) {
				it.OnClick = pick
			}
			return it
		}
	}
	sideList(c, tree.view(c, label, item))
}

// treeItem returns what describes the rows of the tree, as the sidebar's
// list asks for them, between frames too: it answers from what the frame
// knew, the theme's colors and the services.
func (a *app) treeItem(c *ui.Context, tree *pathTree) func(p string) ui.SourceListItem {
	dark := c.Theme().Dark
	colors := map[string]ui.Color{}
	for _, s := range []string{"A", "D", "M", "R", "C"} {
		colors[s] = statusColor(c, s)
	}
	status := func(s string) ui.Color {
		if col, ok := colors[s]; ok {
			return col
		}
		return colors["M"]
	}
	services := c.Services()
	return func(p string) ui.SourceListItem {
		isFile := !tree.isDir(p)
		it := ui.SourceListItem{Title: tree.labels[p], Tooltip: itemPath(p)}
		it.Menu = func(m *ui.Menu) {
			if tree == &a.changeTree {
				a.readMenu(m, p)
				if isGroup(p) {
					return
				}
				m.Separator()
			}
			a.fileMenu(services, m, itemPath(p), !isFile)
		}
		if isGroup(p) {
			// The lines its files add and delete, and how many of them are
			// read, as their diffs fold.
			diffs := a.diffsUnder(p)
			added, deleted := sumLines(diffs)
			it.Group, it.Tooltip = true, ""
			it.Badges = []ui.SourceListBadge{
				{Text: fmt.Sprintf("+%d", added), Color: status("A"), Digits: true},
				{Text: fmt.Sprintf("−%d", deleted), Color: status("D"), Digits: true},
				{Text: fmt.Sprintf("%d/%d", countRead(diffs), len(diffs)), Digits: true},
			}
			return it
		}
		if !isFile {
			// A click on a folder chooses it, and its arrow alone opens and
			// closes it, as AppKit's outline views keep the two apart.
			if icon := fileicons.FolderPicture(p, tree.state.Open.Has(p), 16, dark); icon != nil {
				it.Image = icon
			} else {
				it.Symbol = "folder" // in the accent, as Finder's sidebar
			}
			return it
		}
		it.Image, it.Template = fileicons.Picture(p, 16, dark)
		// Read, as the diffs fold it away.
		it.Dimmed = tree == &a.changeTree && a.readFile(p)
		if ch, changed := a.changes[p]; changed {
			it.Badges = []ui.SourceListBadge{{Text: ch.Status, Color: status(ch.Status), Bold: true}}
		}
		return it
	}
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
	if a.openURL == nil {
		a.openURL = c.Services().OpenURL
	}
	a.configureNavigation()
	a.checkFileDependencies()
	detail := ""
	if a.picture != nil {
		detail = pictureSize(a.picture)
	}
	jumpBar(c, a.openFile, "", func() {
		a.navigationTools(c)
		if detail != "" {
			ui.Text(c, detail).FontSize(smallSize).TextColor(c.Theme().TextMuted).SingleLine().Shrink(0)
		}
		if a.preview != nil && a.codeNote == "" {
			a.previewButton(c)
		}
	}, func() {
		ui.Column(c.Key("code-body")).Grow(1).FillWidth().Children(func() {
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
				a.code.Theme = codeTheme(a.prefs().CodeTheme, c.Theme().Dark)
				editor.View(c.Key("code:"+a.openFile), a.code).Grow(1).FillWidth().Bind(&a.navigation.editor)
				if a.navigation.focus {
					a.navigation.focus = false
					a.navigation.editor.Focus(c)
				}
			}
			a.definitionChoices(c)
		})
	})
}

// sync starts loading what the choices in the history, the trees and the
// mode ask for, once for each new choice.
func (a *app) sync(c *ui.Context) {
	a.checkNavigation()
	a.applyShot(c)
	cm, ok := a.loadChosen(c)
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
		a.releaseFarDiffs()
	case modeFiles:
		if p := a.allTree.chosen(); p != "" && !a.allTree.isDir(p) && p != a.openFile {
			a.openFile = p
			a.loadText(cm.Hash, p)
		}
	}
}

// loadChosen starts loading the changes of the commit chosen in the
// history, and its files in the files mode, for the sidebar to list,
// and returns the commit loaded, or loading.
func (a *app) loadChosen(c *ui.Context) (git.Commit, bool) {
	if a.changeTree.sync() || a.allTree.sync() {
		c.Services().Invalidate()
	}
	if a.commitRow >= 0 && a.commitRow < len(a.commits) && a.commitRow != a.loadedRow {
		if a.loadedRow >= 0 {
			a.navigator = navFiles // the entry chosen shows its changes
		}
		a.loadedRow = a.commitRow
		hash := a.commits[a.commitRow].Hash
		a.loadCommit(hash)
		if a.projects != nil {
			a.projects.keepEntry(a.repo, hash)
		}
	}
	cm, ok := a.current()
	if ok && a.mode == modeFiles && a.filesFor != a.loadedRow {
		a.filesFor = a.loadedRow
		a.loadFiles(cm.Hash)
	}
	return cm, ok
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
	a.cancelNavigation()
	a.mode, a.modeChoice = mode, mode
	a.restoring = nil
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
	if p == "" || (!a.allTree.has(p) && !(hash == git.Worktree && filepath.IsAbs(p))) {
		return false
	}
	a.showFileInTree(p)
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
			a.readPullRequest(false)
			if len(h.commits) > 0 {
				a.commitRow = landingRow(h)
				if row, ok := a.keptRow(); ok {
					a.commitRow = row // as the user left it
				}
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
			a.restoreFile()
		}
	})
}

// loadFiles lists the files of the commit for the files mode, keeping the
// file open where the commit has it too. The files and the text of the
// commit shown before stay until the new ones come, not to flash empty.
func (a *app) loadFiles(hash string) {
	nest := a.prefs().NestFiles
	a.background(func() func() {
		files, err := git.ListFiles(a.repo, hash)
		tree := foldFiles(files, nest)
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
				a.restoring = nil // the file kept is gone
			}
		}
	})
}

func (a *app) loadText(hash, p string) {
	a.cancelNavigation()
	a.textGen++
	gen, repo := a.textGen, a.repo
	a.background(func() func() {
		limit := maxTextSize // a text too large to show is not read whole
		if isPicture(p) {
			limit = 0
		}
		text, err := sourceText(repo, hash, p, limit)
		picture, note := fileView(p, text, err)
		var buf *editor.Buffer
		if note == "" && picture == nil {
			buf = codeBuffer(p, text)
		}
		return func() {
			cm, ok := a.current()
			if !a.closed && gen == a.textGen && p == a.openFile && ok && cm.Hash == hash {
				var (
					code    *editor.State
					preview *mdPreview
				)
				if buf != nil {
					code = codeOf(buf, a.prefs())
					if isMarkdown(p) {
						preview = a.newPreview(hash, p, text)
					}
				}
				a.code, a.picture, a.codeNote, a.preview = code, picture, note, preview
				a.codePath, a.codeHash = p, hash
				a.restoreCaret(p)
				if hash == git.Worktree {
					a.placeCaret(p, code)
				}
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
	for _, d := range []string{"/opt/homebrew/bin", "/opt/homebrew/sbin", "/usr/local/bin", filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin"), filepath.Join(home, ".vite-plus", "bin"), filepath.Join(home, ".volta", "bin")} {
		if !slices.Contains(have, d) && exists(d) {
			dirs = append(dirs, d)
		}
	}
	os.Setenv("PATH", strings.Join(dirs, string(os.PathListSeparator)))
}

// Run opens the window over the repository the folder in args holds, or
// the one the working directory is in, and runs the app until it quits;
// run as the command line tool, it opens the repository in the app.
func Run(args []string) {
	extendPath()
	if forward(args) {
		return
	}
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
	// Checks the releases on GitHub in the background, and offers new
	// versions in a window of native UI.
	mygo.Use(native.Plugin)
	p := newProjects()
	// The icons load while the app starts, ready by its first window.
	icons := make(chan struct{})
	go func() {
		fileicons.FollowVSCode()
		fileicons.Warm()
		close(icons)
	}()
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
	mygo.App.OnBeforeQuit(func(*mygo.QuitEvent) {
		p.quitting = true
		p.keepViews()
	})
	p.followApp()
	if p.prefs().MenuBar {
		useDock(false) // from the start, its icon never showing
	}
	mygo.App.WhenReady(func() {
		<-icons
		p.setMenu()
		p.setDockMenu()
		p.showMenuBar(p.prefs().MenuBar)
		p.followNetwork()
		switch {
		case err == nil:
			p.open(repo)
		case mygo.App.WasOpenedAtLogin() && p.prefs().MenuBar:
			// Opened as the user logged in, it waits in the menu bar.
		default:
			p.restore()
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
