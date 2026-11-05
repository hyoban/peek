package git

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"slices"
	"strings"
	"testing"
)

// The work trees listed to clean up are those whose branch the remote
// deleted, or that were pushed and merged; not a new branch, a locked
// work tree or one whose folder is gone, which pruning forgets. Removing
// keeps the branch, and refuses changes but where forced.
func TestCleanableWorktrees(t *testing.T) {
	ctx := context.Background()
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Update the files")
	testutil.Git(t, repo, "push", "-q")
	base := t.TempDir()
	add := func(branch string, commit, push bool) string {
		dir := filepath.Join(base, branch)
		testutil.Git(t, repo, "worktree", "add", "-q", "-b", branch, dir)
		if commit {
			os.WriteFile(filepath.Join(dir, branch+".txt"), []byte(branch+"\n"), 0o644)
			testutil.Git(t, dir, "add", "--all")
			testutil.Git(t, dir, "commit", "-q", "-m", "Add "+branch)
		}
		if push {
			testutil.Git(t, dir, "push", "-q", "-u", "origin", branch)
		}
		return dir
	}
	merged := add("merged", true, true)
	testutil.Git(t, repo, "merge", "-q", "--no-edit", "merged")
	testutil.Git(t, repo, "push", "-q")
	testutil.Git(t, repo, "fetch", "-q")
	add("gone", true, true)
	testutil.Git(t, repo, "push", "-q", "origin", "--delete", "gone")
	testutil.Git(t, repo, "fetch", "-q", "--prune")
	add("fresh", false, false)
	add("ahead", true, true)
	locked := add("locked", true, true)
	testutil.Git(t, repo, "push", "-q", "origin", "--delete", "locked")
	testutil.Git(t, repo, "fetch", "-q", "--prune")
	testutil.Git(t, repo, "worktree", "lock", locked)
	missing := add("missing", true, true)
	testutil.Git(t, repo, "push", "-q", "origin", "--delete", "missing")
	testutil.Git(t, repo, "fetch", "-q", "--prune")
	os.RemoveAll(missing)

	done, kept, err := CleanableWorktrees(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, w := range done {
		names = append(names, w.Branch)
		if w.Gone != (w.Branch == "gone") {
			t.Errorf("%s gone %v", w.Branch, w.Gone)
		}
	}
	slices.Sort(names)
	if !slices.Equal(names, []string{"gone", "merged"}) {
		t.Fatalf("cleanable %q, want gone and merged", names)
	}
	reasons := map[string]string{}
	for _, w := range kept {
		reasons[w.Branch] = w.Reason
	}
	want := map[string]string{"fresh": "its branch was never pushed", "ahead": "its branch isn’t merged into main", "locked": "it’s locked"}
	if !maps.Equal(reasons, want) {
		t.Fatalf("kept %q, want %q", reasons, want)
	}
	if err := RemoveWorktree(ctx, repo, locked, false); err == nil || err.Error() != "it’s locked" {
		t.Errorf("removing a locked worktree: %v", err)
	}
	// From a linked work tree, the main one is never listed.
	if from, _, err := CleanableWorktrees(ctx, merged); err != nil || slices.ContainsFunc(from, func(w DoneWorktree) bool { return SamePath(w.Dir, repo) || SamePath(w.Dir, merged) }) {
		t.Fatalf("from a worktree %v, %v", from, err)
	}

	os.WriteFile(filepath.Join(merged, "wip.txt"), []byte("wip\n"), 0o644)
	if err := RemoveWorktree(ctx, repo, merged, false); !errors.Is(err, ErrWorktreeChanged) || err.Error() != "it has 1 untracked file" {
		t.Fatalf("removing a changed worktree: %v", err)
	}
	if err := RemoveWorktree(ctx, repo, merged, true); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(merged); !os.IsNotExist(err) {
		t.Errorf("folder left: %v", err)
	}
	if testutil.Git(t, repo, "branch", "--list", "merged") == "" {
		t.Error("branch deleted with its worktree")
	}
	if err := PruneWorktrees(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if list := testutil.Git(t, repo, "worktree", "list"); strings.Contains(list, string(filepath.Separator)+"missing ") {
		t.Errorf("missing worktree kept: %s", list)
	}
}
