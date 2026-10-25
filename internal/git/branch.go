package git

import (
	"context"
	"fmt"
	"path/filepath"
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
