package main

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
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
		n, ok := parsePullRequest(tc.in, page)
		if n != tc.want || ok != (tc.want > 0) {
			t.Errorf("parsePullRequest(%q) = %d, %v; want %d", tc.in, n, ok, tc.want)
		}
	}
}

func TestListAndSwitchBranches(t *testing.T) {
	repo, remote := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	pushTestGit(t, repo, "branch", "feature")
	other := t.TempDir()
	pushTestGit(t, other, "clone", "-q", remote, ".")
	pushTestGit(t, other, "switch", "-c", "upstream-only")
	pushTestGit(t, other, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "--allow-empty", "-m", "Remote work")
	pushTestGit(t, other, "push", "-q", "origin", "upstream-only")
	pushTestGit(t, repo, "fetch", "-q", "origin")
	// Tags of the branches' names, which shadow their short names.
	pushTestGit(t, repo, "tag", "feature")
	pushTestGit(t, repo, "tag", "origin/upstream-only")

	ctx := context.Background()
	b, err := listBranches(ctx, repo)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(b.local)
	if !slices.Equal(b.local, []string{"feature", "main"}) || !slices.Equal(b.remote, []string{"origin/upstream-only"}) {
		t.Fatalf("branches %+v", b)
	}
	if err := switchBranch(ctx, repo, "feature", false); err != nil {
		t.Fatal(err)
	}
	if got := currentBranch(repo); got != "feature" {
		t.Fatalf("on %q, want feature", got)
	}
	if err := switchBranch(ctx, repo, "origin/upstream-only", true); err != nil {
		t.Fatal(err)
	}
	if got := currentBranch(repo); got != "upstream-only" {
		t.Fatalf("on %q, want upstream-only", got)
	}
	tracked := pushTestGit(t, repo, "config", "branch.upstream-only.remote")
	if merge := pushTestGit(t, repo, "config", "branch.upstream-only.merge"); tracked != "origin" || merge != "refs/heads/upstream-only" {
		t.Fatalf("tracks %q of %q", merge, tracked)
	}
	// A change the switch would lose stops it, with git's reason alone.
	pushTestGit(t, repo, "switch", "-q", "main")
	pushTestGit(t, repo, "switch", "-q", "-c", "diverged")
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // diverged\n"), 0o644)
	pushTestGit(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-qam", "Diverge")
	os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // edited\n"), 0o644)
	err = switchBranch(ctx, repo, "feature", false)
	if err == nil || strings.HasPrefix(err.Error(), "git ") || !strings.Contains(err.Error(), "main.go") {
		t.Fatalf("switch over a change: %v", err)
	}
	if got := currentBranch(repo); got != "diverged" {
		t.Fatalf("on %q after a refused switch", got)
	}
}

// fakeGH puts a gh on the PATH that lists one pull request and checks
// it out as a branch of its name, recording what it was asked; its default
// repository is github's, the page of one.
func fakeGH(t *testing.T, github string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$@" >> "` + log + `"
case "$1 $2" in
  "repo view") echo "` + github + `" ;;
  "pr list") printf '%s' '[{"number":7,"title":"Add the switcher","headRefName":"switcher","isDraft":false,"author":{"login":"ada"}}]' ;;
  "pr checkout") git switch -q -c "pr-$3" ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

func TestSwitcherUI(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	pushTestGit(t, repo, "branch", "feature")
	pushTestGit(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	log := fakeGH(t, "https://github.com/hyoban/peek")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if a.branch != "main" || a.github == "" {
		t.Fatalf("branch %q, github %q", a.branch, a.github)
	}
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Branches", "feature", "Pull Requests", "#7 Add the switcher"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	if err := tt.Click("feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.branch != "feature" || a.switcher.open || a.switcher.err != "" {
		t.Fatalf("branch %q, switcher %+v", a.branch, a.switcher)
	}

	// A number typed and entered checks out its pull request.
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	tt.Type("#7")
	tt.Frame()
	if a.switcher.query != "#7" || tt.HasText("main") || !tt.HasText("#7 Add the switcher") {
		t.Fatalf("query %q, texts %q", a.switcher.query, tt.Texts())
	}
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.branch != "pr-7" || a.switcher.open {
		t.Fatalf("branch %q, switcher %+v", a.branch, a.switcher)
	}
	data, _ := os.ReadFile(log)
	if !strings.Contains(string(data), "pr list --repo hyoban/peek") || !strings.Contains(string(data), "pr checkout 7 --repo hyoban/peek") {
		t.Fatalf("gh was asked %q", data)
	}

	// The branch switched to is current at once, before the history is
	// read again, for the next choice to see it.
	a.refreshing = true // a read under way, which another waits for
	a.switchTo(switchTarget{branch: "main"})
	if a.branch != "main" || a.switcher.running != "" {
		t.Fatalf("branch %q, switcher %+v", a.branch, a.switcher)
	}
	a.switchTo(switchTarget{branch: "pr-7"})
	if a.branch != "pr-7" {
		t.Fatalf("switching back left %q", a.branch)
	}
	a.refreshing = false

	// A failure keeps the switcher open, with the reason.
	a.openSwitcher()
	a.switchTo(switchTarget{branch: "missing"})
	tt.Frame()
	if !a.switcher.open || a.switcher.err == "" || a.branch != "pr-7" {
		t.Fatalf("branch %q, switcher %+v", a.branch, a.switcher)
	}
}

func TestSwitcherOpensWorktree(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	other := filepath.Join(t.TempDir(), "other")
	pushTestGit(t, repo, "worktree", "add", "-q", "-b", "elsewhere", other)
	b, err := listBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.worktrees) != 1 || !samePath(b.worktrees["elsewhere"], other) {
		t.Fatalf("worktrees %v", b.worktrees)
	}

	p := &projects{apps: map[string]*app{}, inline: true}
	p.open(repo)
	tt := ui.NewTester(p.view, 1200, 800)
	settle(tt)
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Worktree") {
		t.Fatalf("no worktree mark in %q", tt.Texts())
	}
	if err := tt.Click("elsewhere"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !samePath(p.cur.repo, other) || p.cur.branch != "elsewhere" || currentBranch(repo) != "main" {
		t.Fatalf("showing %q on %q; repo on %q", p.cur.repo, p.cur.branch, currentBranch(repo))
	}
}

// In a fork whose gh takes the upstream, its pull requests are listed,
// linked to and checked out alike, and the fork's links are not taken.
func TestSwitcherForkPullRequests(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "remote", "set-url", "origin", "https://github.com/alice/peek.git")
	log := fakeGH(t, "https://github.com/org/peek")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	a.openSwitcher()
	tt.Frame()
	if a.switcher.prRepo != "https://github.com/org/peek" {
		t.Fatalf("pull requests of %q", a.switcher.prRepo)
	}
	a.switcher.query = "https://github.com/alice/peek/pull/7"
	if _, _, _, named := a.matches(); named != 0 {
		t.Fatalf("the fork's link named #%d", named)
	}
	a.switcher.query = "https://github.com/org/peek/pull/7/files"
	a.submitSwitch()
	tt.Frame()
	data, _ := os.ReadFile(log)
	if a.branch != "pr-7" || !strings.Contains(string(data), "pr checkout 7 --repo org/peek") {
		t.Fatalf("on %q; gh was asked %q", a.branch, data)
	}
}

// Ship and a checkout never run at once, whichever starts first.
func TestShipWaitsForCheckout(t *testing.T) {
	repo, _ := pushTestRepo(t)
	fakeCodex(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if !a.canPush {
		t.Fatal("nothing to ship")
	}
	a.switcher.running = "#7"
	tt.Frame()
	if err := tt.Click("Ship"); err != nil {
		t.Fatal(err)
	}
	a.startPush()
	a.startReview(ui.Services{})
	tt.Frame()
	if a.push.status != "" || a.review.running {
		t.Fatalf("started during a checkout: push %+v, review %+v", a.push, a.review)
	}
	a.switcher.running = ""
	a.push = pushState{running: true, cancel: func() {}}
	a.switchTo(switchTarget{branch: "elsewhere"})
	if a.switcher.running != "" {
		t.Fatal("checkout started during Ship")
	}
}
