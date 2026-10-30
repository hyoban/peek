package git

import (
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Discard undoes the uncommitted changes to these files, as
// LoadWorktreeChanges lists them, in the index and the work tree both:
// the files HEAD has go back to it, and those it does not, as new files
// and a rename's new path, go, with the folders they leave empty.
func Discard(repo string, changes []Change) error {
	var restore, remove []string
	for _, ch := range changes {
		if ch.Status != "A" {
			restore = append(restore, ch.Old)
		}
		switch ch.Status {
		case "A", "R", "C":
			remove = append(remove, ch.Path)
		}
	}
	// The paths as they are, not patterns nor :magic.
	env := []string{"GIT_LITERAL_PATHSPECS=1"}
	if len(remove) > 0 {
		// Out of the index, where they are added, though it differs from
		// the file.
		input := strings.Join(remove, "\x00") + "\x00"
		if _, err := gitWith(repo, env, input, "rm", "-q", "-f", "--cached", "--ignore-unmatch", "--sparse",
			"--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return err
		}
	}
	if len(restore) > 0 {
		input := strings.Join(restore, "\x00") + "\x00"
		if _, err := gitWith(repo, env, input, "restore", "--source=HEAD", "--staged", "--worktree",
			"--pathspec-from-file=-", "--pathspec-file-nul"); err != nil {
			return err
		}
	}
	for _, p := range remove {
		if err := os.Remove(filepath.Join(repo, filepath.FromSlash(p))); err != nil && !os.IsNotExist(err) {
			return err
		}
		// The folders it leaves empty, as git's own removals do; Remove
		// leaves those that still hold something.
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if os.Remove(filepath.Join(repo, filepath.FromSlash(dir))) != nil {
				break
			}
		}
	}
	return nil
}
