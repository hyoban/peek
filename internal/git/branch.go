package git

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

// Branches are what the branch switcher lists: the local Branches, the
// latest committed to first, and the remote ones no local branch of the
// same name tracks yet, as remote/name; and where the Branches checked
// out in the repository's other work trees are, which git keeps from
// being checked out here too.
type Branches struct {
	Local, Remote []string
	Worktrees     map[string]string // by branch
}

func ListBranches(ctx context.Context, repo string) (Branches, error) {
	var b Branches
	// lstrip=2 is the branch's name itself, where :short makes heads/v1 of
	// one a tag shares its name with, which git switch takes for no branch.
	out, err := Run(ctx, repo, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)%09%(worktreepath)", "refs/heads")
	if err != nil {
		return b, err
	}
	have := map[string]bool{}
	b.Worktrees = map[string]string{}
	for line := range strings.Lines(out) {
		name, dir, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if name == "" {
			continue
		}
		b.Local = append(b.Local, name)
		have[name] = true
		if dir != "" && !SamePath(dir, repo) {
			b.Worktrees[name] = dir
		}
	}
	// %(refname:lstrip=2) is the name itself, as above, and keeps the
	// remote's name, which :short drops from origin/HEAD.
	out, err = Run(ctx, repo, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)", "refs/remotes")
	if err != nil {
		return b, err
	}
	for _, name := range strings.Fields(out) {
		_, branch, ok := strings.Cut(name, "/")
		if !ok || branch == "HEAD" || have[branch] {
			continue
		}
		have[branch] = true // the first remote's, where several have it
		b.Remote = append(b.Remote, name)
	}
	return b, nil
}

// SamePath reports whether the paths name one folder, through symbolic
// links, as macOS's /tmp is /private/tmp.
func SamePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// SwitchBranch checks out a local branch, or a remote one as a new local
// branch tracking it, carrying the work tree's changes along as git does,
// and refusing where they would be lost.
func SwitchBranch(ctx context.Context, repo, name string, remote bool) error {
	args := []string{"switch", name}
	if remote {
		// The ref in full, which no tag of the same name shadows.
		args = []string{"switch", "--track", "refs/remotes/" + name}
	}
	_, err := Run(ctx, repo, args...)
	return gitFailure(err)
}

// gitFailure is what git said of a failure, less the command and its
// exit status, for a note to read.
func gitFailure(err error) error {
	if err == nil {
		return nil
	}
	_, said, ok := strings.Cut(err.Error(), "\n")
	if said = strings.TrimSpace(said); !ok || said == "" {
		return err
	}
	return fmt.Errorf("%s", said)
}

// BranchRange is what the branch checked out changes since it left the
// branch it is to merge into, Onto: from Base, the merge base of the two,
// to Head, in Commits commits.
type BranchRange struct {
	Base, Head, Onto string
	Commits          int
}

// BranchBase returns the branch's range onto the branch its pull request
// is to merge into, or else the default branch of the repository pull
// requests go to, or else the local main or master: from the nearer merge
// base where the remote's and the local branch both are, as a local one
// ahead of its remote leaves. ok is false on that branch itself, on a
// detached HEAD, and where the branch has no commits of its own.
func BranchBase(repo string) (r BranchRange, ok bool) {
	g := func(args ...string) (string, error) {
		out, err := git(repo, args...)
		return strings.TrimSpace(out), err
	}
	branch, err := g("symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return r, false
	}
	if r.Head, err = g("rev-parse", "--verify", "HEAD"); err != nil {
		return r, false
	}
	var refs []string
	for _, remote := range baseRemotes(repo) {
		onto := pullBase(repo, branch, remote)
		if onto == "" {
			onto = remoteDefaultBranch(repo, remote)
		}
		if onto != "" {
			r.Onto, refs = onto, []string{"refs/remotes/" + remote + "/" + onto, "refs/heads/" + onto}
			break
		}
	}
	if r.Onto == "" {
		for _, name := range []string{"main", "master"} {
			if _, err := g("rev-parse", "--verify", "--quiet", "refs/heads/"+name); err == nil {
				r.Onto, refs = name, []string{"refs/heads/" + name}
				break
			}
		}
	}
	if r.Onto == "" || r.Onto == branch {
		return r, false
	}
	for _, ref := range refs {
		base, err := g("merge-base", ref, r.Head)
		if err != nil {
			continue // no such branch, or no history shared with it
		}
		out, err := g("rev-list", "--count", base+".."+r.Head)
		n, _ := strconv.Atoi(out)
		if err == nil && (r.Base == "" || n < r.Commits) {
			r.Base, r.Commits = base, n
		}
	}
	return r, r.Base != "" && r.Commits > 0
}

// baseRemotes are the remotes whose branches pull requests merge into, in
// order: the one gh repo set-default chose, a fork's upstream, origin. The
// branch's own remote is not, a fork's as gh pr checkout adds it.
func baseRemotes(repo string) []string {
	var remotes []string
	if out, err := git(repo, "config", "--get-regexp", `^remote\..*\.gh-resolved$`); err == nil {
		for line := range strings.Lines(out) {
			key, value, _ := strings.Cut(strings.TrimSpace(line), " ")
			if value == "base" {
				remotes = append(remotes, strings.TrimSuffix(strings.TrimPrefix(key, "remote."), ".gh-resolved"))
			}
		}
	}
	for _, name := range []string{"upstream", "origin"} {
		if !slices.Contains(remotes, name) {
			if _, err := git(repo, "config", "--get", "remote."+name+".url"); err == nil {
				remotes = append(remotes, name)
			}
		}
	}
	return remotes
}

// pullBase is the branch of the remote the branch's pull request is to
// merge into, fetched, as gh and GitHub's VS Code extension note it, ""
// where neither does.
func pullBase(repo, branch, remote string) string {
	var onto string
	if out, err := git(repo, "config", "--get", "branch."+branch+".github-pr-base-branch"); err == nil {
		// owner#name#branch, of the repository of the remote's URL.
		owner, rest, _ := strings.Cut(strings.TrimSpace(out), "#")
		name, base, _ := strings.Cut(rest, "#")
		url, _ := git(repo, "config", "--get", "remote."+remote+".url")
		if strings.EqualFold(githubPage(strings.TrimSpace(url)), "https://github.com/"+owner+"/"+name) {
			onto = base
		}
	} else if out, err := git(repo, "config", "--get", "branch."+branch+".gh-merge-base"); err == nil {
		onto = strings.TrimSpace(out)
	}
	if onto == "" || onto == branch {
		return ""
	}
	if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+onto); err != nil {
		return ""
	}
	return onto
}
