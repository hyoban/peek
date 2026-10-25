package app

import (
	"context"
	"peek/internal/git"
	"peek/internal/review"
	"time"

	"github.com/egoist/mygo"
)

// history is what the history column lists, read together.
type history struct {
	commits   []git.Commit // the work tree's entry first
	changes   []git.Change // the work tree's
	branch    string
	github    string // the repository's page on GitHub, "" for none
	canPush   bool
	behind    int    // the upstream's commits the branch has not
	upstream  string // the upstream's name, where it is behind
	review    review.Snapshot
	reviewErr string
	err       error
}

func readHistory(repo string) history {
	commits, err := git.LoadCommits(repo, 500)
	changes, _ := git.LoadWorktreeChanges(repo)
	// The work tree's entry always shows, for its files to read as they
	// are on disk, with changes or none.
	summary := "No changes"
	if n := len(changes); n > 0 {
		summary = count(n, "file") + " changed"
	}
	wt := git.Commit{Hash: git.Worktree, Subject: "Uncommitted Changes", Summary: summary}
	commits = append([]git.Commit{wt}, commits...)
	review, reviewErr := review.ReadSnapshot(repo)
	behind, upstream := git.BehindUpstream(repo)
	var reviewNote string
	if reviewErr != nil {
		reviewNote = reviewErr.Error()
	}
	return history{commits: commits, changes: changes, branch: git.CurrentBranch(repo), github: git.GitHubURL(repo),
		canPush: len(changes) > 0 || git.HasUnpushedCommits(repo), behind: behind, upstream: upstream, review: review, reviewErr: reviewNote, err: err}
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
	a.commits, a.branch, a.github, a.err = h.commits, h.branch, h.github, ""
	a.canPush, a.behind, a.upstream = h.canPush, h.behind, h.upstream
	if h.behind == 0 {
		a.pull.err = "" // up to date after all, as pulled elsewhere
	}
	a.updateReviewSource(h.review, h.reviewErr)
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
	if shown == git.Worktree {
		a.updateChanges(h.changes)
		if filesShown {
			a.reloadFiles()
		}
	}
}

// changesByPath maps the changes by their new paths, and lists those.
func changesByPath(changes []git.Change) (map[string]git.Change, []string) {
	byPath := make(map[string]git.Change, len(changes))
	paths := make([]string, len(changes))
	for i, ch := range changes {
		byPath[ch.Path], paths[i] = ch, ch.Path
	}
	return byPath, paths
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
// the list and the tree change only where the files changed are others,
// not where only their line counts are.
func (a *app) updateChanges(changes []git.Change) {
	same := len(changes) == len(a.changes)
	for _, ch := range changes {
		if a.changes[ch.Path].Shape() != ch.Shape() {
			same = false
		}
	}
	if same {
		for _, ch := range changes {
			a.changes[ch.Path] = ch
			if d := a.diffOf[ch.Path]; d != nil {
				d.change = ch
			}
		}
	} else {
		old := map[string]*fileDiff{}
		for _, d := range a.diffs {
			old[d.change.Path] = d
		}
		var paths []string
		a.changes, paths = changesByPath(changes)
		a.changeTree.set(foldGrouped(paths))
		a.openChanges()
		a.diffs = nil
		for _, p := range a.changeTree.files() {
			d := old[p]
			if d == nil || d.change.Shape() != a.changes[p].Shape() {
				d = a.newDiff(a.changes[p])
			}
			d.change = a.changes[p]
			a.diffs = append(a.diffs, d)
		}
		a.indexDiffs()
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
	open, md := a.openFile, a.preview
	a.background(func() func() {
		files, err := git.ListFiles(a.repo, git.Worktree)
		tree := fold(files)
		var text string
		var textErr error
		if open != "" {
			text, textErr = git.FileText(a.repo, git.Worktree, open)
		}
		picture, note := fileView(open, text, textErr)
		// The preview shows anew where the text or its pictures changed.
		stale := md != nil && md.stale(text)
		return func() {
			if cm, ok := a.current(); err != nil || !ok || cm.Hash != git.Worktree || a.filesFor != a.loadedRow {
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
				a.openFile, a.code, a.picture, a.codeNote, a.preview = "", nil, nil, "", nil // deleted
				return
			}
			a.allTree.show(open)
			switch {
			case note != "" || picture != nil:
				a.code, a.picture, a.codeNote, a.preview = nil, picture, note, nil
			case a.code != nil:
				resetText(a.code.Buffer, text)
				if stale && a.preview == md {
					a.preview = a.newPreview(git.Worktree, open, text)
				}
			default:
				a.code = newCode(open, text)
				a.picture, a.codeNote, a.preview = nil, "", nil
				if isMarkdown(open) {
					a.preview = a.newPreview(git.Worktree, open, text)
				}
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
	// The remote, at once and every while, for its new commits.
	go func() {
		tick := time.NewTicker(fetchEvery)
		defer tick.Stop()
		for {
			win.Update(func() {
				if p.cur == a {
					a.fetchRemote()
				}
			})
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
			}
		}
	}()
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
