package app

import (
	"context"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/review"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

// history is what the history column lists, read together.
type history struct {
	commits   []git.Commit // the work tree's entry first, then the branch's
	changes   []git.Change // the work tree's
	branch    string
	github    string // the repository's page on GitHub, "" for none
	branchURL string // the branch's page on GitHub, "" for none
	canPush   bool
	behind    int    // the upstream's commits the branch has not
	upstream  string // the upstream's name, where it is behind
	review    review.Snapshot
	reviewErr string
	// branchReview is the review of the branch's changes, none where it
	// has no entry.
	branchReview review.Snapshot
	err          error
}

func readHistory(repo string) history {
	// Each part runs git apart from the others, all at once, as gitSlots
	// allows: one after another, their processes added up to most of the
	// wait for a project to show.
	var (
		wg          sync.WaitGroup
		commits     []git.Commit
		err         error
		changes     []git.Change
		snapshot    review.Snapshot
		snapshotErr error
		r           git.BranchRange
		hasBranch   bool
		branch      string
		github      string
		branchURL   string
		unpushed    bool
		behind      int
		upstream    string
		published   git.Published
	)
	// The review's snapshot asks too, not to wait on a read of its own.
	hasUnpushed := sync.OnceValue(func() bool { return git.HasUnpushedCommits(repo) })
	wg.Go(func() { commits, err = git.LoadCommits(repo, 500) })
	wg.Go(func() { published = git.ReadPublished(repo, 500) })
	wg.Go(func() { changes, _ = git.LoadWorktreeChanges(repo) })
	wg.Go(func() { snapshot, snapshotErr = review.ReadSnapshotWith(repo, hasUnpushed) })
	wg.Go(func() { r, hasBranch = git.BranchBase(repo) })
	wg.Go(func() { branch = git.CurrentBranch(repo) })
	wg.Go(func() { github = git.GitHubURL(repo) })
	wg.Go(func() { branchURL = git.BranchGitHubURL(repo) })
	wg.Go(func() { unpushed = hasUnpushed() })
	wg.Go(func() { behind, upstream = git.BehindUpstream(repo) })
	wg.Wait()
	for i := range commits {
		cm := &commits[i]
		cm.Unpushed = published.Unpushed[cm.Hash]
		if cm.Hash == published.Hash {
			cm.Remote = published.Ref
		}
	}
	// The work tree's entry always shows, for its files to read as they
	// are on disk, with changes or none.
	summary := "No changes"
	if n := len(changes); n > 0 {
		summary = count(n, "file") + " changed"
	}
	wt := git.Commit{Hash: git.Worktree, Subject: "Uncommitted Changes", Summary: summary}
	entries := []git.Commit{wt}
	var branchReview review.Snapshot
	// The branch's changes since it left the branch it is to merge into,
	// as its pull request's diff has them, where it has commits of its own.
	if hasBranch {
		entries = append(entries, git.Commit{Hash: git.Range(r.Base, r.Head), Subject: "Branch Changes",
			Summary: count(r.Commits, "commit") + " since " + r.Onto,
			Body:    "The changes of the branch since it left " + r.Onto + ", at " + r.Base[:7]})
		branchReview = review.BranchSnapshot(branch, r)
	}
	commits = append(entries, commits...)
	var reviewNote string
	if snapshotErr != nil {
		reviewNote = snapshotErr.Error()
	}
	return history{commits: commits, changes: changes, branch: branch, github: github, branchURL: branchURL,
		canPush: len(changes) > 0 || unpushed, behind: behind, upstream: upstream, review: snapshot, reviewErr: reviewNote, branchReview: branchReview, err: err}
}

// refresh reads the history and the work tree again, as the watch asks
// once files change, keeping what shows where it still is.
func (a *app) refresh() {
	a.restartDefinitions()
	a.reread()
}

// reread is refresh for changes only git's own files have, as a fetch's
// or a commit's: the language servers read nothing of them.
func (a *app) reread() {
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
				defer a.reread()
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
	a.commits, a.branch, a.github, a.branchURL, a.err = h.commits, h.branch, h.github, h.branchURL, ""
	a.canPush, a.behind, a.upstream, a.dirty = h.canPush, h.behind, h.upstream, len(h.changes)
	a.updateReviewSource(h.review, h.reviewErr)
	a.updateBranchReview(h.branchReview)
	a.readPullRequest(false)
	a.commitRow = -1
	if len(h.commits) > 0 {
		a.commitRow = 0
	}
	if row := a.rowOf(chosen); wasChosen && row >= 0 {
		a.commitRow = row
	}
	// A branch just checked out shows what it changes, where it has
	// nothing uncommitted to show first.
	if a.landOnBranch && h.branch == a.branch {
		a.landOnBranch = false
		a.commitRow = landingRow(h)
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
	if now := h.commits[row].Hash; now != shown {
		a.reloadRange(now, filesShown)
	}
}

// landingRow is the row to show first: the branch's changes where it has
// them and nothing uncommitted, else the work tree's.
func landingRow(h history) int {
	if len(h.changes) == 0 && len(h.commits) > 1 && git.IsRange(h.commits[1].Hash) {
		return 1
	}
	return 0
}

// reloadRange shows the branch's changes read again once it moved, as a
// commit or a switch moves it, the diffs of the files in place as the
// work tree's.
func (a *app) reloadRange(hash string, filesShown bool) {
	a.background(func() func() {
		changes, err := git.LoadChanges(a.repo, hash)
		return func() {
			if cm, ok := a.current(); err != nil || !ok || cm.Hash != hash {
				return
			}
			a.updateChanges(changes)
			if filesShown {
				a.loadFiles(hash)
			}
		}
	})
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

// rowOf returns the row of the commit, or of the branch's changes for a
// range, which a commit or a switch moves.
func (a *app) rowOf(hash string) int {
	for i, cm := range a.commits {
		if cm.Hash == hash || git.IsRange(hash) && git.IsRange(cm.Hash) {
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
		known := a.changeTree.dirs
		a.changeTree.replace(foldGrouped(paths))
		a.openChanges(known)
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
		// The file shown stays where it is, as the list keys its rows by
		// path; one gone gives way to the first.
		if _, ok := a.changes[a.file]; !ok {
			a.file = ""
			if len(a.diffs) > 0 {
				a.file = a.diffs[0].change.Path
				a.changeTree.show(a.file)
				a.scrollToFile(a.file)
			}
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
	open, md, code := a.openFile, a.preview, a.code
	gen := a.textGen
	nest := a.prefs().NestFiles
	a.background(func() func() {
		files, err := git.ListFiles(a.repo, git.Worktree)
		tree := foldFiles(files, nest)
		var text string
		var textErr error
		if open != "" {
			text, textErr = sourceText(a.repo, git.Worktree, open, maxTextSize)
		}
		picture, note := fileView(open, text, textErr)
		// The text shown is compared here, and a new one's buffer made, a
		// long file's being long to read back and to make.
		same := code != nil && code.Buffer.String() == text
		var buf *editor.Buffer
		if note == "" && picture == nil && code == nil {
			buf = codeBuffer(open, text)
		}
		// The preview shows anew where the text or its pictures changed.
		stale := md != nil && md.stale(text)
		return func() {
			if cm, ok := a.current(); a.closed || err != nil || !ok || cm.Hash != git.Worktree || a.filesFor != a.loadedRow {
				return
			}
			a.allTree.replace(tree)
			if open == "" || a.openFile != open || gen != a.textGen {
				return
			}
			if !a.allTree.has(open) && !filepath.IsAbs(open) {
				a.openFile, a.code, a.picture, a.codeNote, a.preview = "", nil, nil, "", nil // deleted
				return
			}
			a.showFileInTree(open)
			switch {
			case note != "" || picture != nil:
				a.code, a.picture, a.codeNote, a.preview = nil, picture, note, nil
			case a.code != nil:
				if a.code != code || !same {
					resetText(a.code.Buffer, text)
				}
				a.codePath, a.codeHash = open, git.Worktree
				if stale && a.preview == md {
					a.preview = a.newPreview(git.Worktree, open, text)
				}
			default:
				if buf == nil {
					buf = codeBuffer(open, text)
				}
				a.code = codeOf(buf, a.prefs())
				a.codePath, a.codeHash = open, git.Worktree
				a.picture, a.codeNote, a.preview = nil, "", nil
				if isMarkdown(open) {
					a.preview = a.newPreview(git.Worktree, open, text)
				}
			}
		}
	})
}

// watch refreshes the project as its files change, and fetches its
// remote every while, until its window closes.
func (a *app) watch() {
	ctx, cancel := context.WithCancel(context.Background())
	a.unwatch = cancel
	win := a.win
	// The remote, at once and every while, for its new commits: each tick
	// asks whether the interval the user chose has passed.
	go func() {
		tick := time.NewTicker(fetchTick)
		defer tick.Stop()
		for {
			win.Update(a.fetchIfDue)
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
			events, err := w.Next(ctx)
			if err != nil {
				return
			}
			if ignoredOnly(a.repo, events) {
				continue
			}
			if gitOnly(events) {
				win.Update(a.reread)
			} else {
				win.Update(a.refresh)
			}
		}
	}()
}

// ignoredOnly reports whether the changes are all in folders git ignores,
// as a build's output, which change nothing the window shows: a build
// writing to them would else read the history again every while.
func ignoredOnly(repo string, events []mygo.FileEvent) bool {
	var dirs []string
	for _, ev := range events {
		if ev.Op != mygo.FileHint || ev.Path == "." || ev.Path == ".git" || strings.HasPrefix(ev.Path, ".git/") {
			return false
		}
		dirs = append(dirs, ev.Path)
	}
	return git.AllIgnored(repo, dirs)
}

// gitOnly reports whether the changes are all in .git.
func gitOnly(events []mygo.FileEvent) bool {
	for _, ev := range events {
		if ev.Path != ".git" && !strings.HasPrefix(ev.Path, ".git/") {
			return false
		}
	}
	return len(events) > 0
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
