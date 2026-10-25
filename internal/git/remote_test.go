package git

import (
	"context"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"testing"
)

func TestFetchAndPullUpstream(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	ctx := context.Background()
	if n, _ := BehindUpstream(repo); n != 0 {
		t.Fatalf("behind %d before remote work", n)
	}
	if moved, err := FetchUpstream(ctx, repo); err != nil || moved {
		t.Fatalf("fetch moved %v, %v with nothing new", moved, err)
	}
	testutil.RemoteWork(t, remote, "remote.txt")
	if n, _ := BehindUpstream(repo); n != 0 {
		t.Fatalf("behind %d before fetching", n)
	}
	moved, err := FetchUpstream(ctx, repo)
	if err != nil || !moved {
		t.Fatalf("fetch moved %v, %v", moved, err)
	}
	if n, name := BehindUpstream(repo); n != 1 || name != "origin/main" {
		t.Fatalf("behind %d of %q, want 1 of origin/main", n, name)
	}
	// The work tree's changes come along.
	if err := PullUpstream(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if n, _ := BehindUpstream(repo); n != 0 {
		t.Fatalf("behind %d after pulling", n)
	}
	if _, err := os.Stat(filepath.Join(repo, "remote.txt")); err != nil {
		t.Fatal(err)
	}

	// Diverged branches are not merged.
	testutil.RemoteWork(t, remote, "more.txt")
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Local work")
	if _, err := FetchUpstream(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := PullUpstream(ctx, repo); err == nil {
		t.Fatal("pulled diverged branches")
	}

	// A branch that tracks nothing fetches nothing.
	testutil.Git(t, repo, "switch", "-q", "-c", "local")
	if moved, err := FetchUpstream(ctx, repo); err != nil || moved {
		t.Fatalf("fetch moved %v, %v for an untracked branch", moved, err)
	}
	if n, _ := BehindUpstream(repo); n != 0 {
		t.Fatalf("behind %d for an untracked branch", n)
	}
}
