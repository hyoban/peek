package app

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"peek/internal/git"
	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// A worktree's Remove Worktree… in the branch switcher asks first, asks
// again where it has changes, then deletes its folder, keeping its
// branch, and closes the project open on it.
func TestRemoveWorktree(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "elsewhere", other)
	os.WriteFile(filepath.Join(other, "wip.txt"), []byte("wip\n"), 0o644)

	p := &projects{apps: map[string]*app{}, inline: true}
	p.open(other)
	p.open(repo)
	tt := ui.NewTester(p.view, 1200, 800)
	settle(tt)
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if err := tt.RightClick("elsewhere"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Remove Worktree…"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !tt.HasText("Remove the worktree of “elsewhere”?") {
		t.Fatalf("no alert asking, texts %q", tt.Texts())
	}
	if err := tt.Click("Remove Worktree"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !tt.HasText("The worktree of “elsewhere” has changes.") || !tt.HasText("It has 1 untracked file. Removing the worktree deletes its changes, and you can’t undo this.") {
		t.Fatalf("no alert of the changes, texts %q", tt.Texts())
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatalf("removed with its changes: %v", err)
	}
	if err := tt.Click("Remove Anyway"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if _, err := os.Stat(other); !os.IsNotExist(err) {
		t.Fatalf("folder left: %v", err)
	}
	if testutil.Git(t, repo, "branch", "--list", "elsewhere") == "" {
		t.Error("branch deleted with its worktree")
	}
	if p.apps[other] != nil || !git.SamePath(p.cur.repo, repo) {
		t.Errorf("projects open %v, showing %q", p.apps, p.cur.repo)
	}
}

// Clean Up Worktrees… asks before removing those whose branches are
// merged, leaving a new one, and says so where there are none left.
func TestCleanUpWorktrees(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	base := t.TempDir()
	merged, fresh := filepath.Join(base, "merged"), filepath.Join(base, "fresh")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "merged", merged)
	testutil.Git(t, merged, "push", "-q", "-u", "origin", "merged")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "fresh", fresh)

	a, tt := testApp(t, repo, nil)
	settle(tt)
	a.cleanUpWorktrees()
	settle(tt)
	if !tt.HasText("Remove the worktree of “merged”?") || !hasTextContaining(tt, "/fresh: its branch was never pushed") {
		t.Fatalf("no alert asking, texts %q", tt.Texts())
	}
	if err := tt.Click("Remove Worktrees"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if _, err := os.Stat(merged); !os.IsNotExist(err) {
		t.Errorf("merged worktree left: %v", err)
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Errorf("new worktree removed: %v", err)
	}
	a.cleanUpWorktrees()
	settle(tt)
	if !tt.HasText("No Worktrees to Clean Up") || !hasTextContaining(tt, "its branch was never pushed") {
		t.Fatalf("no alert of none, texts %q", tt.Texts())
	}
}

// Clean Up Worktrees… fetches to learn the branches the remote deleted,
// as it does those merged by squashing.
func TestCleanUpWorktreesFetches(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	squashed := filepath.Join(t.TempDir(), "squashed")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "squashed", squashed)
	os.WriteFile(filepath.Join(squashed, "squashed.txt"), []byte("squashed\n"), 0o644)
	testutil.Git(t, squashed, "add", "--all")
	testutil.Git(t, squashed, "commit", "-q", "-m", "Add squashed")
	testutil.Git(t, squashed, "push", "-q", "-u", "origin", "squashed")
	testutil.Git(t, remote, "branch", "-D", "squashed")

	a, tt := testApp(t, repo, nil)
	settle(tt)
	a.cleanUpWorktrees()
	settle(tt)
	if !tt.HasText("Remove the worktree of “squashed”?") {
		t.Fatalf("no alert asking, texts %q", tt.Texts())
	}
}

// Worktrees cleaned up that have changes are kept, each named with what
// it has.
func TestCleanUpWorktreesWithChanges(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	merged := filepath.Join(t.TempDir(), "merged")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "merged", merged)
	testutil.Git(t, merged, "push", "-q", "-u", "origin", "merged")
	os.WriteFile(filepath.Join(merged, "main.go"), []byte("package changed\n"), 0o644)

	a, tt := testApp(t, repo, nil)
	settle(tt)
	a.cleanUpWorktrees()
	settle(tt)
	if err := tt.Click("Remove Worktrees"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !tt.HasText("The worktree of “merged” wasn’t removed.") || !hasTextContaining(tt, "/merged: it has 1 uncommitted change") {
		t.Fatalf("no reason given, texts %q", tt.Texts())
	}
	if _, err := os.Stat(merged); err != nil {
		t.Errorf("removed with its changes: %v", err)
	}
}

// hasTextContaining reports whether a text shown has s in it.
func hasTextContaining(tt *ui.Tester, s string) bool {
	return slices.ContainsFunc(tt.Texts(), func(text string) bool { return strings.Contains(text, s) })
}

// Removing worktrees shows how far it has got, and Stop leaves the rest.
func TestWorktreeProgress(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	stop := a.startWorktreeProgress(3)
	a.worktreeProgress.step(1, "second")
	tt.Frame()
	if !tt.HasText("Removing Worktrees") || !tt.HasText("Removing “second” — 2 of 3") {
		t.Fatalf("no progress, texts %q", tt.Texts())
	}
	if err := tt.Click("Stop"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !stop.Load() || !tt.HasText("Stopping after “second”…") {
		t.Fatalf("not stopping, texts %q", tt.Texts())
	}
	a.worktreeProgress = worktreeProgress{}
	tt.Frame()
	if tt.HasText("Removing Worktrees") {
		t.Fatal("progress left up")
	}
}

// An alert names a few worktrees and counts the rest, so its buttons stay
// on the screen however many there are.
func TestListLines(t *testing.T) {
	lines := func(n int) []string {
		l := make([]string, n)
		for i := range l {
			l[i] = fmt.Sprint(i + 1)
		}
		return l
	}
	for _, tc := range []struct {
		n    int
		want string
	}{
		{1, "1"},
		{6, "1\n2\n3\n4\n5\n6"}, // one more is named, not counted
		{12, "1\n2\n3\n4\n5\nand 7 more"},
	} {
		if got := listLines(lines(tc.n)); got != tc.want {
			t.Errorf("listLines(%d lines) = %q, want %q", tc.n, got, tc.want)
		}
	}
}
