package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

// remoteWork pushes a commit to the remote from another clone, as a
// teammate would.
func remoteWork(t *testing.T, remote, file string) {
	t.Helper()
	other := t.TempDir()
	pushTestGit(t, other, "clone", "-q", remote, ".")
	if err := os.WriteFile(filepath.Join(other, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushTestGit(t, other, "add", "--all")
	pushTestGit(t, other, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Remote work")
	pushTestGit(t, other, "push", "-q", "origin", "main")
}

func TestFetchAndPullUpstream(t *testing.T) {
	repo, remote := pushTestRepo(t)
	ctx := context.Background()
	if n, _ := behindUpstream(repo); n != 0 {
		t.Fatalf("behind %d before remote work", n)
	}
	if moved, err := fetchUpstream(ctx, repo); err != nil || moved {
		t.Fatalf("fetch moved %v, %v with nothing new", moved, err)
	}
	remoteWork(t, remote, "remote.txt")
	if n, _ := behindUpstream(repo); n != 0 {
		t.Fatalf("behind %d before fetching", n)
	}
	moved, err := fetchUpstream(ctx, repo)
	if err != nil || !moved {
		t.Fatalf("fetch moved %v, %v", moved, err)
	}
	if n, name := behindUpstream(repo); n != 1 || name != "origin/main" {
		t.Fatalf("behind %d of %q, want 1 of origin/main", n, name)
	}
	// The work tree's changes come along.
	if err := pullUpstream(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if n, _ := behindUpstream(repo); n != 0 {
		t.Fatalf("behind %d after pulling", n)
	}
	if _, err := os.Stat(filepath.Join(repo, "remote.txt")); err != nil {
		t.Fatal(err)
	}

	// Diverged branches are not merged.
	remoteWork(t, remote, "more.txt")
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-q", "-m", "Local work")
	if _, err := fetchUpstream(ctx, repo); err != nil {
		t.Fatal(err)
	}
	if err := pullUpstream(ctx, repo); err == nil {
		t.Fatal("pulled diverged branches")
	}

	// A branch that tracks nothing fetches nothing.
	pushTestGit(t, repo, "switch", "-q", "-c", "local")
	if moved, err := fetchUpstream(ctx, repo); err != nil || moved {
		t.Fatalf("fetch moved %v, %v for an untracked branch", moved, err)
	}
	if n, _ := behindUpstream(repo); n != 0 {
		t.Fatalf("behind %d for an untracked branch", n)
	}
}

func TestPullButton(t *testing.T) {
	repo, remote := pushTestRepo(t)
	a, tt := testApp(t, repo, nil)
	tt.Frame()
	if tt.HasText("↓ 1") {
		t.Fatal("pull shows while up to date")
	}
	remoteWork(t, remote, "remote.txt")
	a.fetchRemote()
	tt.Frame()
	tt.Frame()
	if a.behind != 1 || !tt.HasText("↓ 1") {
		t.Fatalf("behind %d; texts %q", a.behind, tt.Texts())
	}
	if err := tt.Click("Pull"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Frame()
	if a.behind != 0 || a.pull.err != "" || tt.HasText("↓ 1") {
		t.Fatalf("behind %d, err %q after pulling", a.behind, a.pull.err)
	}
	if a.commits[1].Subject != "Remote work" {
		t.Fatalf("history %+v", a.commits[:2])
	}
}

// A pull in progress holds off what would use the checkout meanwhile.
func TestPullBlocksOtherOperations(t *testing.T) {
	repo, _ := pushTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushTestGit(t, repo, "branch", "feature")
	a, tt := testApp(t, repo, nil)
	tt.Frame()
	a.pull.running = true
	tt.Frame()
	a.startPush()
	a.startReview(ui.Services{})
	a.switchTo(switchTarget{branch: "feature"})
	tt.Frame()
	if a.push.running || a.push.status != "" || a.review.running || a.switcher.running != "" {
		t.Fatalf("started during a pull: push %+v, review running %v, switching %q", a.push, a.review.running, a.switcher.running)
	}
	if got := currentBranch(repo); got != "main" {
		t.Fatalf("switched to %q during a pull", got)
	}
	_ = tt.Click("Ship")
	_ = tt.Click("Switch Branch")
	tt.Frame()
	if a.push.running || a.push.status != "" || a.switcher.open {
		t.Fatalf("controls stayed enabled during a pull: push %+v, switcher open %v", a.push, a.switcher.open)
	}
}
