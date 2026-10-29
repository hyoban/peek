package app

import (
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"testing"
)

// The branch's changes show its pull request over their diffs: its title,
// its checks and its description, the failing check listed at once; the
// history's row of them marks how the checks stand.
func TestPullHeader(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add the feature")
	testutil.Git(t, repo, "push", "-q", "-u", "origin", "feature")
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	testutil.FakeGH(t, "https://github.com/hyoban/peek")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if cm, ok := a.current(); !ok || !git.IsRange(cm.Hash) {
		t.Fatalf("shown %+v, want the branch's changes", cm)
	}
	settle(tt)
	for _, want := range []string{"Add the switcher ", "#7", "Open", "By ada", "3 checks: 1 failed, 1 running, 1 passed", "test", "Lists the branches."} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	if s, ok := a.pullReq.ChecksState(); !ok || s != git.CheckFailed {
		t.Fatalf("checks %v, %v, want failed", s, ok)
	}
	if err := tt.Click("Hide Checks"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if tt.HasText("lint") {
		t.Fatal("checks still listed once hidden")
	}

	// A commit of the branch shows its own message.
	a.commitRow = 2
	settle(tt)
	if tt.HasText("By ada") || !tt.HasText("Ada") {
		t.Fatalf("commit shows the pull request: %q", tt.Texts())
	}
}

func TestChecksSummary(t *testing.T) {
	pr := &git.BranchPull{Checks: []git.Check{{State: git.CheckPassed}, {State: git.CheckPassed}}}
	if got := checksSummary(pr); got != "All 2 checks passed" {
		t.Fatalf("summary %q", got)
	}
	pr.Checks = append(pr.Checks, git.Check{State: git.CheckSkipped})
	if got := checksSummary(pr); got != "3 checks: 2 passed, 1 skipped" {
		t.Fatalf("summary %q", got)
	}
	if got := pullDescription("<!-- template -->\r\nFixes it.\r\n<!--\nmore\n-->"); got != "Fixes it." {
		t.Fatalf("description %q", got)
	}
}
