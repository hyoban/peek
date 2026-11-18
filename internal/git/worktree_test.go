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

// A branch merged by squashing, which the remote then deleted, is only
// cleanable once its remote is fetched with pruning; a remote that can't
// be reached is reported and leaves the rest fetched.
func TestPruneTrackedRemotes(t *testing.T) {
	ctx := context.Background()
	repo, remote := testutil.RemoteRepo(t)
	dir := filepath.Join(t.TempDir(), "squashed")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "squashed", dir)
	os.WriteFile(filepath.Join(dir, "squashed.txt"), []byte("squashed\n"), 0o644)
	testutil.Git(t, dir, "add", "--all")
	testutil.Git(t, dir, "commit", "-q", "-m", "Add squashed")
	testutil.Git(t, dir, "push", "-q", "-u", "origin", "squashed")
	testutil.Git(t, remote, "branch", "-D", "squashed")

	if done, _, err := CleanableWorktrees(ctx, repo); err != nil || len(done) != 0 {
		t.Fatalf("cleanable before fetching %v, %v", done, err)
	}
	testutil.Git(t, repo, "remote", "add", "dead", filepath.Join(t.TempDir(), "missing"))
	testutil.Git(t, repo, "branch", "-q", "elsewhere")
	testutil.Git(t, repo, "config", "branch.elsewhere.remote", "dead")
	testutil.Git(t, repo, "config", "branch.elsewhere.merge", "refs/heads/elsewhere")
	if err := PruneTrackedRemotes(ctx, repo); err == nil || !strings.Contains(err.Error(), "dead") {
		t.Errorf("unreachable remote: %v", err)
	}
	done, _, err := CleanableWorktrees(ctx, repo)
	if err != nil || len(done) != 1 || done[0].Branch != "squashed" || !done[0].Gone {
		t.Fatalf("cleanable after fetching %v, %v", done, err)
	}
}
