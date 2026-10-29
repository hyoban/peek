package app

import (
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"slices"
	"strings"
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
	for _, want := range []string{"Add the switcher ", "#7", "Open", "By ada into main", "Review required", "3 checks: 1 failed, 1 running, 1 passed",
		"test", "Required", "Merging is blocked", "An approving review is required", "1 required check failed", "1 conversation is not resolved",
		"Lists the branches."} {
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
	if tt.HasText("By ada into main") || !tt.HasText("Ada") {
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

// Why a pull request cannot merge, as its state tells.
func TestMergeStatus(t *testing.T) {
	failing := []git.Check{{Name: "test", State: git.CheckFailed, Required: true}, {Name: "lint", State: git.CheckFailed},
		{Name: "build", State: git.CheckPending, Required: true}}
	for _, tc := range []struct {
		pr    git.BranchPull
		level mergeLevel
		want  []string
	}{
		{git.BranchPull{MergeState: "CLEAN", Base: "main"}, mergeReady, nil},
		{git.BranchPull{Draft: true, MergeState: "DRAFT"}, mergeBlocked, []string{"Mark it ready for review on GitHub to merge it"}},
		{git.BranchPull{Mergeable: "CONFLICTING", MergeState: "DIRTY", Base: "main"}, mergeBlocked,
			[]string{"This branch has conflicts with main that must be resolved"}},
		{git.BranchPull{MergeState: "BEHIND", Base: "main"}, mergeBlocked, []string{"This branch is out of date with main: update it to merge"}},
		{git.BranchPull{MergeState: "BLOCKED", Review: "CHANGES_REQUESTED", Checks: failing, Unresolved: 2}, mergeBlocked,
			[]string{"A reviewer requested changes", "1 required check failed", "1 required check is still running", "2 conversations are not resolved"}},
		// Where GitHub did not tell which checks are required, any.
		{git.BranchPull{MergeState: "BLOCKED", Checks: failing, Unresolved: -1}, mergeBlocked,
			[]string{"2 checks failed", "1 check is still running"}},
		{git.BranchPull{MergeState: "BLOCKED", Base: "main"}, mergeBlocked, []string{"The rules of main aren’t met yet"}},
		{git.BranchPull{MergeState: "UNSTABLE"}, mergeWarning, nil},
		{git.BranchPull{Mergeable: "UNKNOWN", MergeState: "UNKNOWN"}, mergeChecking, nil},
	} {
		m := mergeStatusOf(&tc.pr)
		if m.level != tc.level || !slices.Equal(m.reasons, tc.want) {
			t.Errorf("%+v: %+v, want level %d, reasons %q", tc.pr, m, tc.level, tc.want)
		}
	}
}

// A pull request read after the branch's changes show comes in at the
// top of their diffs, the first file chosen under it, not above out of
// sight.
func TestPullHeaderComesInView(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte(strings.Repeat("line\n", 200)), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add the feature")
	testutil.Git(t, repo, "push", "-q", "-u", "origin", "feature")
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	testutil.FakeGH(t, "https://github.com/hyoban/peek")
	a, tt := testApp(t, repo, nil)
	pr := a.pullReq
	a.pullReq = nil // not read yet
	settle(tt)
	pr.Body = strings.Repeat("A paragraph of the description.\n\n", 80)
	a.pullReq = pr
	settle(tt)
	if r, ok := tt.Find("By ada into main"); !ok || r.H == 0 {
		t.Fatalf("pull request's header out of sight: %+v", r)
	}
}
