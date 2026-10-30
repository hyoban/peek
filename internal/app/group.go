package app

import (
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A group is repositories the window shows together, as the projects of
// Xcode's workspace: the sidebar lists the files of each under its name,
// and the one whose file is chosen shows its diffs, history, terminal and
// Codex, as the toolbar and the menu bar act on it. Each repository is in
// one group at most, kept in the app's folder for the next launch.

// groupOf returns the group the repository is in, nil for none.
func (p *projects) groupOf(repo string) []string {
	for _, g := range p.groups {
		if slices.Contains(g, repo) {
			return g
		}
	}
	return nil
}

// together returns the projects the sidebar lists with this one, in the
// order they were added, this one among them; nil where it shows alone.
func (a *app) together() []*app {
	if a.projects == nil {
		return nil
	}
	g := a.projects.groupOf(a.repo)
	if len(g) < 2 {
		return nil
	}
	members := make([]*app, 0, len(g))
	for _, repo := range g {
		members = append(members, a.projects.app(repo))
	}
	return members
}

// add shows the repository in the window together with the project shown,
// and chooses it, as File ▸ Add Folder… does; it opens it alone where no
// project shows.
func (p *projects) add(repo string) {
	cur := p.cur
	if cur == nil || cur.repo == repo {
		p.open(repo)
		return
	}
	if !slices.Contains(p.groupOf(cur.repo), repo) {
		p.ungroup(repo)
		var g []string
		if i := p.groupIndex(cur.repo); i >= 0 {
			g = append(p.groups[i], repo)
			p.groups[i] = g
		} else {
			p.groups = append(p.groups, []string{cur.repo, repo})
		}
		p.saveGroups()
	}
	p.open(repo)
}

// remove takes the repository out of its group, the window showing it
// alone again among the projects open; the group's next shows where it
// showed.
func (p *projects) remove(repo string) {
	g := p.groupOf(repo)
	if g == nil {
		return
	}
	p.ungroup(repo)
	p.saveGroups()
	if p.cur != nil && p.cur.repo == repo {
		i := slices.Index(g, repo)
		rest := slices.Delete(slices.Clone(g), i, i+1)
		p.open(rest[min(i, len(rest)-1)])
	}
}

// ungroup takes the repository out of its group, which goes where one
// repository is left.
func (p *projects) ungroup(repo string) {
	i := p.groupIndex(repo)
	if i < 0 {
		return
	}
	g := slices.DeleteFunc(slices.Clone(p.groups[i]), func(r string) bool { return r == repo })
	if len(g) < 2 {
		p.groups = slices.Delete(p.groups, i, i+1)
		return
	}
	p.groups[i] = g
}

func (p *projects) groupIndex(repo string) int {
	return slices.IndexFunc(p.groups, func(g []string) bool { return slices.Contains(g, repo) })
}

// readGroups reads the groups kept, a line each, its repositories apart
// by tabs, of those that still exist.
func (p *projects) readGroups() [][]string {
	var groups [][]string
	for _, line := range p.readLines("groups") {
		var g []string
		for _, repo := range strings.Split(line, "\t") {
			if repo != "" && exists(repo) && !slices.Contains(g, repo) {
				g = append(g, repo)
			}
		}
		if len(g) > 1 {
			groups = append(groups, g)
		}
	}
	return groups
}

func (p *projects) saveGroups() {
	lines := make([]string, len(p.groups))
	for i, g := range p.groups {
		lines[i] = strings.Join(g, "\t")
	}
	p.writeList("groups", lines)
}

// chooseToAdd asks for a folder and shows the repository it is in with
// the project shown, asking again while the folder is in none.
func (p *projects) chooseToAdd() {
	win := p.win
	go func() {
		for {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
				Parent:      win,
				Title:       "Add Folder",
				ButtonLabel: "Add",
				Message:     "Choose a folder in a Git repository to show in this window",
				Directory:   true,
			})
			if err != nil || len(paths) == 0 {
				return
			}
			if p.addFolder(paths[0], win) {
				return
			}
		}
	}()
}

// takeLayout gives the project the columns, the mode and the terminal of
// the one shown before it, as the projects of a group share the window.
func (a *app) takeLayout(from *app) {
	a.showHistory, a.showTree = from.showHistory, from.showTree
	a.collapsedHistory, a.collapsedTree = from.collapsedHistory, from.collapsedTree
	a.historyWidth, a.treeWidth = from.historyWidth, from.treeWidth
	a.terminal.shown, a.terminal.height = from.terminal.shown, from.terminal.height
	a.split = from.split
	a.setMode(from.mode)
}

// memberTrees lists in the sidebar the files of each repository of the
// group under its name, in the mode shown, filtered as the filter field
// asks: each repository's part as tall as the others'. A file chosen in
// another repository's shows that repository.
func (a *app) memberTrees(c *ui.Context, members []*app, key, label string) {
	t := c.Theme()
	filter := a.sidebarTree().filter
	for _, m := range members {
		tree := m.sidebarTree()
		if m != a {
			m.setMode(a.mode)
			m.loadChosen(c)
			tree.filter = filter
			tree.applyFilter()
		}
		folded := a.projects.isFolded(m.repo)
		member := ui.Column(c.Key("member:" + m.repo)).MinHeight(0).FillWidth()
		if folded {
			member.Shrink(0)
		} else {
			member.Grow(1).Basis(0)
		}
		member.Children(func() {
			heading := ui.Row(c).Shrink(0).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).
				Padding(t.Space(2), t.Space(4), t.Space(1), t.Space(4)).Tooltip(m.repo)
			// A click on the name folds the repository's files away, or
			// shows them again, as the sections of Finder's sidebar.
			heading.KeepFocus().OnClick(func() { a.projects.fold(m.repo, !folded) })
			heading.ContextMenu(func(menu *ui.Menu) {
				if menu.Item("Discard All Changes…").Disabled(!m.discardable()).Chosen() {
					m.confirmDiscard(m.diffs)
				}
				menu.Separator()
				if menu.Item("Remove from Window").Chosen() {
					a.projects.remove(m.repo)
				}
			})
			heading.Children(func() {
				chevron := ui.Icon(c, chevronIcon).Size(10, 10).Shrink(0).TextColor(t.TextMuted)
				if folded {
					chevron.Rotate(-90)
				}
				color := t.TextMuted
				if m == a {
					color = t.Text // the repository shown
				}
				ui.Text(c, a.projects.label(m.repo)).FontSize(smallSize).FontWeight(700).TextColor(color).
					SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				if m.mode == modeChanges && len(m.diffs) > 0 {
					added, deleted := sumLines(m.diffs)
					lineCounts(c, added, deleted, false)
				}
			})
			if folded {
				return
			}
			if m.err != "" {
				emptyNote(c, m.err)
				return
			}
			if m.mode == modeChanges && tree.dirs != nil && len(tree.dirs[""]) == 0 && tree.keep == nil {
				emptyNote(c, "No Changes")
				return
			}
			var pick func()
			if m != a {
				services := c.Services()
				pick = func() {
					a.projects.next = m
					services.Invalidate()
				}
			}
			m.treeBody(c, tree, key, label, pick)
		})
		if m != a && m.choseInSidebar() {
			a.projects.next = m
			c.Services().Invalidate()
		}
	}
}

// isFolded reports whether the sidebar hides the repository's files under
// its name.
func (p *projects) isFolded(repo string) bool {
	return slices.Contains(p.folded, repo)
}

// fold hides the repository's files in the sidebar under its name, or
// shows them again, kept for the next launch.
func (p *projects) fold(repo string, fold bool) {
	if p.isFolded(repo) == fold {
		return
	}
	if fold {
		p.folded = append(p.folded, repo)
	} else {
		p.folded = slices.DeleteFunc(p.folded, func(r string) bool { return r == repo })
	}
	p.writeList("folded", p.folded)
}

// choseInSidebar reports whether the user chose a file in the sidebar that
// the project does not show yet.
func (a *app) choseInSidebar() bool {
	tree := a.sidebarTree()
	p := tree.chosen()
	if p == "" || tree.isDir(p) {
		return false
	}
	if a.mode == modeFiles {
		return p != a.openFile
	}
	return p != a.file && a.commitRow == a.loadedRow
}
