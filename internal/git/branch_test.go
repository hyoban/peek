package git

import (
	"context"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"slices"
	"strings"
	"testing"
)

func TestParsePullRequest(t *testing.T) {
	const page = "https://github.com/hyoban/peek"
	for _, tc := range []struct {
		in   string
		want int
	}{
		{"12", 12},
		{" #34 ", 34},
		{"https://github.com/hyoban/peek/pull/56", 56},
		{"https://github.com/hyoban/peek/pull/56/files", 56},
		{"https://github.com/Hyoban/Peek/pull/7#discussion_r1", 7},
		{"https://github.com/other/peek/pull/56", 0},
		{"main", 0},
		{"0", 0},
		{"", 0},
	} {
		n, ok := ParsePullRequest(tc.in, page)
		if n != tc.want || ok != (tc.want > 0) {
			t.Errorf("parsePullRequest(%q) = %d, %v; want %d", tc.in, n, ok, tc.want)
		}
	}
}

func TestListAndSwitchBranches(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	testutil.Git(t, repo, "branch", "feature")
	other := t.TempDir()
	testutil.Git(t, other, "clone", "-q", remote, ".")
	testutil.Git(t, other, "switch", "-c", "upstream-only")
	testutil.Git(t, other, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "--allow-empty", "-m", "Remote work")
	testutil.Git(t, other, "push", "-q", "origin", "upstream-only")
	testutil.Git(t, repo, "fetch", "-q", "origin")
	// Tags of the branches' names, which shadow their short names.
	testutil.Git(t, repo, "tag", "feature")
	testutil.Git(t, repo, "tag", "origin/upstream-only")

	ctx := context.Background()
	b, err := ListBranches(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(b.Local)
	if !slices.Equal(b.Local, []string{"feature", "main"}) || !slices.Equal(b.Remote, []string{"origin/upstream-only"}) {
		t.Fatalf("branches %+v", b)
	}
	if err := SwitchBranch(ctx, repo, "feature", false); err != nil {
		t.Fatal(err)
	}
	if got := CurrentBranch(repo); got != "feature" {
		t.Fatalf("on %q, want feature", got)
	}
	if err := SwitchBranch(ctx, repo, "origin/upstream-only", true); err != nil {
		t.Fatal(err)
	}
	if got := CurrentBranch(repo); got != "upstream-only" {
		t.Fatalf("on %q, want upstream-only", got)
	}
	tracked := testutil.Git(t, repo, "config", "branch.upstream-only.remote")
	if merge := testutil.Git(t, repo, "config", "branch.upstream-only.merge"); tracked != "origin" || merge != "refs/heads/upstream-only" {
		t.Fatalf("tracks %q of %q", merge, tracked)
	}
	// A change the switch would lose stops it, with git's reason alone.
	testutil.Git(t, repo, "switch", "-q", "main")
	testutil.Git(t, repo, "switch", "-q", "-c", "diverged")
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // diverged\n"), 0o644)
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-qam", "Diverge")
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // edited\n"), 0o644)
	err = SwitchBranch(ctx, repo, "feature", false)
	if err == nil || strings.HasPrefix(err.Error(), "git ") || !strings.Contains(err.Error(), "main.go") {
		t.Fatalf("switch over a change: %v", err)
	}
	if got := CurrentBranch(repo); got != "diverged" {
		t.Fatalf("on %q after a refused switch", got)
	}
}
