package app

import (
	"cmp"
	"path"
	"slices"
	"strings"

	"peek/internal/filekind"
	"peek/internal/filenesting"

	"github.com/egoist/mygo/ui"
)

// pathTree is an outline of file paths: directories that hold only one
// directory fold into it, as editors' compact folders do, so that a deep
// path such as Java's or Kotlin's takes one row rather than a level each.
type pathTree struct {
	dirs    map[string][]string // a directory's entries, "" the root
	labels  map[string]string   // the names the rows show, by path
	parents map[string]string   // the directory each item is in
	nested  map[string]bool     // files with nested children, still selectable files
	state   ui.OutlineState[string]
	row     int
	reveal  string // the path to choose once its row shows
	filterState
}

// filterState is the tree's filter, as the field at the bottom of Xcode's
// navigator: the files whose paths hold each word of its query, and the
// folders they are in, open, the folders open before coming back as it
// clears.
type filterState struct {
	filter string              // the query of the field
	field  ui.SearchFieldState // the field, which the menu bar's Filter Files focuses
	focus  bool                // the field is to take the keys
	toList bool                // the tree is to take the keys, as Down in the field asks
	gen    int                 // counts the trees set, whose open folders differ
	// filtered is the query keep holds the items of, for the tree of
	// filteredGen; keep is nil without a query.
	filtered    string
	filteredGen int
	keep        map[string]bool
	// saved are the folders open as filtering began, in the tree of
	// savedGen.
	saved    []string
	savedGen int
}

func newPathTree() pathTree { return pathTree{row: -1, filterState: filterState{filteredGen: -1}} }

// folded is a tree of paths, built off the UI thread for a pathTree.
type folded struct {
	dirs    map[string][]string
	labels  map[string]string
	parents map[string]string
	nested  map[string]bool
}

// foldFiles adds file nesting to the Files navigator. Changes continues to
// use foldGrouped, keeping its file-kind groups and diff order unchanged.
func foldFiles(files []string) folded {
	f := fold(files)
	f.nested = map[string]bool{}
	children := map[string][]string{}
	for dir, entries := range f.dirs {
		var names []string
		paths := map[string]string{}
		for _, p := range entries {
			if f.dirs[p] == nil {
				name := path.Base(p)
				names = append(names, name)
				paths[name] = p
			}
		}
		parents := filenesting.Parents(names)
		var roots []string
		for _, p := range entries {
			if parent, ok := parents[path.Base(p)]; ok && f.dirs[p] == nil {
				parent = paths[parent]
				children[parent] = append(children[parent], p)
				f.parents[p] = parent
				f.nested[parent] = true
			} else {
				roots = append(roots, p)
			}
		}
		f.dirs[dir] = roots
	}
	for parent, entries := range children {
		f.dirs[parent] = entries
	}
	return f
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
	f := folded{dirs: map[string][]string{}, labels: map[string]string{}, parents: map[string]string{}}
	var walk func(parent string)
	walk = func(parent string) {
		for _, p := range raw[parent] {
			for len(raw[p]) == 1 && raw[raw[p][0]] != nil {
				p = raw[p][0]
			}
			f.dirs[parent] = append(f.dirs[parent], p)
			f.parents[p] = parent
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

// groupPrefix starts the items of a group of files and of the directories
// in it, which a directory of the same path in another group would share.
const groupPrefix = "\x00"

// foldGrouped builds the tree of these files in a group of each kind, as
// the diffs list them, where they are of more than one kind.
func foldGrouped(files []string) folded {
	byKind := map[filekind.Kind][]string{}
	for _, p := range files {
		k := filekind.Of(p)
		byKind[k] = append(byKind[k], p)
	}
	if len(byKind) < 2 {
		return fold(files)
	}
	f := folded{dirs: map[string][]string{}, labels: map[string]string{}, parents: map[string]string{}}
	for k, name := range filekind.Names {
		if byKind[filekind.Kind(k)] == nil {
			continue
		}
		group := groupPrefix + name
		g := fold(byKind[filekind.Kind(k)])
		key := func(p string) string {
			switch {
			case p == "":
				return group
			case g.dirs[p] != nil:
				return group + "/" + p
			}
			return p
		}
		f.dirs[""] = append(f.dirs[""], group)
		f.labels[group], f.parents[group] = name, ""
		for dir, entries := range g.dirs {
			for _, e := range entries {
				f.dirs[key(dir)] = append(f.dirs[key(dir)], key(e))
				f.parents[key(e)] = key(dir)
			}
		}
		for p, label := range g.labels {
			f.labels[key(p)] = label
		}
	}
	return f
}

// isGroup reports whether the item is a group of files rather than a
// directory or a file.
func isGroup(item string) bool {
	return strings.HasPrefix(item, groupPrefix) && !strings.Contains(item, "/")
}

// itemPath returns the path of the item, a directory in a group without
// the group's name; "" for a group.
func itemPath(item string) string {
	if !strings.HasPrefix(item, groupPrefix) {
		return item
	}
	_, p, _ := strings.Cut(item, "/")
	return p
}

// set replaces the tree with a folded one, its directories closed.
func (t *pathTree) set(f folded) {
	t.dirs, t.labels, t.parents = f.dirs, f.labels, f.parents
	t.nested = f.nested
	t.state.Open.Clear()
	t.row, t.reveal = -1, ""
	t.gen++
}

// replace replaces the tree with another of mostly the same paths, as
// another commit's files, keeping open the folders open in both and the
// item chosen chosen where the new tree has it, so that the rows show
// at once as they were rather than closed and unchosen for a pass.
func (t *pathTree) replace(f folded) {
	opened := slices.Collect(t.state.Open.All())
	saved := t.savedGen == t.gen
	chosen := t.chosen()
	if chosen == "" {
		chosen = t.reveal
	}
	t.set(f)
	for _, dir := range opened {
		if len(t.dirs[dir]) > 0 {
			t.state.Open.Add(dir)
		}
	}
	if saved {
		t.savedGen = t.gen // the folders to open again as the filter clears
	}
	if _, ok := t.labels[chosen]; ok {
		t.show(chosen)
	}
}

// choose chooses the item, its row known before the outline builds its
// rows anew, or once it shows; sync then scrolls to it.
func (t *pathTree) choose(p string) {
	t.row, t.reveal = slices.Index(t.rows(), p), p
}

// rows returns the items the outline shows, in its order: each folder's
// entries under it where it is open.
func (t *pathTree) rows() []string {
	var rows []string
	var walk func(dir string)
	walk = func(dir string) {
		for _, p := range t.entries(dir) {
			rows = append(rows, p)
			if t.state.Open.Has(p) {
				walk(p)
			}
		}
	}
	walk("")
	return rows
}

// applyFilter keeps the items the query asks for, once for each query and
// tree, opening the folders that hold them. The item chosen stays chosen
// where it shows, as the rows move under it.
func (t *pathTree) applyFilter() {
	q := strings.ToLower(strings.TrimSpace(t.filter))
	if q == t.filtered && t.filteredGen == t.gen {
		return
	}
	chosen := t.chosen()
	if chosen == "" {
		chosen = t.reveal
	}
	t.filtered, t.filteredGen = q, t.gen
	if q == "" {
		if t.keep == nil {
			return // a new tree, unfiltered
		}
		t.keep = nil
		if t.savedGen == t.gen {
			t.state.Open.Clear()
			for _, dir := range t.saved {
				t.state.Open.Add(dir)
			}
		}
		t.saved = nil
		t.row = -1
		if chosen != "" {
			t.show(chosen)
		}
		return
	}
	if t.keep == nil || t.savedGen != t.gen {
		t.saved, t.savedGen = slices.Collect(t.state.Open.All()), t.gen
	}
	words := strings.Fields(q)
	t.keep = map[string]bool{}
	var walk func(dir string) bool
	walk = func(dir string) bool {
		kept := false
		for _, p := range t.dirs[dir] {
			if !t.isDir(p) && pathMatches(itemPath(p), words) {
				t.keep[p], kept = true, true
			}
			if walk(p) {
				t.keep[p], kept = true, true
				t.state.Open.Add(p)
			}
		}
		return kept
	}
	walk("")
	t.choose(chosen)
}

// pathMatches reports whether the path holds each of the words, which are
// in lower case.
func pathMatches(p string, words []string) bool {
	p = strings.ToLower(p)
	for _, w := range words {
		if !strings.Contains(p, w) {
			return false
		}
	}
	return true
}

// entries are the items of the directory that show: those the filter
// keeps.
func (t *pathTree) entries(dir string) []string {
	if t.keep == nil {
		return t.dirs[dir]
	}
	var kept []string
	for _, p := range t.dirs[dir] {
		if t.keep[p] {
			kept = append(kept, p)
		}
	}
	return kept
}

// filterCount counts the files the filter keeps, and all of them.
func (t *pathTree) filterCount() (kept, all int) {
	words := strings.Fields(t.filtered)
	for _, p := range t.files() {
		all++
		if t.keep[p] && pathMatches(itemPath(p), words) {
			kept++
		}
	}
	return kept, all
}

func (t *pathTree) isDir(p string) bool { return t.dirs[p] != nil && !t.nested[p] }

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
}

// files returns the tree's files in the order its rows list them, every
// directory open.
func (t *pathTree) files() []string { return t.filesUnder("") }

// filesUnder returns the files the directory holds, in its subdirectories
// too, in the order the rows list them.
func (t *pathTree) filesUnder(dir string) []string {
	var files []string
	for _, p := range t.dirs[dir] {
		if !t.isDir(p) {
			files = append(files, p)
		}
		files = append(files, t.filesUnder(p)...)
	}
	return files
}

// show opens the directories above the file and chooses it, in the pass
// that builds its row.
func (t *pathTree) show(p string) {
	for dir, ok := t.parents[p]; ok && dir != ""; dir, ok = t.parents[dir] {
		t.state.Open.Add(dir)
	}
	t.choose(p)
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

func (t *pathTree) view(c *ui.Context, label string, item func(p string) ui.SourceListItem) ui.Element {
	t.state.List.Selected = &t.row
	t.state.Indent = 12
	if t.toList {
		t.toList = false
		if t.row < 0 {
			for i := range t.state.Rows() {
				if !t.isDir(t.state.Item(i)) {
					t.row = i
					break
				}
			}
		}
		t.state.List.Handle.Focus(c)
	}
	return ui.SourceList(c, &t.state, t.entries(""), t.entries, item).Label(label)
}
