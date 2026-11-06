package git

import (
	"path/filepath"
	"peek/internal/testutil"
	"testing"
)

// A main checkout keeps its git files in its work tree; a linked work
// tree keeps them in the main checkout's .git, which is to be watched.
func TestReadGitDirs(t *testing.T) {
	repo := testutil.Repo(t)
	if d := ReadGitDirs(repo); d != (GitDirs{}) {
		t.Fatalf("main checkout: %+v, want none apart", d)
	}
	linked := filepath.Join(t.TempDir(), "linked")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "linked", linked)
	d := ReadGitDirs(linked)
	if !SamePath(d.Common, filepath.Join(repo, ".git")) || d.Own != "worktrees/linked" || d.Apart != "" {
		t.Fatalf("linked work tree: %+v", d)
	}
}
