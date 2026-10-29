package app

import (
	"context"
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSwitcherUI(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	testutil.Git(t, repo, "branch", "feature")
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	log := testutil.FakeGH(t, "https://github.com/hyoban/peek")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if a.branch != "main" || a.github == "" {
		t.Fatalf("branch %q, github %q", a.branch, a.github)
	}
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	for _, want := range []string{"Branches", "feature", "Pull Requests"} {
		if !tt.HasText(want) {
			t.Fatalf("no %q in %q", want, tt.Texts())
		}
	}
	// The pull requests list in a tab of their own, which the switcher
	// keeps.
	if tt.HasText("#7 Add the switcher") {
		t.Fatal("pull requests listed among the branches")
	}
	if err := tt.Click("Pull Requests"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("#7 Add the switcher") || tt.HasText("feature") {
		t.Fatalf("pull requests' tab shows %q", tt.Texts())
	}
	a.switcher.open = false
	tt.Frame()
	a.openSwitcher()
	tt.Frame()
	if !tt.HasText("#7 Add the switcher") {
		t.Fatalf("tab not kept: %q", tt.Texts())
	}
	if err := tt.Click("Branches"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("feature"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.branch != "feature" || a.switcher.open || a.switcher.err != "" {
		t.Fatalf("branch %q, switcher %+v", a.branch, a.switcher)
	}

	// The arrows move the highlight among the rows, which Enter checks out.
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	rows, _ := a.rows()
	if len(rows) < 2 || a.highlighted(len(rows), 0) != 0 {
		t.Fatalf("rows %+v", rows)
	}
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	if a.highlighted(len(rows), 0) != 1 {
		t.Fatal("Down did not move the highlight")
	}
	tt.Key(0, ui.KeyUp)
	tt.Key(0, ui.KeyUp)
	tt.Frame()
	if got := a.highlighted(len(rows), 0); got != len(rows)-1 {
		t.Fatalf("Up from the first row went to %d", got)
	}
	tt.Key(0, ui.KeyDown)
	tt.Key(0, ui.KeyDown)
	tt.Frame()
	want := rows[1].target.branch
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.branch != want || a.switcher.open {
		t.Fatalf("branch %q, want %q; switcher %+v", a.branch, want, a.switcher)
	}
	a.switchTo(switchTarget{branch: "feature"})
	tt.Frame()

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
	if !strings.Contains(string(data), "pr list --repo hyoban/peek --state open --limit 50 --search sort:updated-desc") || !strings.Contains(string(data), "pr checkout 7 --repo hyoban/peek") {
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
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "elsewhere", other)
	b, err := git.ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.Worktrees) != 1 || !git.SamePath(b.Worktrees["elsewhere"], other) {
		t.Fatalf("worktrees %v", b.Worktrees)
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
	if !git.SamePath(p.cur.repo, other) || p.cur.branch != "elsewhere" || git.CurrentBranch(repo) != "main" {
		t.Fatalf("showing %q on %q; repo on %q", p.cur.repo, p.cur.branch, git.CurrentBranch(repo))
	}
}

// A pull request whose branch another work tree has checked out opens
// there, as gh would fail to check it out here.
func TestSwitcherOpensPullRequestWorktree(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	testutil.FakeGH(t, "https://github.com/hyoban/peek")
	other := filepath.Join(t.TempDir(), "other")
	testutil.Git(t, repo, "worktree", "add", "-q", "-b", "switcher", other)

	p := &projects{apps: map[string]*app{}, inline: true}
	p.open(repo)
	tt := ui.NewTester(p.view, 1200, 800)
	settle(tt)
	if err := tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if err := tt.Click("Pull Requests"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Worktree") {
		t.Fatalf("no worktree mark in %q", tt.Texts())
	}
	if err := tt.Click("#7 Add the switcher"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !git.SamePath(p.cur.repo, other) || p.cur.branch != "switcher" || git.CurrentBranch(repo) != "main" {
		t.Fatalf("showing %q on %q; repo on %q", p.cur.repo, p.cur.branch, git.CurrentBranch(repo))
	}
}

// In a fork whose gh takes the upstream, its pull requests are listed,
// linked to and checked out alike, and the fork's links are not taken.
func TestSwitcherForkPullRequests(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/alice/peek.git")
	log := testutil.FakeGH(t, "https://github.com/org/peek")
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
	repo, _ := testutil.RemoteRepo(t)
	testutil.FakeCodex(t)
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
	a.startReview("")
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

// The GitHub button opens the branch's pull request where it has one, and
// the repository's page on the default branch.
func TestGitHubButtonOpensPullRequest(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	testutil.Git(t, repo, "push", "-q", "-u", "origin", "feature")
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	testutil.FakeGH(t, "https://github.com/hyoban/peek")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if got, want := a.githubPage(), "https://github.com/hyoban/peek/pull/7"; got != want {
		t.Fatalf("feature's page %q, want %q", got, want)
	}
	testutil.Git(t, repo, "switch", "-q", "main")
	a.refresh()
	settle(tt)
	if got, want := a.githubPage(), "https://github.com/hyoban/peek"; got != want {
		t.Fatalf("main's page %q, want %q", got, want)
	}
}

// The switcher is as tall loading as loaded, and on either tab, at the
// height its popover opens at: it never jumps as the lists come.
func TestSwitcherKeepsItsHeight(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "remote", "set-url", "origin", "https://github.com/hyoban/peek.git")
	testutil.FakeGH(t, "https://github.com/hyoban/peek")
	a, _ := testApp(t, repo, nil)
	height := func() int {
		var h ui.Handle
		var got float32
		tt := ui.NewTester(func(c *ui.Context) {
			a.themed(c)
			a.childTheme(c)
			ui.Column(c).Bind(&h).Padding(popoverPadding).Children(func() { a.switcherPanel(c) })
			if e := c.Resolve(h); e.Valid() {
				got = e.Bounds().H
			}
		}, 600, 900)
		tt.Frame()
		tt.Frame()
		return int(got + 0.5)
	}
	loading := height()
	a.openSwitcher()
	loaded := height()
	a.switcher.tab = switchPulls
	pulls := height()
	if loading != a.switcherHeight() || loaded != loading || pulls != loading {
		t.Fatalf("heights %d loading, %d loaded, %d of the pull requests; opens at %d", loading, loaded, pulls, a.switcherHeight())
	}
}
