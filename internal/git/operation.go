package git

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Operation is what git is in the middle of in a work tree, a merge, a
// rebase, a cherry-pick or a revert stopped for the files to be resolved
// or the commit to be made, which its changes are then of.
type Operation struct {
	// Title says what git is doing, as "Merging origin/main"; "" where it
	// is doing none.
	Title string
	// Conflicts are the files still to resolve.
	Conflicts int
}

// mergeName is the name a merge's message gives what is merged in, as
// "Merge remote-tracking branch 'origin/main' into topic" does.
var mergeName = regexp.MustCompile(`^Merge (?:remote-tracking )?(?:branch|branches|tag|commit) '([^']+)'`)

// ReadOperation reads what git is in the middle of in the work tree, as
// its own files there tell it.
func ReadOperation(repo string) Operation {
	states := []string{"MERGE_HEAD", "MERGE_MSG", "rebase-merge", "rebase-apply", "CHERRY_PICK_HEAD", "REVERT_HEAD"}
	out, err := git(repo, append([]string{"rev-parse"}, prefixed("--git-path", states)...)...)
	if err != nil {
		return Operation{}
	}
	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) != len(states) {
		return Operation{}
	}
	path := map[string]string{}
	for i, s := range states {
		p := lines[i]
		if !filepath.IsAbs(p) {
			p = filepath.Join(repo, p)
		}
		path[s] = p
	}
	read := func(s string) (string, bool) {
		b, err := os.ReadFile(path[s])
		return strings.TrimSpace(string(b)), err == nil
	}
	exists := func(s string) bool {
		_, err := os.Stat(path[s])
		return err == nil
	}
	short := func(hash string) string {
		if len(hash) > 7 {
			return hash[:7]
		}
		return hash
	}
	var op Operation
	switch {
	case exists("rebase-merge") || exists("rebase-apply"):
		dir := path["rebase-merge"]
		if !exists("rebase-merge") {
			dir = path["rebase-apply"]
		}
		op.Title = "Rebasing"
		if b, err := os.ReadFile(filepath.Join(dir, "head-name")); err == nil {
			if name := strings.TrimPrefix(strings.TrimSpace(string(b)), "refs/heads/"); name != "" && name != "detached HEAD" {
				op.Title += " " + name
			}
		}
		if b, err := os.ReadFile(filepath.Join(dir, "onto")); err == nil {
			if onto := strings.TrimSpace(string(b)); onto != "" {
				op.Title += " onto " + short(onto)
			}
		}
	case exists("MERGE_HEAD"):
		op.Title = "Merging"
		msg, _ := read("MERGE_MSG")
		if m := mergeName.FindStringSubmatch(msg); m != nil {
			op.Title += " " + m[1]
		} else if head, ok := read("MERGE_HEAD"); ok && head != "" {
			op.Title += " " + short(strings.Fields(head)[0])
		}
	case exists("CHERRY_PICK_HEAD"):
		head, _ := read("CHERRY_PICK_HEAD")
		op.Title = strings.TrimSpace("Cherry-picking " + short(head))
	case exists("REVERT_HEAD"):
		head, _ := read("REVERT_HEAD")
		op.Title = strings.TrimSpace("Reverting " + short(head))
	default:
		return Operation{}
	}
	if out, err := git(repo, "diff", "--name-only", "--diff-filter=U", "-z"); err == nil {
		op.Conflicts = len(nulSeparated(out))
	}
	return op
}

func prefixed(flag string, values []string) []string {
	args := make([]string, 0, 2*len(values))
	for _, v := range values {
		args = append(args, flag, v)
	}
	return args
}
