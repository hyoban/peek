package main

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// pathTree is an outline of file paths: directories that hold only one
// directory fold into it, as editors' compact folders do, so that a deep
// path such as Java's or Kotlin's takes one row rather than a level each.
type pathTree struct {
	dirs   map[string][]string // a directory's entries, "" the root
	labels map[string]string   // the names the rows show, by path
	state  ui.OutlineState[string]
	row    int
	reveal string // the path to choose once its row shows
	// opened is what the app last opened: an item opened or closed
	// otherwise was so by the outline's arrow or keys, or a click on a
	// folder's row.
	opened map[string]bool
}

func newPathTree() pathTree { return pathTree{row: -1} }

// folded is a tree of paths, built off the UI thread for a pathTree.
type folded struct {
	dirs   map[string][]string
	labels map[string]string
}

// fold builds the tree of these files: directories first, then by name,
// each directory holding only a directory folded into it.
func fold(files []string) folded {
	raw := map[string][]string{}
	seen := map[string]bool{}
	for _, p := range files {
		for p != "" && !seen[p] {
			seen[p] = true
			dir := path.Dir(p)
			if dir == "." {
				dir = ""
			}
			raw[dir] = append(raw[dir], p)
			p = dir
		}
	}
	f := folded{dirs: map[string][]string{}, labels: map[string]string{}}
	var walk func(parent string)
	walk = func(parent string) {
		for _, p := range raw[parent] {
			for len(raw[p]) == 1 && raw[raw[p][0]] != nil {
				p = raw[p][0]
			}
			f.dirs[parent] = append(f.dirs[parent], p)
			f.labels[p] = strings.TrimPrefix(strings.TrimPrefix(p, parent), "/")
			if raw[p] != nil {
				walk(p)
			}
		}
	}
	walk("")
	for _, entries := range f.dirs {
		lower := make(map[string]string, len(entries))
		for _, e := range entries {
			lower[e] = strings.ToLower(e)
		}
		slices.SortFunc(entries, func(a, b string) int {
			if da, db := f.dirs[a] != nil, f.dirs[b] != nil; da != db {
				if da {
					return -1
				}
				return 1
			}
			return cmp.Compare(lower[a], lower[b])
		})
	}
	return f
}

// set replaces the tree with a folded one, its directories closed.
func (t *pathTree) set(f folded) {
	t.dirs, t.labels = f.dirs, f.labels
	t.state.Open.Clear()
	t.row, t.reveal = -1, ""
	t.remember()
}

func (t *pathTree) remember() {
	t.opened = map[string]bool{}
	for dir := range t.state.Open.All() {
		t.opened[dir] = true
	}
}

// arrowed returns the item last opened or closed other than by the app,
// the outermost where several were, as Option-click opens all inside, and
// remembers what is open now; "" for none.
func (t *pathTree) arrowed() string {
	var changed []string
	for dir := range t.state.Open.All() {
		if !t.opened[dir] {
			changed = append(changed, dir)
		}
	}
	if len(changed) == 0 && t.state.Open.Len() == len(t.opened) {
		return ""
	}
	for dir := range t.opened {
		if !t.state.Open.Has(dir) {
			changed = append(changed, dir)
		}
	}
	t.remember()
	if len(changed) == 0 {
		return ""
	}
	return slices.MinFunc(changed, func(a, b string) int { return len(a) - len(b) })
}

func (t *pathTree) isDir(p string) bool { return t.dirs[p] != nil }

// has reports whether the tree lists the file.
func (t *pathTree) has(p string) bool {
	_, ok := t.labels[p]
	return ok && !t.isDir(p)
}

func (t *pathTree) openAll() {
	for dir := range t.dirs {
		if dir != "" {
			t.state.Open.Add(dir)
		}
	}
	t.remember()
}

// files returns the tree's files in the order its rows list them, every
// directory open.
func (t *pathTree) files() []string {
	var files []string
	var walk func(dir string)
	walk = func(dir string) {
		for _, p := range t.dirs[dir] {
			if t.isDir(p) {
				walk(p)
			} else {
				files = append(files, p)
			}
		}
	}
	walk("")
	return files
}

// show opens the directories above the file and chooses it once its row
// shows.
func (t *pathTree) show(p string) {
	for dir := path.Dir(p); dir != "." && dir != "/"; dir = path.Dir(dir) {
		if t.isDir(dir) {
			t.state.Open.Add(dir)
		}
	}
	t.reveal = p
	t.remember()
}

// flip opens the directory or closes it, as its arrow does: arrowed then
// chooses it.
func (t *pathTree) flip(dir string) {
	if t.state.Open.Has(dir) {
		t.state.Open.Remove(dir)
	} else {
		t.state.Open.Add(dir)
	}
}

// rowOf returns the row of the item as the outline last built its rows,
// -1 where it does not show.
func (t *pathTree) rowOf(item string) int {
	for i := range t.state.Rows() {
		if t.state.Item(i) == item {
			return i
		}
	}
	return -1
}

// chosen returns the path of the row chosen, "" for none.
func (t *pathTree) chosen() string {
	if t.row < 0 || t.row >= t.state.Rows() {
		return ""
	}
	return t.state.Item(t.row)
}

// sync chooses the row of the path to reveal, after the outline built
// its rows; it reports whether it did.
func (t *pathTree) sync() bool {
	if t.reveal == "" {
		return false
	}
	i := t.rowOf(t.reveal)
	if i < 0 {
		return false
	}
	t.row, t.reveal = i, ""
	t.state.List.ScrollIntoView(i)
	return true
}

func (t *pathTree) view(c *ui.Context, label string, item func(p string, selected bool)) ui.Element {
	th := c.Theme()
	t.state.List.Selected = &t.row
	t.state.Indent = 12
	chosen := t.chosen()
	children := func(p string) []string { return t.dirs[p] }
	return ui.Outline(c, &t.state, t.dirs[""], children, func(p string) {
		item(p, p == chosen)
	}).Grow(1).FillWidth().Padding(th.Space(1.5), th.Space(2)).Label(label)
}
