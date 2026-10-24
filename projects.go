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
