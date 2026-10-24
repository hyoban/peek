package main

import (
	"context"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

// history is what the history column lists, read together.
type history struct {
	commits []commit // the work tree's entry first
	changes []change // the work tree's
	branch  string
	err     error
}

func readHistory(repo string) history {
	commits, err := loadCommits(repo, 500)
	changes, _ := loadWorktreeChanges(repo)
	// The work tree's entry always shows, for its files to read as they
	// are on disk, with changes or none.
	summary := "No changes"
	if n := len(changes); n > 0 {
		summary = count(n, "file") + " changed"
	}
	wt := commit{Hash: worktree, Subject: "Uncommitted Changes", Summary: summary}
	commits = append([]commit{wt}, commits...)
	return history{commits: commits, changes: changes, branch: currentBranch(repo), err: err}
}

// refresh reads the history and the work tree again, as the watch asks
// once files change, keeping what shows where it still is.
func (a *app) refresh() {
	if a.refreshing {
		a.refreshAgain = true
		return
	}
	a.refreshing = true
	a.background(func() func() {
		h := readHistory(a.repo)
		return func() {
			a.refreshing = false
			if a.refreshAgain {
				a.refreshAgain = false
				defer a.refresh()
			}
			if h.err == nil {
				a.applyHistory(h)
			}
		}
	})
}

// applyHistory lists the history read again: the commit chosen and the
// one shown stay chosen and shown where they still are, and the work
// tree's changes, where they show, follow the files.
func (a *app) applyHistory(h history) {
	chosen, wasChosen := a.hashAt(a.commitRow)
	shown, wasShown := a.hashAt(a.loadedRow)
	filesShown := a.filesFor >= 0 && a.filesFor == a.loadedRow
	a.commits, a.branch, a.err = h.commits, h.branch, ""
	a.commitRow = -1
	if len(h.commits) > 0 {
		a.commitRow = 0
	}
	if row := a.rowOf(chosen); wasChosen && row >= 0 {
		a.commitRow = row
	}
	if !wasShown {
		a.worktreeChanges = h.changes
		return
	}
	row := a.rowOf(shown)
	a.loadedRow, a.filesFor = row, -1
	if row < 0 {
		return // gone, as a commit amended: sync loads the chosen
	}
	if filesShown {
		a.filesFor = row
	}
	if shown == worktree {
		a.updateChanges(h.changes)
		if filesShown {
			a.reloadFiles()
		}
	}
}

func (a *app) hashAt(row int) (string, bool) {
	if row < 0 || row >= len(a.commits) {
		return "", false
	}
	return a.commits[row].Hash, true
}

func (a *app) rowOf(hash string) int {
	for i, cm := range a.commits {
		if cm.Hash == hash {
			return i
		}
	}
	return -1
}

// updateChanges shows the work tree's changes read again: the diffs of
// the files changed before reload as they come into view, in place, and
// the list and the tree change only where the files changed are others.
func (a *app) updateChanges(changes []change) {
	same := len(changes) == len(a.changes)
	for _, ch := range changes {
		if a.changes[ch.Path] != ch {
			same = false
		}
	}
	if !same {
		old := map[string]*fileDiff{}
		for _, d := range a.diffs {
			old[d.change.Path] = d
		}
		a.changes = map[string]change{}
		paths := make([]string, len(changes))
		for i, ch := range changes {
			a.changes[ch.Path] = ch
			paths[i] = ch.Path
		}
		a.changeTree.set(fold(paths))
		a.changeTree.openAll()
		a.diffs = nil
		for _, p := range a.changeTree.files() {
			d := old[p]
			if d == nil || d.change != a.changes[p] {
				d = &fileDiff{change: a.changes[p]}
			}
			a.diffs = append(a.diffs, d)
		}
		if _, ok := a.changes[a.file]; !ok {
			a.file = ""
			if len(a.diffs) > 0 {
				a.file = a.diffs[0].change.Path
			}
		}
		if a.file != "" {
			a.changeTree.show(a.file)
			a.scrollToFile(a.file)
		}
	}
	for _, d := range a.diffs {
		if d.state != nil || d.note != "" {
			d.stale = true
		}
	}
}

// reloadFiles lists the work tree's files again for the files mode,
// keeping the folders open and the file shown, its text where it is.
func (a *app) reloadFiles() {
	open := a.openFile
	a.background(func() func() {
		files, err := listFiles(a.repo, worktree)
		tree := fold(files)
		var text string
		var textErr error
		if open != "" {
			text, textErr = fileText(a.repo, worktree, open)
		}
		return func() {
			if cm, ok := a.current(); err != nil || !ok || cm.Hash != worktree || a.filesFor != a.loadedRow {
				return
			}
			var opened []string
			for dir := range a.allTree.state.Open.All() {
				opened = append(opened, dir)
			}
			a.allTree.set(tree)
			for _, dir := range opened {
				if a.allTree.isDir(dir) {
					a.allTree.state.Open.Add(dir)
				}
			}
			a.allTree.remember()
			if open == "" || a.openFile != open {
				return
			}
			if !a.allTree.has(open) {
				a.openFile, a.code, a.codeNote = "", nil, "" // deleted
				return
			}
			a.allTree.show(open)
			note := textNote(text)
			if textErr != nil {
				note = "Could not read this file"
			}
			switch {
			case note != "":
				a.code, a.codeNote = nil, note
			case a.code != nil:
				resetText(a.code.Buffer, text)
			default:
				a.code = &editor.State{Buffer: codeBuffer(open, text), ReadOnly: true, SoftWrap: true, Font: codeFont}
				a.codeNote = ""
			}
		}
	})
}

// watch refreshes the project shown as its files change, until another
// shows; changes meanwhile wait for the project to show again, which
// refreshes it then.
func (p *projects) watch(a *app) {
	if p.unwatch != nil {
		p.unwatch()
	}
	ctx, cancel := context.WithCancel(context.Background())
	p.unwatch = cancel
	win := p.win
	go func() {
		w, err := mygo.WatchFiles(ctx, a.repo, mygo.FileWatchOptions{
			Recursive: true,
			HintsOnly: true, // what changed is for git to say
			Ignore:    churn,
			Debounce:  300 * time.Millisecond,
			MaxDelay:  2 * time.Second,
		})
		if err != nil {
			return
		}
		defer w.Close()
		for {
			if _, err := w.Next(ctx); err != nil {
				return
			}
			win.Update(func() {
				if p.cur == a {
					a.refresh()
				}
			})
		}
	}()
}

// churn is what changes often without changing what the window shows:
// git's objects and logs, which a commit's refs and index tell of
// anyway, and installed packages.
func churn(p string, isDir bool) bool {
	switch p {
	case ".git/objects", ".git/logs", ".git/lfs", "node_modules":
		return true
	}
	return false
}
