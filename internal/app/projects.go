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
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

const maxRecent = 12 // the projects the switcher lists

// projects are the repositories open, all in one window, which shows one
// of them at a time, the one the menu bar acts on, the others kept as the
// user left them. All of it runs on the main thread.
type projects struct {
	avatars *avatars
	apps    map[string]*app // the projects open, by repository
	cur     *app            // the project the window shows, nil for none
	recent  []string        // the repositories opened, the latest first
	groups  [][]string      // the repositories shown together (group.go)
	// folded are the repositories of groups whose files the sidebar hides
	// under their names (group.go).
	folded []string
	dir    string       // where recent and the projects open are kept, "" for nowhere
	inline bool         // no windows, the apps' loads on the UI thread, for tests
	menu   *menuItems   // the menu bar's items that follow the project, nil in tests
	win    *mygo.Window // the window, which shows the project shown; nil while closed and in tests
	bar    *menuBar     // the item in the menu bar, nil in tests
	badge  int          // the count on the Dock icon
	// next is the project of the group to show from the next pass on, as
	// the user chose a file of it in the sidebar.
	next *app
	// entries are the history's entry each repository showed last
	// (entries.go).
	entries map[string]string
	// views are how the repositories showed as they last closed
	// (views.go).
	views views
	// chosen are the user's choices, nil for the defaults (prefs), and
	// settings the Settings window, nil while closed.
	chosen   *settings
	settings *settingsWindow
	// quitting keeps the projects open as the app quits, for the next
	// launch to open them again.
	quitting bool
}

func newProjects() *projects {
	p := &projects{avatars: newAvatars(), apps: map[string]*app{}}
	if dir, err := os.UserConfigDir(); err == nil {
		p.dir = filepath.Join(dir, "peek")
		p.recent = p.readList("recent")
		p.groups = p.readGroups()
		p.folded = p.readList("folded")
		p.entries = readEntries(p.dir)
		p.views = readViews(p.dir)
		s := readSettings(p.dir)
		p.chosen = &s
	}
	return p
}

// readList reads the repositories listed in the file of the app's folder
// that still exist; nil for none or no file.
func (p *projects) readList(name string) []string {
	var repos []string
	for _, repo := range p.readLines(name) {
		if exists(repo) {
			repos = append(repos, repo)
		}
	}
	return repos
}

// readLines reads the lines of the file of the app's folder that hold
// anything; nil for none or no file.
func (p *projects) readLines(name string) []string {
	data, err := os.ReadFile(filepath.Join(p.dir, name))
	if err != nil {
		return nil
	}
	var lines []string
	for _, line := range strings.Split(string(data), "\n") {
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
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

// setHideInactive chooses whether Peek hides when inactive.
func (p *projects) setHideInactive(hide bool) {
	s := p.prefs()
	s.HideInactive = hide
	p.setPrefs(s)
}

// setMenuBarOn chooses whether Peek's item shows in the menu bar, in
// place of its Dock icon. AppKit may deactivate the app as its icon goes
// or comes, so the window comes to the front again.
func (p *projects) setMenuBarOn(on bool) {
	s := p.prefs()
	s.MenuBar = on
	p.setPrefs(s)
	if !p.inline {
		mygo.App.Focus()
		if p.win != nil {
			p.win.Focus()
		}
	}
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// view shows the project shown, or the welcome while none is, the window's
// whole content: each project builds its own widgets, which keep their
// state apart, as the user left them.
func (p *projects) view(c *ui.Context) {
	if next := p.next; next != nil {
		p.next = nil
		p.open(next.repo)
		next.sidebarTree().toList = true // the sidebar keeps the keys
	}
	if p.cur == nil {
		p.welcome(c)
		p.syncMenu()
		return
	}
	a := p.cur
	ui.Column(c).Key(a.repo).Fill().Children(func() { a.view(c) })
	p.syncMenu()
	p.syncTitle()
	p.syncBadge()
}

// welcome is the window before any project opens, as a Mac app's welcome
// window: what to do, the way to open a folder, and the recent projects.
func (p *projects) welcome(c *ui.Context) {
	t := c.Theme()
	if p.win != nil && !p.inline {
		p.win.SetToolbar(nil) // the projects' toolbar, none open
	}
	ui.Column(c).Fill().Background(t.Background).Children(func() {
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

// showWindow opens the window, where it was last, or brings it to the
// front: the one window, which every project shows in, switched from the
// project's name, as the user asks of Peek over one window a project.
func (p *projects) showWindow() {
	if p.inline {
		return
	}
	if p.win != nil {
		p.win.Focus()
		return
	}
	p.win = mygo.NewWindow(mygo.WindowOptions{
		Title:         "Peek",
		Width:         1440,
		Height:        900,
		MinWidth:      640,
		MinHeight:     400,
		StateKey:      "main",
		TitleBarStyle: mygo.TitleBarHidden,
		Content:       ui.View(p.view),
	})
	win := p.win
	win.OnClosed(func() {
		// Closing it closes the projects, as the app stays in the menu bar,
		// to show as they were as they open again.
		p.keepViews()
		for _, a := range p.apps {
			a.close()
		}
		if !p.quitting {
			p.apps, p.cur = map[string]*app{}, nil
			p.saveOpen()
		}
		if p.win == win {
			p.win = nil
		}
		p.syncMenu()
	})
}

// close stops what the project does in the background, as its window
// closes.
func (a *app) close() {
	a.closed = true
	a.invalidateDefinitions()
	a.closePackages()
	if a.push.cancel != nil {
		a.push.cancel()
	}
	a.review.stop()
	a.answer.stop()
	a.closeTerminal()
	if a.unwatch != nil {
		a.unwatch()
	}
}

// open shows the repository in the window, as the user left it if it was
// shown before, or anew, with the columns of the project shown, and puts
// it first among the recent ones.
func (p *projects) open(repo string) {
	p.showWindow()
	_, was := p.apps[repo]
	a := p.app(repo)
	if a != p.cur && p.cur != nil && slices.Contains(p.groupOf(repo), p.cur.repo) {
		a.takeLayout(p.cur) // the group's window as it is
	}
	if was && a != p.cur {
		a.refresh() // what changed since it last showed
	}
	p.cur = a
	if p.win != nil {
		p.win.SetTitle(path.Base(repo))
		p.win.SetRepresentedFile(repo) // its folder the window's, as a document's file is
		p.win.Update(func() {})
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
		mygo.App.AddRecentDocument(repo) // File ▸ Open Recent
		p.saveOpen()
	}
}

// app returns the project of the repository, opening it, with the
// columns of the project shown, where it is not open.
func (p *projects) app(repo string) *app {
	if a := p.apps[repo]; a != nil {
		return a
	}
	a := newApp(repo)
	a.projects, a.avatars, a.inline, a.win = p, p.avatars, p.inline, p.win
	a.convFile = conversationsFile(p.dir, repo)
	a.loadConversations()
	if cur := p.cur; cur != nil { // the columns as they are
		a.showHistory, a.showTree = cur.showHistory, cur.showTree
		a.historyWidth, a.treeWidth = cur.historyWidth, cur.treeWidth
		a.terminal.shown, a.terminal.height = cur.terminal.shown, cur.terminal.height
		a.split = cur.split
	} else {
		a.split = p.prefs().SideBySide
		if l := p.views.Layout; l != nil { // the columns as the window last closed
			a.showHistory, a.showTree = l.History, l.Tree
			a.historyWidth, a.treeWidth = cmp.Or(l.HistoryWidth, a.historyWidth), cmp.Or(l.TreeWidth, a.treeWidth)
			a.terminal.shown, a.terminal.height = l.Terminal, cmp.Or(l.TerminalHeight, a.terminal.height)
		}
	}
	a.restoreView(p.views.Repos[repo])
	p.apps[repo] = a
	if !p.inline {
		a.shot = shotFromEnv()
		a.watch()
	}
	a.loadCommits()
	return a
}

// saveOpen keeps the projects open, the one shown first, for the next
// launch to open them again.
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
	}, &mygo.MenuItem{
		Label: "Add Folder…", Accelerator: "Shift+CmdOrCtrl+O",
		Click: func(*mygo.MenuItem, *mygo.Window) { p.chooseToAdd() },
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
	win := p.win
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
	return p.openWith(dir, parent, p.open)
}

// addFolder shows the repository the folder is in with the project shown,
// as openFolder opens it.
func (p *projects) addFolder(dir string, parent *mygo.Window) bool {
	return p.openWith(dir, parent, p.add)
}

func (p *projects) openWith(dir string, parent *mygo.Window, open func(repo string)) bool {
	repo, err := git.RepoRoot(dir)
	if err == nil {
		mygo.App.WhenReady(func() { open(repo) })
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
	changes, files, sidebar, history, viewed  *menuItem
	terminal                                  *menuItem
	layout, nextChange, previousChange        *menuItem
	find                                      *menuItem
	definition, back, forward                 *menuItem
	refresh, pull, ship, ask, review, explain *menuItem
	branch, discard                           *menuItem
	github, finder                            *menuItem
	add                                       *menuItem
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
// columns as AppKit's sidebar and inspector, ⌃⌘S and ⌃⌘I, the terminal
// under the text, ⌘J, and the Repository menu's work with the remote.
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
			if p.win != nil {
				p.win.Update(func() {}) // to draw what changed
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
		sidebar:  item("Hide Sidebar", "Ctrl+Cmd+S", func(a *app) { a.toggle(&a.showTree, &a.collapsedTree) }),
		history:  item("Hide History", "Ctrl+Cmd+I", func(a *app) { a.toggle(&a.showHistory, &a.collapsedHistory) }),
		terminal: item("Show Terminal", "Cmd+J", func(a *app) { a.toggleTerminal() }),
		viewed: item("Mark as Viewed", "Alt+Cmd+V", func(a *app) {
			if d := a.diffOf[a.file]; d != nil {
				a.setRead(d, !d.read)
			}
		}),
		layout:         item("Show Side by Side", "Ctrl+Cmd+D", func(a *app) { a.split = !a.split }),
		nextChange:     item("Next Change", "Ctrl+Cmd+Down", func(a *app) { a.goChange(1) }),
		previousChange: item("Previous Change", "Ctrl+Cmd+Up", func(a *app) { a.goChange(-1) }),
		find:           item("Find…", "Cmd+F", func(a *app) { a.find(editor.Find) }),
		definition: item("Go to Definition", "F12", func(a *app) {
			if a.canDefine() {
				a.goDefinition(a.code.Cursor())
			}
		}),
		back:    item("Go Back", "Ctrl+Cmd+Left", func(a *app) { a.goBack(-1) }),
		forward: item("Go Forward", "Ctrl+Cmd+Right", func(a *app) { a.goBack(1) }),
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
				a.review.stop()
				return
			}
			a.startReview("")
		}),
		explain: item("Explain Changes", "Shift+Cmd+E", func(a *app) {
			if a.answer.running {
				a.answer.stop()
				return
			}
			a.startAnswer(askTemplates[0].name, askTemplates[0].request)
		}),
		ask: item("New Conversation", "Shift+Cmd+A", func(a *app) { a.newDraft() }),
		branch: item("Switch Branch…", "Cmd+B", func(a *app) {
			a.openSwitcher()
			a.switcher.focus = true
		}),
		discard: item("Discard All Changes…", "", func(a *app) { a.confirmDiscard(a.diffs) }),
		github:  item("Open on GitHub", "", func(a *app) { mygo.Shell.OpenExternal(a.githubPage()) }),
		finder:  item("Show in Finder", "Alt+Cmd+R", func(a *app) { mygo.Shell.ShowItemInFolder(a.repo) }),
		add:     item("Add Folder…", "Shift+Cmd+O", func(*app) { p.chooseToAdd() }),
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
		}},
	}
	mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		// The app menu as AppKit's, with Check for Updates… after About, as
		// Sparkle puts it, then Settings… (⌘,).
		{Label: "Peek", Submenu: []*mygo.MenuItem{
			{Role: mygo.RoleAbout},
			updater.MenuItem(),
			mygo.Separator(),
			{Label: "Settings…", Accelerator: "Cmd+,", Click: func(*mygo.MenuItem, *mygo.Window) { p.showSettings() }},
			mygo.Separator(),
			{Label: "Install Command Line Tool…", Click: func(_ *mygo.MenuItem, w *mygo.Window) { go installCLI(w) }},
			mygo.Separator(),
			{Role: mygo.RoleServices},
			mygo.Separator(),
			{Role: mygo.RoleHide}, {Role: mygo.RoleHideOthers}, {Role: mygo.RoleUnhide},
			mygo.Separator(),
			{Role: mygo.RoleQuit},
		}},
		{Label: "File", Submenu: []*mygo.MenuItem{
			{Label: "Open Folder…", Accelerator: "CmdOrCtrl+O", Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() }},
			m.add.MenuItem,
			{Role: mygo.RoleOpenRecent},
			mygo.Separator(),
			{Role: mygo.RoleClose},
		}},
		{Label: "Edit", Submenu: edit},
		{Label: "View", Submenu: []*mygo.MenuItem{
			m.changes.MenuItem, m.files.MenuItem,
			mygo.Separator(),
			m.sidebar.MenuItem, m.history.MenuItem, m.terminal.MenuItem,
			mygo.Separator(),
			m.layout.MenuItem,
			mygo.Separator(),
			m.nextChange.MenuItem, m.previousChange.MenuItem,
			m.viewed.MenuItem,
			mygo.Separator(),
			{Role: mygo.RoleToggleFullScreen},
		}},
		{Label: "Repository", Submenu: []*mygo.MenuItem{
			m.refresh.MenuItem,
			mygo.Separator(),
			m.pull.MenuItem, m.ship.MenuItem,
			mygo.Separator(),
			m.discard.MenuItem,
			mygo.Separator(),
			m.ask.MenuItem, m.review.MenuItem, m.explain.MenuItem,
			mygo.Separator(),
			m.branch.MenuItem,
			mygo.Separator(),
			m.github.MenuItem, m.finder.MenuItem,
		}},
		{Label: "Navigate", Submenu: []*mygo.MenuItem{
			m.definition.MenuItem, mygo.Separator(), m.back.MenuItem, m.forward.MenuItem,
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
		for _, item := range []*menuItem{m.changes, m.files, m.sidebar, m.history, m.terminal, m.viewed, m.find, m.layout, m.nextChange, m.previousChange,
			m.refresh, m.pull, m.ship, m.discard, m.ask, m.review, m.explain, m.branch, m.github, m.finder, m.definition, m.back, m.forward, m.add} {
			item.show(item.shownLabel, false, item.shownChecked)
		}
		return
	}
	busy := a.push.running || a.review.running || a.pull.running || a.switcher.running != ""
	m.changes.show("Changes", true, a.mode == modeChanges)
	m.files.show("Files", true, a.mode == modeFiles)
	m.sidebar.show(showOrHide(a.showTree, "Sidebar"), true, false)
	m.history.show(showOrHide(a.showHistory, "History"), true, false)
	m.terminal.show(showOrHide(a.terminal.shown, "Terminal"), true, false)
	d := a.diffOf[a.file]
	viewed := "Mark as Viewed"
	if d != nil && d.read {
		viewed = "Mark as Not Viewed"
	}
	m.viewed.show(viewed, a.mode == modeChanges && d != nil, false)
	layout := "Show Side by Side"
	if a.split {
		layout = "Show Unified"
	}
	diffs := a.mode == modeChanges && len(a.diffs) > 0
	m.layout.show(layout, a.mode == modeChanges, false)
	m.nextChange.show("Next Change", diffs, false)
	m.previousChange.show("Previous Change", diffs, false)
	m.find.show("Find…", a.findable(), false)
	m.definition.show("Go to Definition", a.canDefine(), false)
	m.back.show("Go Back", a.readableSource() && !a.navigation.busy && len(a.navigation.back) > 0, false)
	m.forward.show("Go Forward", a.readableSource() && !a.navigation.busy && len(a.navigation.forward) > 0, false)
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
	target, targetErr := a.reviewTarget()
	m.review.show(review, a.review.running || (target.ID != "" || targetErr != "") && !busy && !a.answer.running, false)
	explain := "Explain Changes"
	if a.answer.running {
		explain = "Stop Codex"
	}
	_, answerable, _ := a.answerable()
	m.explain.show(explain, a.answer.running || answerable != "" && !busy && !a.review.running, false)
	m.discard.show("Discard All Changes…", a.discardable(), false)
	m.ask.show("New Conversation", true, false)
	m.branch.show("Switch Branch…", a.branch != "" && !busy, false)
	m.github.show("Open on GitHub", a.githubPage() != "", false)
	m.finder.show("Show in Finder", true, false)
	m.add.show("Add Folder…", true, false)
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
// one, else the welcome window, the welcome window alone where the user
// chose so.
func (p *projects) restore() {
	if !p.prefs().Reopen {
		p.showWindow() // the welcome
		return
	}
	open := p.readList("open")
	if len(open) == 0 && len(p.recent) > 0 {
		open = p.recent[:1]
	}
	if len(open) == 0 {
		p.showWindow()
		return
	}
	// The one shown last, so that it shows.
	for _, repo := range slices.Backward(open) {
		p.open(repo)
	}
}
