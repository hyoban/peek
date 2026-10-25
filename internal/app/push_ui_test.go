package app

import (
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestPushUI(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.FakeCodex(t)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Ship"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.push.running || a.push.failed || !tt.HasText("Shipped") {
		t.Fatalf("push state %+v; texts %q", a.push, tt.Texts())
	}
	if a.commits[0].Summary != "No changes" || a.commits[1].Subject != "Update the files" {
		t.Fatalf("stale history: %+v", a.commits)
	}
	tt.Advance(2 * time.Second)
	if !tt.HasText("Shipped") {
		t.Fatal("success label disappeared too soon")
	}
	tt.Advance(time.Second)
	if !tt.HasText("Ship") || tt.HasText("Shipped") {
		t.Fatal("success label did not expire", tt.Texts())
	}
	if a.canPush {
		t.Fatal("Ship still enabled after syncing")
	}
	_ = tt.Click("Ship")
	a.startPush()
	tt.Frame()
	if a.push.status != "" {
		t.Fatal("Ship ran with nothing to push")
	}
	cancelled := false
	a.push = pushState{running: true, status: "Generating commit message with Codex…", cancel: func() { cancelled = true }}
	tt.Frame()
	if err := tt.Click("Generating…"); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("not cancelled")
	}
	if err := os.WriteFile(filepath.Join(repo, "retry.txt"), []byte("retry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	tt.Frame()
	if !a.canPush {
		t.Fatal("Ship did not enable for new changes")
	}
	a.push = pushState{failed: true, status: "Test failure"}
	tt.Frame()
	tt.Advance(4 * time.Second)
	if !tt.HasText("Ship failed") {
		t.Fatal(tt.Texts())
	}
	for range 3 {
		tt.Frame()
	}
	if err := tt.Click("Ship failed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Shipped") {
		t.Fatal("retry did not finish", tt.Texts())
	}
}

func TestPushButtonLayouts(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []git.Commit{{Subject: "Uncommitted Changes"}}
	a.loadedRow, a.commitRow = 0, 0
	for _, width := range []int{640, 1200} {
		tt := ui.NewTester(a.view, width, 800)
		tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 68})
		settle(tt)
		if !tt.HasText("Ship") {
			t.Fatalf("no Ship button at width %d", width)
		}
		mode, ok := tt.Find("Changes")
		if !ok {
			t.Fatal("mode switch missing")
		}
		for _, state := range []pushState{
			{running: true, status: "Generating commit message with Codex…"},
			{running: true, status: "Shipping origin/main…"},
			{failed: true, status: "Test failure"},
			{status: "Shipped to origin/main"},
			{},
		} {
			a.push = state
			tt.Frame()
			if got, ok := tt.Find("Changes"); !ok || got != mode {
				t.Fatalf("mode switch moved at width %d for %q: %v → %v", width, state.status, mode, got)
			}
		}
	}
}
