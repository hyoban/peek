package app

import (
	"path"
	"strconv"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// nativeToolbar shows the toolbar as AppKit's (mygo.Toolbar), over the
// columns its separators follow, as Xcode's and Mail's: the repository's
// controls over the sidebar, the mode and the page on GitHub over the
// text, and the actions over the history; a column hidden, its controls
// keep their room, where it left them. It reports whether it shows: not in tests,
// which have no window and draw the toolbar in the view (toolbarPart).
func (a *app) nativeToolbar(c *ui.Context) bool {
	if !a.native() {
		return false
	}
	return a.win.SetToolbar(a.toolbarSpec(c))
}

// toolbarSpec is the toolbar as the state has it now. Its items stay the
// same, hidden where they do not apply, for the arrangement the user
// chooses to keep.
func (a *app) toolbarSpec(c *ui.Context) *mygo.Toolbar {
	// The side columns with the line between them and the text, which
	// shows what is under it: their color.
	w, _ := c.Size()
	// They slide as the view's do (reveal), and keep their part of the
	// toolbar while they hide, its items staying where they stop, as
	// Xcode's: collapsed, the items would jump to the next column's.
	tree, history := float64(a.treeWidth*a.treeShown)+1, float64(a.historyWidth*a.historyShown)+1
	text := float64(w) - tree - history
	services := c.Services()
	page := a.githubPage()
	busy := a.push.running || a.review.running || a.pull.running
	// The sidebar's button at the start, beside the window's buttons,
	// where it stays as the sidebar slides, as Xcode's navigator's; the
	// project and its branch then, as the text's title.
	items := []mygo.ToolbarItem{
		{ID: "sidebar", Symbol: "sidebar.left", Label: showOrHide(a.showTree, "Sidebar"), ToolTip: showOrHide(a.showTree, "Sidebar"),
			Click: func() { a.toggle(&a.showTree, &a.collapsedTree) }},
		{ID: "sidebar-end", Kind: mygo.ToolbarFlexibleSpace},
	}
	items = append(items,
		mygo.ToolbarItem{ID: "sidebar-separator", Kind: mygo.ToolbarSeparator, Divider: 0},
		mygo.ToolbarItem{ID: "project", Title: path.Base(a.repo), Label: "Switch Project", ToolTip: a.repo,
			Click: a.switchProject},
		mygo.ToolbarItem{ID: "branch", Symbol: "arrow.triangle.branch", Title: a.branch, Label: "Switch Branch", ToolTip: a.branch,
			Hidden: a.branch == "", Disabled: busy, Click: a.toggleSwitcher},
	)
	items = append(items, a.pullItems()...)
	items = append(items,
		mygo.ToolbarItem{ID: "text-start", Kind: mygo.ToolbarFlexibleSpace},
		mygo.ToolbarItem{ID: "mode", Kind: mygo.ToolbarSegmented, Segments: []string{"Changes", "Files"}, Selected: a.mode, Label: "Mode",
			Select: func(i int) { a.modeChoice = i; a.setMode(i) }},
		mygo.ToolbarItem{ID: "text-end", Kind: mygo.ToolbarFlexibleSpace},
		mygo.ToolbarItem{ID: "github", Symbol: "arrow.up.forward.app", Label: "Open on GitHub", ToolTip: "Open on GitHub", Hidden: page == "",
			Click: func() { services.OpenURL(page) }},
		mygo.ToolbarItem{ID: "history-separator", Kind: mygo.ToolbarSeparator, Divider: 1},
		mygo.ToolbarItem{ID: "history-start", Kind: mygo.ToolbarFlexibleSpace},
	)
	items = append(items, toolItems("explain", a.explainTool(c))...)
	items = append(items, toolItems("review", a.reviewTool(c))...)
	items = append(items, toolItems("push", a.pushTool(c))...)
	items = append(items, mygo.ToolbarItem{ID: "history", Symbol: "sidebar.right", Label: showOrHide(a.showHistory, "History"),
		ToolTip: showOrHide(a.showHistory, "History"), Click: func() { a.toggle(&a.showHistory, &a.collapsedHistory) }})
	return &mygo.Toolbar{
		Items: items,
		Columns: []mygo.ToolbarColumn{
			// The sidebar and the history over the window's color, as
			// Xcode's navigator and inspector, around the text.
			{Width: tree, Sidebar: a.treeShown > 0},
			{Width: text},
			{Width: history, Sidebar: a.historyShown > 0},
		},
		Customizable: true,
		AutosaveName: "peek.toolbar",
	}
}

// toolItems are the items of a button of the toolbar: the button, or the
// activity of its work under way, which stops it, as one button of the
// same capsule. Its stage alone shows, for its width to stay as the
// seconds pass, and the time it has taken in its tooltip.
func toolItems(key string, b toolButton) []mygo.ToolbarItem {
	tooltip := b.stop + "\n" + b.tooltip
	if b.elapsed != "" {
		tooltip += " (" + b.elapsed + ")"
	}
	return []mygo.ToolbarItem{
		{ID: key, Title: b.label, Label: b.label, ToolTip: b.tooltip, Disabled: b.disabled, Hidden: b.busy, Click: b.click},
		{ID: key + "-activity", Kind: mygo.ToolbarActivity, Title: b.label, Label: b.stop, ToolTip: tooltip, Hidden: !b.busy, Click: b.cancel},
	}
}

// pullItems are the Pull button's: the commits to pull, or the pull under
// way.
func (a *app) pullItems() []mygo.ToolbarItem {
	tooltip := "Pull " + count(a.behind, "new commit") + " from " + a.upstream + ", fast-forward only"
	busy := a.push.running || a.review.running || a.switcher.running != ""
	return []mygo.ToolbarItem{
		{ID: "pull", Symbol: "arrow.down", Title: strconv.Itoa(a.behind), Label: "Pull", ToolTip: tooltip,
			Hidden: a.behind == 0 || a.pull.running, Disabled: busy, Click: a.startPull},
		{ID: "pull-activity", Kind: mygo.ToolbarActivity, Title: "Pulling…", Label: "Pull", ToolTip: "Pulling " + a.upstream + "…",
			Hidden: !a.pull.running},
	}
}

// switchProject lists the projects to switch to under the project's name.
func (a *app) switchProject() {
	if a.projects == nil {
		return
	}
	r, _ := a.win.ToolbarItemBounds("project")
	a.projects.switcher(a.win, float32(r.X), float32(r.Y+r.Height+4))
}

// toggleSwitcher opens the branch switcher, or closes it.
func (a *app) toggleSwitcher() {
	if a.switcher.open {
		a.switcher.open = false
		return
	}
	a.openSwitcher()
	a.switcher.focus = true
}
