package app

import (
	"cmp"
	"os"
	"path"
	"path/filepath"
	"peek/internal/git"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const maxRecent = 12 // the projects the switcher lists

// projects are the repositories open, a window each, as a Mac app opens a
// document in a window of its own, and the one whose window has the focus,
// which the menu bar acts on. All of it runs on the main thread.
type projects struct {
	avatars *avatars
	apps    map[string]*app // the projects open, by repository
	cur     *app            // the project of the window with the focus, nil for none
	recent  []string        // the repositories opened, the latest first
	dir     string          // where recent and the projects open are kept, "" for nowhere
	inline  bool            // no windows, the apps' loads on the UI thread, for tests
	menu    *menuItems      // the menu bar's items that follow the project, nil in tests
	empty   *mygo.Window    // the window asking for a project while none is open
	// quitting keeps the projects open as the app quits, for the next
	// launch to open them again.
	quitting bool
}

func newProjects() *projects {
	p := &projects{avatars: newAvatars(), apps: map[string]*app{}}
	if dir, err := os.UserConfigDir(); err == nil {
		p.dir = filepath.Join(dir, "peek")
		p.recent = p.readList("recent")
	}
	return p
}

// readList reads the repositories listed in the file of the app's folder
// that still exist; nil for none or no file.
func (p *projects) readList(name string) []string {
	data, err := os.ReadFile(filepath.Join(p.dir, name))
	if err != nil {
		return nil
	}
	var repos []string
	for _, repo := range strings.Split(string(data), "\n") {
		if repo != "" && exists(repo) {
			repos = append(repos, repo)
		}
	}
	return repos
}

// writeList keeps the repositories in the file of the app's folder.
func (p *projects) writeList(name string, repos []string) {
	if p.dir == "" {
		return
	}
	data := []byte(strings.Join(repos, "\n") + "\n")
	file := filepath.Join(p.dir, name)
	go func() {
		os.MkdirAll(filepath.Dir(file), 0o755)
		os.WriteFile(file, data, 0o644)
	}()
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// view shows the project opened last, as a window would, for tests.
func (p *projects) view(c *ui.Context) {
	if p.cur == nil {
		p.welcome(c)
		return
	}
	// Each project builds its own widgets, which keep their state apart.
	ui.Column(c).Key(p.cur.repo).Fill().Children(func() { p.cur.view(c) })
}

// window is the view of a project's window, whose state the menu bar
// shows while it has the focus.
func (p *projects) window(c *ui.Context, a *app) {
	a.view(c)
	if a == p.cur {
		p.syncMenu()
	}
}

// welcome is the window before any project opens, as a Mac app's welcome
// window: what to do, the way to open a folder, and the recent projects.
func (p *projects) welcome(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(titleBarHeight).Shrink(0).FillWidth().DragWindow()
		ui.Column(c).Grow(1).FillWidth().Center().Gap(t.Space(2)).Padding(t.Space(6)).Children(func() {
			ui.Icon(c, repoIcon).Size(48, 48).TextColor(t.TextMuted).Margin(0, 0, t.Space(2), 0)
			ui.Text(c, "No Repository").FontSize(fontSize + 4).FontWeight(700)
			ui.Text(c, "Open a folder in a Git repository to see its history.").TextColor(t.TextMuted).TextAlign(ui.Center)
			ui.PrimaryButton(c.Key("open"), "Open Folder…").Margin(t.Space(2), 0, 0, 0).OnClick(p.choose)
			if len(p.recent) == 0 {
				return
			}
			ui.Column(c).Width(320).Gap(1).Margin(t.Space(4), 0, 0, 0).Children(func() {
				ui.Text(c, "Recent").FontSize(smallSize).FontWeight(600).TextColor(t.TextMuted).
					Padding(0, t.Space(2), t.Space(1), t.Space(2))
				for _, repo := range p.recent {
					b := ui.ButtonBase(c.Key("recent:"+repo)).FillWidth().Padding(t.Space(1), t.Space(2)).
						Radius(t.Radius).Label(p.label(repo)).Tooltip(repo)
					flat(b, t, false)
					b.OnClick(func() { p.open(repo) })
					b.Children(func() {
						ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
							ui.Icon(c, folderIcon).Size(15, 15).Shrink(0).TextColor(t.Accent)
							ui.Text(c, p.label(repo)).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
						})
					})
				}
			})
		})
	})
}

// showWelcome opens the welcome window, or brings it to the front.
func (p *projects) showWelcome() {
	if p.empty != nil {
		p.empty.Focus()
		return
	}
	p.empty = mygo.NewWindow(mygo.WindowOptions{
		Title:                "Peek",
		Width:                520,
		Height:               480,
		DisableResize:        true,
		DisableMaximize:      true,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &trafficLightsAt,
		Content:              ui.View(p.welcome),
	})
	win := p.empty
	win.OnClosed(func() {
		if p.empty == win {
			p.empty = nil
		}
	})
}

// open shows the repository in its window, as the user left it if it is
// open, or in a new one, taking the columns of the window with the focus,
// and puts it first among the recent ones.
func (p *projects) open(repo string) {
	a := p.apps[repo]
	if a == nil {
		a = newApp(repo)
		a.projects, a.avatars, a.inline = p, p.avatars, p.inline
		if cur := p.cur; cur != nil { // the columns as they are
			a.showHistory, a.showTree = cur.showHistory, cur.showTree
			a.historyWidth, a.treeWidth = cur.historyWidth, cur.treeWidth
		}
		p.apps[repo] = a
		if !p.inline {
			p.newWindow(a)
		}
		a.loadCommits()
	} else if a != p.cur {
		a.refresh() // what changed since it last had the focus
		if a.win != nil {
			a.win.Focus()
		}
	}
	p.cur = a
	if p.empty != nil {
		p.empty.Close()
	}
	recent := []string{repo}
	for _, r := range p.recent {
		if r != repo && len(recent) < maxRecent {
			recent = append(recent, r)
		}
	}
	p.recent = recent
	p.writeList("recent", recent)
	if !p.inline {
		mygo.App.AddRecentDocument(repo) // File ▸ Open Recent and the Dock's menu
		p.saveOpen()
	}
}

// newWindow opens the project's window, where it was last, named for the
// project, its folder the window's, as a document's file is.
func (p *projects) newWindow(a *app) {
	a.win = mygo.NewWindow(mygo.WindowOptions{
		Title:                path.Base(a.repo),
		Width:                1440,
		Height:               900,
		MinWidth:             640,
		MinHeight:            400,
		StateKey:             "project:" + a.repo,
		TitleBarStyle:        mygo.TitleBarHidden,
		TrafficLightPosition: &trafficLightsAt,
		Vibrancy:             mygo.VibrancySidebar,
		Content:              ui.View(func(c *ui.Context) { p.window(c, a) }),
	})
	a.win.SetRepresentedFile(a.repo)
	a.win.OnFocus(func() {
		p.cur = a
		p.syncMenu()
	})
	a.win.OnClosed(func() {
		if a.push.cancel != nil {
			a.push.cancel()
		}
		if a.review.cancel != nil {
			a.review.cancel()
		}
		if a.unwatch != nil {
			a.unwatch()
		}
		delete(p.apps, a.repo)
		if p.cur == a {
			p.cur = nil
			p.syncMenu()
		}
		if !p.quitting {
			p.saveOpen()
		}
	})
	a.watch()
}

// saveOpen keeps the projects open, the one with the focus first, for the
// next launch to open them again.
func (p *projects) saveOpen() {
	var open []string
	for _, repo := range p.recent {
		if p.apps[repo] != nil {
			open = append(open, repo)
		}
	}
	p.writeList("open", open)
}

// switcher lists the recent projects in a menu at the point of the
// window, the one shown checked, and the way to open another.
func (p *projects) switcher(win *mygo.Window, x, y float32) {
	var items []*mygo.MenuItem
	for _, repo := range p.recent {
		items = append(items, &mygo.MenuItem{
			Label:   p.label(repo),
			ToolTip: repo,
			Type:    mygo.MenuItemCheckbox,
			Checked: p.cur != nil && repo == p.cur.repo,
			Click:   func(*mygo.MenuItem, *mygo.Window) { p.open(repo) },
		})
	}
	if len(items) > 0 {
		items = append(items, mygo.Separator())
	}
	items = append(items, &mygo.MenuItem{
		Label: "Open Folder…", Accelerator: "CmdOrCtrl+O",
		Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() },
	})
	menu := mygo.NewMenu(items)
	go menu.PopupAt(win, int(x), int(y)) // which waits for the menu to close
}

// label names the repository by its folder, and its parent's too where
// another recent one has the same name.
func (p *projects) label(repo string) string {
	name := path.Base(repo)
	for _, r := range p.recent {
		if r != repo && path.Base(r) == name {
			dir := path.Dir(repo)
			if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(dir, home) {
				dir = "~" + dir[len(home):]
			}
			return name + " — " + dir
		}
	}
	return name
}

// choose asks for a folder and opens the repository it is in, asking
// again while the folder is in none.
func (p *projects) choose() {
	var win *mygo.Window
	if p.cur != nil {
		win = p.cur.win
	}
	go func() {
		for {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Parent:      win,
				Title:       "Open Folder",
				ButtonLabel: "Open",
				Message:     "Choose a folder in a Git repository",
				Directory:   true,
			})
			if err != nil || len(paths) == 0 {
				return
			}
			if p.openFolder(paths[0], win) {
				return
			}
		}
	}()
}

// openFolder opens the repository the folder is in, from any goroutine,
// or warns that it is in none. It waits for the warning to be read.
func (p *projects) openFolder(dir string, parent *mygo.Window) bool {
	repo, err := git.RepoRoot(dir)
	if err == nil {
		mygo.App.WhenReady(func() { p.open(repo) })
		return true
	}
	mygo.Dialog.Message(mygo.MessageOptions{
		Parent:  parent,
		Type:    mygo.MessageWarning,
		Message: path.Base(dir) + " is not in a Git repository",
		Detail:  "Choose a folder inside a repository's work tree.",
	})
	return false
}

// menuItems are the items of the menu bar that follow the project with
// the focus: checked, named for what choosing them does, or disabled
// where it cannot.
type menuItems struct {
	changes, files, sidebar, history, info, viewed *menuItem
	find                                           *menuItem
	refresh, pull, ship, review, branch            *menuItem
	github, finder                                 *menuItem
}

// menuItem is an item of the menu bar, and what it last showed, so that
// the native menu changes only where the item does.
type menuItem struct {
	*mygo.MenuItem
	shownLabel    string
	shownDisabled bool
	shownChecked  bool
}

func newMenuItem(item *mygo.MenuItem) *menuItem {
	return &menuItem{MenuItem: item, shownLabel: item.Label, shownChecked: item.Checked}
}

// show sets the item's label, and whether it is enabled and checked.
func (m *menuItem) show(label string, enabled, checked bool) {
	if label != m.shownLabel {
		m.shownLabel = label
		m.SetLabel(label)
	}
	if !enabled != m.shownDisabled {
		m.shownDisabled = !enabled
		m.SetEnabled(enabled)
	}
	if checked != m.shownChecked {
		m.shownChecked = checked
		m.SetChecked(checked)
	}
}

// setMenu gives the app its menu bar, which holds every command of the
// window as Mac apps' does: the File menu's folders, Find in the Edit
// menu, the View menu's modes as Finder's views, ⌘1 and ⌘2, and its
// columns as AppKit's sidebar and inspector, ⌃⌘S and ⌃⌘I, and the
// Repository menu's work with the remote.
func (p *projects) setMenu() {
	// do runs fn on the project with the focus, and shows the menu bar
	// as it leaves it.
	do := func(fn func(a *app)) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			a := p.cur
			if a == nil {
				return
			}
			fn(a)
			p.syncMenu()
			if a.win != nil {
				a.win.Update(func() {}) // to draw what changed
			}
		}
	}
	item := func(label, key string, fn func(a *app)) *menuItem {
		return newMenuItem(&mygo.MenuItem{Label: label, Accelerator: key, Click: do(fn)})
	}
	m := &menuItems{
		changes: newMenuItem(&mygo.MenuItem{Label: "Changes", Type: mygo.MenuItemRadio, Checked: true, Accelerator: "Cmd+1",
			Click: do(func(a *app) { a.setMode(modeChanges) })}),
		files: newMenuItem(&mygo.MenuItem{Label: "Files", Type: mygo.MenuItemRadio, Accelerator: "Cmd+2",
			Click: do(func(a *app) { a.setMode(modeFiles) })}),
		sidebar: item("Hide Sidebar", "Ctrl+Cmd+S", func(a *app) { a.toggle(&a.showTree, &a.collapsedTree) }),
		history: item("Hide History", "Ctrl+Cmd+I", func(a *app) { a.toggle(&a.showHistory, &a.collapsedHistory) }),
		info:    item("Show Commit Info", "Cmd+I", func(a *app) { a.info.open = !a.info.open }),
		viewed: item("Mark as Viewed", "Alt+Cmd+V", func(a *app) {
			if d := a.diffOf[a.file]; d != nil {
				a.setRead(d, !d.read)
			}
		}),
		find:    item("Find…", "Cmd+F", func(a *app) { a.find(editor.Find) }),
		refresh: item("Refresh", "Cmd+R", func(a *app) { a.refresh() }),
		pull:    item("Pull", "Shift+Cmd+P", func(a *app) { a.startPull() }),
		ship: item("Ship", "Cmd+P", func(a *app) {
			if a.push.running {
				a.push.cancel()
				return
			}
			a.startPush()
		}),
		review: item("Review Changes", "Shift+Cmd+R", func(a *app) {
			if a.review.running {
				a.review.cancel()
				return
			}
			a.startReview(ui.Services{})
		}),
		branch: item("Switch Branch…", "Cmd+B", func(a *app) {
			a.openSwitcher()
			a.switcher.focus = true
		}),
		github: item("Open on GitHub", "", func(a *app) { mygo.Shell.OpenExternal(cmp.Or(a.branchURL, a.github)) }),
		finder: item("Show in Finder", "Alt+Cmd+R", func(a *app) { mygo.Shell.ShowItemInFolder(a.repo) }),
	}
	p.menu = m
	edit := []*mygo.MenuItem{
		{Role: mygo.RoleUndo}, {Role: mygo.RoleRedo},
		mygo.Separator(),
		{Role: mygo.RoleCut}, {Role: mygo.RoleCopy}, {Role: mygo.RolePaste}, {Role: mygo.RoleSelectAll},
		mygo.Separator(),
		{Label: "Find", Submenu: []*mygo.MenuItem{
			m.find.MenuItem,
			{Label: "Find Next", Accelerator: "Cmd+G", Click: do(func(a *app) { a.find(editor.FindNext) })},
			{Label: "Find Previous", Accelerator: "Shift+Cmd+G", Click: do(func(a *app) { a.find(editor.FindPrevious) })},
			{Label: "Use Selection for Find", Accelerator: "Cmd+E", Click: do(func(a *app) { a.find(editor.UseSelectionForFind) })},
			mygo.Separator(),
			{Label: "Filter Files", Accelerator: "Alt+Cmd+J", Click: do(func(a *app) { a.filterFiles() })},
			{Label: "Search Commits", Accelerator: "Alt+Cmd+F", Click: do(func(a *app) { a.searchHistory() })},
		}},
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Role: mygo.RoleAppMenu},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Open Folder…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() }},
			{Role: mygo.RoleOpenRecent},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Label: "Edit", Submenu: edit},
		{Label: "View", Submenu: []*mygo.MenuItem{
			m.changes.MenuItem, m.files.MenuItem,
			mygo.Separator(),
			m.sidebar.MenuItem, m.history.MenuItem,
			mygo.Separator(),
			m.info.MenuItem, m.viewed.MenuItem,
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Repository", Submenu: []*mygo.MenuItem{
			m.refresh.MenuItem,
			mygo.Separator(),
			m.pull.MenuItem, m.ship.MenuItem, m.review.MenuItem,
			mygo.Separator(),
			m.branch.MenuItem,
			mygo.Separator(),
			m.github.MenuItem, m.finder.MenuItem,
		}},
		{Role: mygo.RoleWindowMenu},
		{Role: mygo.RoleHelp},
	}))
	p.syncMenu()
}

// syncMenu shows in the menu bar the state of the project with the focus,
// where it changed: its items gray without one.
func (p *projects) syncMenu() {
	m, a := p.menu, p.cur
	if m == nil {
		return
	}
	if a == nil {
		for _, item := range []*menuItem{m.changes, m.files, m.sidebar, m.history, m.info, m.viewed, m.find,
			m.refresh, m.pull, m.ship, m.review, m.branch, m.github, m.finder} {
			item.show(item.shownLabel, false, item.shownChecked)
		}
		return
	}
	cm, hasCommit := a.current()
	busy := a.push.running || a.review.running || a.pull.running || a.switcher.running != ""
	m.changes.show("Changes", true, a.mode == modeChanges)
	m.files.show("Files", true, a.mode == modeFiles)
	m.sidebar.show(showOrHide(a.showTree, "Sidebar"), true, false)
	m.history.show(showOrHide(a.showHistory, "History"), true, false)
	m.info.show("Show Commit Info", hasCommit && cm.Hash != git.Worktree, a.info.open)
	d := a.diffOf[a.file]
	viewed := "Mark as Viewed"
	if d != nil && d.read {
		viewed = "Mark as Not Viewed"
	}
	m.viewed.show(viewed, a.mode == modeChanges && d != nil, false)
	m.find.show("Find…", a.findable(), false)
	m.refresh.show("Refresh", true, false)
	m.pull.show("Pull", a.behind > 0 && !busy, false)
	ship := "Ship"
	if a.push.running {
		ship = "Stop Shipping"
	}
	m.ship.show(ship, a.push.running || a.canPush && !busy, false)
	review := "Review Changes"
	if a.review.running {
		review = "Stop Review"
	}
	m.review.show(review, a.review.running || (a.reviewSource.ID != "" || a.reviewSourceErr != "") && !busy, false)
	m.branch.show("Switch Branch…", a.branch != "" && !busy, false)
	m.github.show("Open on GitHub", cmp.Or(a.branchURL, a.github) != "", false)
	m.finder.show("Show in Finder", true, false)
}

// showOrHide names the command that shows or hides a column, as AppKit's
// View menu does.
func showOrHide(shown bool, pane string) string {
	if shown {
		return "Hide " + pane
	}
	return "Show " + pane
}

// findable reports whether a text shows to find in: the file open, or
// the diff at the top of the changes.
func (a *app) findable() bool {
	if a.mode == modeFiles {
		return a.code != nil && !a.previewing()
	}
	d := a.diffOf[a.file]
	return d != nil && d.state != nil && !d.folded()
}

// find runs a command of finding in the text that shows, as the Edit
// menu's Find items do in the editor with the focus.
func (a *app) find(cmd editor.Command) {
	if !a.findable() {
		return
	}
	if a.mode == modeFiles {
		a.code.Run(cmd)
		return
	}
	a.diffOf[a.file].state.Run(cmd)
}

// restore opens the projects open as the app last quit, else the latest
// one, else the welcome window.
func (p *projects) restore() {
	open := p.readList("open")
	if len(open) == 0 && len(p.recent) > 0 {
		open = p.recent[:1]
	}
	if len(open) == 0 {
		p.showWelcome()
		return
	}
	// The one with the focus last, so that it is in front.
	for _, repo := range slices.Backward(open) {
		p.open(repo)
	}
}
