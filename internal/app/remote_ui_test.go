package app

import (
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestPullButton(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	a, tt := testApp(t, repo, nil)
	tt.Frame()
	if tt.HasText("↓ 1") {
		t.Fatal("pull shows while up to date")
	}
	testutil.RemoteWork(t, remote, "remote.txt")
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
	repo, _ := testutil.RemoteRepo(t)
	if err := os.WriteFile(filepath.Join(repo, "change.txt"), []byte("change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "branch", "feature")
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
	if got := git.CurrentBranch(repo); got != "main" {
		t.Fatalf("switched to %q during a pull", got)
	}
	_ = tt.Click("Ship")
	_ = tt.Click("Switch Branch")
	tt.Frame()
	if a.push.running || a.push.status != "" || a.switcher.open {
		t.Fatalf("controls stayed enabled during a pull: push %+v, switcher open %v", a.push, a.switcher.open)
	}
}
