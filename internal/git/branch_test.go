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

func TestBranchBase(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Update the files")
	testutil.Git(t, repo, "push", "-q", "origin", "main")
	left := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.RemoteWork(t, remote, "theirs.txt")
	testutil.Git(t, repo, "fetch", "-q", "origin")
	if _, ok := BranchBase(repo); ok {
		t.Error("main has a range onto itself")
	}
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	if _, ok := BranchBase(repo); ok {
		t.Error("a branch of no commits of its own has a range")
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		testutil.Git(t, repo, "add", "--all")
		testutil.Git(t, repo, "commit", "-q", "-m", "Change "+name)
	}
	write("main.go", "package main\n\nfunc main() { run() }\n")
	write("run.go", "package main\n\nfunc run() {}\n")
	r, ok := BranchBase(repo)
	head := testutil.Git(t, repo, "rev-parse", "HEAD")
	if want := (BranchRange{Base: left, Head: head, Onto: "main", Commits: 2}); !ok || r != want {
		t.Fatalf("BranchBase = %+v, %v; want %+v", r, ok, want)
	}
	hash := Range(r.Base, r.Head)
	changes, err := LoadChanges(repo, hash)
	if err != nil {
		t.Fatal(err)
	}
	// Not theirs.txt, which main has since the branch left it.
	if want := []Change{{Status: "M", Old: "main.go", Path: "main.go", Added: 1, Deleted: 1}, {Status: "A", Old: "run.go", Path: "run.go", Added: 3}}; !slices.Equal(changes, want) {
		t.Errorf("LoadChanges = %+v; want %+v", changes, want)
	}
	before, after := FileTexts(repo, hash, changes[0])
	if before != "package main\n\nfunc main() {}\n" || after != "package main\n\nfunc main() { run() }\n" {
		t.Errorf("FileTexts = %q, %q", before, after)
	}
	if patch, err := Patch(repo, hash, changes[1], 1<<20); err != nil || !strings.Contains(patch, "+func run() {}") {
		t.Errorf("Patch = %q, %v", patch, err)
	}
	if files, err := ListFiles(repo, hash); err != nil || !slices.Contains(files, "run.go") || slices.Contains(files, "theirs.txt") {
		t.Errorf("ListFiles = %q, %v", files, err)
	}

	// A local main ahead of its remote's, which the branch left after.
	testutil.Git(t, repo, "switch", "-q", "main")
	write("local.txt", "local\n")
	ahead := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.Git(t, repo, "switch", "-q", "-c", "later")
	write("later.txt", "later\n")
	if r, ok := BranchBase(repo); !ok || r.Base != ahead || r.Commits != 1 {
		t.Errorf("BranchBase off a local main ahead = %+v, %v; want base %s, 1 commit", r, ok, ahead)
	}

	// The repository's main where it has no remote.
	testutil.Git(t, repo, "remote", "remove", "origin")
	if r, ok := BranchBase(repo); !ok || r.Base != ahead || r.Onto != "main" {
		t.Errorf("BranchBase with no remote = %+v, %v; want onto the local main", r, ok)
	}
	testutil.Git(t, repo, "switch", "-q", "--detach")
	if _, ok := BranchBase(repo); ok {
		t.Error("a detached HEAD has a range")
	}
}
