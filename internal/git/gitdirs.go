package git

import (
	"path/filepath"
	"strings"
)

// GitDirs are the folders holding a repository's git files outside its
// work tree, which watching the work tree misses: a linked work tree's
// own (its index and HEAD) and the one it shares with the main checkout
// (its branches), where a commit made in it lands.
type GitDirs struct {
	// Common is the shared folder, "" where it is in the work tree, as a
	// main checkout's .git.
	Common string
	// Own is the work tree's folder in Common, slashed and relative to it,
	// as "worktrees/name"; "" where it is Common itself.
	Own string
	// Apart is the work tree's own folder where it is outside Common too,
	// as git's --separate-git-dir leaves it; else "".
	Apart string
}

// ReadGitDirs finds the git folders of the repository outside its work
// tree.
func ReadGitDirs(repo string) GitDirs {
	out, err := git(repo, "rev-parse", "--path-format=absolute", "--git-dir", "--git-common-dir")
	if err != nil {
		return GitDirs{}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != 2 {
		return GitDirs{}
	}
	own, common := resolved(lines[0]), resolved(lines[1])
	root := resolved(repo)
	var d GitDirs
	if !within(common, root) {
		d.Common = common
	}
	if rel, ok := relWithin(own, common); ok {
		if d.Common != "" && rel != "." {
			d.Own = rel
		}
	} else if !within(own, root) {
		d.Apart = own
	}
	return d
}

func resolved(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return filepath.Clean(p)
}

// within reports whether p is dir or in it.
func within(p, dir string) bool {
	_, ok := relWithin(p, dir)
	return ok
}

// relWithin returns p relative to dir, slashed, where p is dir or in it.
func relWithin(p, dir string) (string, bool) {
	rel, err := filepath.Rel(dir, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}
