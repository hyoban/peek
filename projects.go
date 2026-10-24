package main

import (
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const maxRecent = 12 // the projects the switcher lists

// projects is the window: the repositories opened in it, each kept as the
// user left it, and the one it shows. All of it runs on the UI thread.
type projects struct {
	win     *mygo.Window
	avatars *avatars
	apps    map[string]*app // by repository
	cur     *app            // nil until a repository opens
	recent  []string        // the repositories opened, the latest first
	file    string          // where recent is kept, "" for nowhere
	inline  bool            // the apps', for tests
	unwatch func()          // stops watching the project shown
	menu    *viewItems      // the View menu's, nil in tests
}

// viewItems are the items of the View menu that follow the window: the
// mode chosen, and the columns' items, named for what choosing them does.
type viewItems struct {
	changes, files, sidebar, history *mygo.MenuItem
	// shown is what the items last showed.
	mode                  int
	showTree, showHistory bool
}

// viewMenu is the View menu, which holds the window's commands and their
// shortcuts as Mac apps' do: the modes as Finder's views, ⌘1 and ⌘2, and
// the columns as AppKit's sidebar and inspector, ⌃⌘S and ⌃⌘I.
func (p *projects) viewMenu() *mygo.MenuItem {
	do := func(fn func(a *app)) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) {
			p.win.Update(func() {
				if p.cur != nil {
					fn(p.cur)
				}
				p.syncMenu() // a radio item checks itself as it is chosen
			})
		}
	}
	m := &viewItems{
		changes: &mygo.MenuItem{Label: "Changes", Type: mygo.MenuItemRadio, Checked: true, Accelerator: "Cmd+1",
			Click: do(func(a *app) { a.setMode(modeChanges) })},
		files: &mygo.MenuItem{Label: "Files", Type: mygo.MenuItemRadio, Accelerator: "Cmd+2",
			Click: do(func(a *app) { a.setMode(modeFiles) })},
		sidebar: &mygo.MenuItem{Label: "Hide Sidebar", Accelerator: "Ctrl+Cmd+S",
			Click: do(func(a *app) { a.showTree = !a.showTree })},
		history: &mygo.MenuItem{Label: "Hide History", Accelerator: "Ctrl+Cmd+I",
			Click: do(func(a *app) { a.showHistory = !a.showHistory })},
		showTree: true, showHistory: true,
	}
	p.menu = m
	return &mygo.MenuItem{Label: "View", Submenu: []*mygo.MenuItem{
		m.changes, m.files,
		mygo.Separator(),
		m.sidebar, m.history,
		mygo.Separator(),
		{Role: mygo.RoleToggleFullScreen},
	}}
}

// syncMenu shows in the View menu the state of the project shown, where it
// changed.
func (p *projects) syncMenu() {
	m, a := p.menu, p.cur
	if m == nil || a == nil {
		return
	}
	if a.mode != m.mode {
		m.mode = a.mode
		m.changes.SetChecked(a.mode == modeChanges)
		m.files.SetChecked(a.mode == modeFiles)
	}
	if a.showTree != m.showTree {
		m.showTree = a.showTree
		m.sidebar.SetLabel(showOrHide(a.showTree, "Sidebar"))
	}
	if a.showHistory != m.showHistory {
		m.showHistory = a.showHistory
		m.history.SetLabel(showOrHide(a.showHistory, "History"))
	}
}

// showOrHide names the command that shows or hides a column, as AppKit's
// View menu does.
func showOrHide(shown bool, pane string) string {
	if shown {
		return "Hide " + pane
	}
	return "Show " + pane
}

func newProjects() *projects {
	p := &projects{avatars: newAvatars(), apps: map[string]*app{}}
	if dir, err := os.UserConfigDir(); err == nil {
		p.file = filepath.Join(dir, "peek", "recent")
		if data, err := os.ReadFile(p.file); err == nil {
			for _, repo := range strings.Split(string(data), "\n") {
				if repo != "" && exists(repo) {
					p.recent = append(p.recent, repo)
				}
			}
		}
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func (p *projects) view(c *ui.Context) {
	if p.cur == nil {
		p.empty(c)
		return
	}
	// Each project builds its own widgets, which keep their state apart.
	ui.Column(c).Key(p.cur.repo).Fill().Children(func() { p.cur.view(c) })
	p.syncMenu()
}

// empty is the window before any repository opens.
func (p *projects) empty(c *ui.Context) {
	t := c.Theme()
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(titleBarHeight).Shrink(0).FillWidth().DragWindow()
		ui.Column(c).Grow(1).FillWidth().Center().Gap(t.Space(3)).Children(func() {
			ui.Text(c, "No Repository").FontSize(fontSize + 2).FontWeight(600)
			ui.Text(c, "Open a folder in a Git repository to see its history.").TextColor(t.TextMuted)
			ui.Button(c.Key("open"), "Open Folder…").OnClick(p.choose)
		})
	})
}

// open shows the repository, as the user left it if it was open before,
// and puts it first among the recent ones.
func (p *projects) open(repo string) {
	a := p.apps[repo]
	if a == nil {
		a = newApp(repo)
		a.win, a.projects, a.avatars, a.inline = p.win, p, p.avatars, p.inline
		if cur := p.cur; cur != nil { // the columns as they are
			a.showHistory, a.showTree = cur.showHistory, cur.showTree
			a.historyWidth, a.treeWidth = cur.historyWidth, cur.treeWidth
		}
		p.apps[repo] = a
		a.loadCommits()
	} else if a != p.cur {
		a.refresh() // what changed while another showed
	}
	if a != p.cur && p.win != nil {
		p.watch(a)
	}
	p.cur = a
	if p.win != nil {
		p.win.SetTitle("peek — " + path.Base(repo))
	}
	recent := []string{repo}
	for _, r := range p.recent {
		if r != repo && len(recent) < maxRecent {
			recent = append(recent, r)
		}
	}
	p.recent = recent
	if p.file != "" {
		data := []byte(strings.Join(recent, "\n") + "\n")
		go func() {
			os.MkdirAll(filepath.Dir(p.file), 0o755)
			os.WriteFile(p.file, data, 0o644)
		}()
	}
}

// switcher lists the recent projects in a menu at the point of the
// window, the one shown checked, and the way to open another.
func (p *projects) switcher(x, y float32) {
	var items []*mygo.MenuItem
	for _, repo := range p.recent {
		items = append(items, &mygo.MenuItem{
			Label:   p.label(repo),
			ToolTip: repo,
			Type:    mygo.MenuItemCheckbox,
			Checked: p.cur != nil && repo == p.cur.repo,
			Click: func(*mygo.MenuItem, *mygo.Window) {
				p.win.Update(func() { p.open(repo) })
			},
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
	go menu.PopupAt(p.win, int(x), int(y)) // which waits for the menu to close
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
			repo, err := repoRoot(paths[0])
			if err == nil {
				win.Update(func() { p.open(repo) })
				return
			}
			mygo.Dialog.Message(mygo.MessageOptions{
				Parent:  win,
				Type:    mygo.MessageWarning,
				Message: path.Base(paths[0]) + " is not in a Git repository",
				Detail:  "Choose a folder inside a repository's work tree.",
			})
		}
	}()
}
