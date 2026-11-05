package git

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// DoneWorktree is another work tree of the repository that its branch is
// done with: Gone where the remote deleted the branch it tracked, as
// GitHub does once a pull request merges, else merged into the branch
// pull requests go to.
type DoneWorktree struct {
	Dir, Branch string
	Gone        bool
}

// KeptWorktree is another work tree CleanableWorktrees leaves, and the
// Reason, for a note to give.
type KeptWorktree struct {
	Dir, Branch, Reason string // Branch is "" on a detached HEAD
}

// ErrWorktreeChanged is RemoveWorktree's where the work tree has changes
// not committed or files git does not track, which only force removes;
// a WorktreeChangedError says how many.
var ErrWorktreeChanged = errors.New("the worktree has changes")

// WorktreeChangedError is the files a work tree has changed, staged or
// not, and those git does not track.
type WorktreeChangedError struct{ Changed, Untracked int }

func (e *WorktreeChangedError) Is(target error) bool { return target == ErrWorktreeChanged }

func (e *WorktreeChangedError) Error() string {
	var parts []string
	if e.Changed > 0 {
		parts = append(parts, plural(e.Changed, "uncommitted change"))
	}
	if e.Untracked > 0 {
		parts = append(parts, plural(e.Untracked, "untracked file"))
	}
	if len(parts) == 0 {
		return "it has changes"
	}
	return "it has " + strings.Join(parts, " and ")
}

func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// worktreeChanges counts what git status lists of the work tree.
func worktreeChanges(ctx context.Context, dir string) *WorktreeChangedError {
	e := &WorktreeChangedError{}
	out, err := Run(ctx, dir, "status", "--porcelain", "--untracked-files=all")
	if err != nil {
		return e
	}
	for line := range strings.Lines(out) {
		if strings.HasPrefix(line, "??") {
			e.Untracked++
		} else if strings.TrimSpace(line) != "" {
			e.Changed++
		}
	}
	return e
}

// RemoveWorktree deletes the work tree's folder and git's record of it,
// keeping its branch; changes in it are lost only with force.
func RemoveWorktree(ctx context.Context, repo, dir string, force bool) error {
	args := []string{"worktree", "remove", dir}
	if force {
		// Twice, as a locked work tree takes.
		args = []string{"worktree", "remove", "--force", "--force", dir}
	}
	_, err := Run(ctx, repo, args...)
	switch {
	case err == nil:
		return nil
	case strings.Contains(err.Error(), "use --force"):
		return worktreeChanges(ctx, dir)
	case strings.Contains(err.Error(), "locked working tree"):
		if _, reason, ok := strings.Cut(err.Error(), "lock reason: "); ok {
			reason, _, _ = strings.Cut(reason, "\n")
			return fmt.Errorf("it’s locked: %s", strings.TrimSpace(reason))
		}
		return errors.New("it’s locked")
	}
	// What git said, less its fatal: as a reason to show.
	said := gitFailure(err).Error()
	for _, prefix := range []string{"fatal: ", "error: "} {
		said = strings.TrimPrefix(said, prefix)
	}
	return errors.New(said)
}

// PruneWorktrees forgets the work trees whose folders are gone.
func PruneWorktrees(ctx context.Context, repo string) error {
	_, err := Run(ctx, repo, "worktree", "prune")
	return gitFailure(err)
}

// CleanableWorktrees lists the repository's other work trees whose
// branches are done with: the remote they tracked deleted them, or they
// were pushed and are merged into the default branch. It keeps, saying
// why, those locked or detached, on the default branch, not merged, or
// never pushed, which a new branch with no commits of its own would look
// merged as. Neither lists the main work tree, repo's own, or one whose
// folder is gone, which PruneWorktrees forgets.
func CleanableWorktrees(ctx context.Context, repo string) ([]DoneWorktree, []KeptWorktree, error) {
	out, err := Run(ctx, repo, "worktree", "list", "--porcelain")
	if err != nil {
		return nil, nil, err
	}
	type entry struct {
		dir, branch, locked string
		skip, detached      bool
		lockedSet           bool
	}
	var entries []entry
	for i, block := range strings.Split(out, "\n\n") {
		var e entry
		for line := range strings.Lines(block) {
			key, value, _ := strings.Cut(strings.TrimRight(line, "\n"), " ")
			switch key {
			case "worktree":
				e.dir = value
			case "branch":
				e.branch = strings.TrimPrefix(value, "refs/heads/")
			case "locked":
				e.locked, e.lockedSet = value, true
			case "detached":
				e.detached = true
			case "prunable", "bare":
				e.skip = true
			}
		}
		if i == 0 || e.skip || e.dir == "" || SamePath(e.dir, repo) {
			continue // the main work tree is first
		}
		entries = append(entries, e)
	}
	if len(entries) == 0 {
		return nil, nil, nil
	}
	onto, ontoRef := defaultBase(repo)
	format := "--format=%(refname:lstrip=2)%09%(upstream)%09%(upstream:track)"
	out, err = Run(ctx, repo, "for-each-ref", format, "refs/heads")
	if err != nil {
		return nil, nil, err
	}
	type state struct{ pushed, gone, merged bool }
	branches := map[string]*state{}
	for line := range strings.Lines(out) {
		// Run trims the last line's empty fields, as its tabs.
		name, rest, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		upstream, track, _ := strings.Cut(rest, "\t")
		if name != "" {
			branches[name] = &state{pushed: upstream != "", gone: track == "[gone]"}
		}
	}
	if ontoRef != "" {
		out, err = Run(ctx, repo, "for-each-ref", "--merged="+ontoRef, "--format=%(refname:lstrip=2)", "refs/heads")
		if err != nil {
			return nil, nil, err
		}
		for _, name := range strings.Fields(out) {
			if s := branches[name]; s != nil {
				s.merged = true
			}
		}
	}
	var done []DoneWorktree
	var kept []KeptWorktree
	keep := func(e entry, reason string) {
		kept = append(kept, KeptWorktree{Dir: e.dir, Branch: e.branch, Reason: reason})
	}
	for _, e := range entries {
		s := branches[e.branch]
		switch {
		case e.lockedSet && e.locked != "":
			keep(e, "it’s locked: "+e.locked)
		case e.lockedSet:
			keep(e, "it’s locked")
		case e.detached || s == nil:
			keep(e, "it has no branch checked out")
		case e.branch == onto:
			keep(e, "it’s on the default branch")
		case s.gone || s.pushed && s.merged:
			done = append(done, DoneWorktree{Dir: e.dir, Branch: e.branch, Gone: s.gone})
		case !s.pushed:
			keep(e, "its branch was never pushed")
		case onto != "":
			keep(e, "its branch isn’t merged into "+onto)
		default:
			keep(e, "there’s no default branch to compare it with")
		}
	}
	return done, kept, nil
}

// defaultBase is the branch pull requests go to, as BranchBase chooses
// it for a branch with none of its own, and the ref to read it from: the
// remote's, else the local main or master.
func defaultBase(repo string) (name, ref string) {
	st := readRefState(repo)
	for _, remote := range st.baseRemotes() {
		if onto := st.defaultBranch(remote); onto != "" {
			return onto, "refs/remotes/" + remote + "/" + onto
		}
	}
	for _, name := range []string{"main", "master"} {
		if st.refs["refs/heads/"+name] {
			return name, "refs/heads/" + name
		}
	}
	return "", ""
}
