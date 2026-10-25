package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/review"
	"peek/internal/testutil"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestReviewUI(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, testutil.PassingReview)
	a, tt := testApp(t, repo, nil)
	tt.SetClipboard("Keep this clipboard")
	askCodex(t, tt, "Review")
	conv := a.shownConversation()
	if a.review.running || !tt.HasText("Passed") || conv == nil || conv.Title != "Review" || conv.Turns[0].Review == nil {
		t.Fatalf("unexpected result: %+v, %q", a.review, tt.Texts())
	}
	if tt.Clipboard() != "Keep this clipboard" {
		t.Fatal("passing review changed the clipboard")
	}
	// The review shows in its conversation.
	if !tt.HasText("No actionable defects found.") || !tt.HasText("Review passed") {
		t.Fatal("missing review details", tt.Texts())
	}
	if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	tt.Frame()
	if _, ok := tt.Find("Passed"); ok {
		t.Fatal("stale verdict still shown", tt.Texts())
	}
	if err := os.WriteFile(os.Getenv("PEEK_TEST_REVIEW_RESPONSE"), []byte(testutil.FailingReview), 0o600); err != nil {
		t.Fatal(err)
	}
	askCodex(t, tt, "Review")
	if !tt.HasText("Not Passed") || len(a.conversations) != 2 {
		t.Fatal("missing failing verdict", tt.Texts())
	}
	wantClipboard := "Review not passed\nUncommitted changes\n\nEmpty inputs now crash.\n\n[P2] Handle empty inputs\ncalc.py:2\nRestore the guard so an empty list does not divide by zero."
	if got := tt.Clipboard(); got != "Keep this clipboard" {
		t.Fatalf("failing review changed the clipboard: %q", got)
	}
	if !tt.HasText("calc.py:2") || !tt.HasText("[P2] Handle empty inputs") {
		t.Fatal("missing findings", tt.Texts())
	}
	if err := tt.Click("Copy"); err != nil {
		t.Fatal(err)
	}
	if got := tt.Clipboard(); got != wantClipboard {
		t.Fatalf("Copy did not copy the review: %q", got)
	}
	tt.SetClipboard("Copied something else")
	if err := os.WriteFile(os.Getenv("PEEK_TEST_REVIEW_RESPONSE"), []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	askCodex(t, tt, "Review")
	if !tt.HasText("Review Error") || tt.HasText("Not Passed") {
		t.Fatal("invalid result treated as a verdict", tt.Texts())
	}
	if tt.Clipboard() != "Copied something else" {
		t.Fatal("redraw or review error overwrote the clipboard")
	}
}

func TestReviewCancelAndLayout(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []git.Commit{{Subject: "Uncommitted Changes"}}
	a.loadedRow, a.commitRow = 0, 0
	a.canPush = true
	for _, width := range []int{640, 1200} {
		tt := ui.NewTester(a.view, width, 800)
		tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 68})
		tt.Frame()
		tt.Frame() // the toolbar laid out for the width of its controls
		mode, ok := tt.Find("Changes")
		if !ok {
			t.Fatal("missing mode switch")
		}
		cancelled := false
		a.review = reviewState{running: true, cancel: func() { cancelled = true }}
		tt.Frame()
		tt.Frame()
		if !tt.HasText("Reviewing…") {
			t.Fatal("no activity", tt.Texts())
		}
		if err := tt.Click("Stop Review"); err != nil {
			t.Fatal(err)
		}
		if !cancelled {
			t.Fatal("review did not cancel")
		}
		a.startPush() // a running review must not race a commit
		if a.push.running {
			t.Fatal("push started during review")
		}
		for _, state := range []reviewState{
			{running: true, phase: "Inspecting…", detail: "git diff", cancel: func() {}},
			{stale: true},
			{err: "Network failure"},
			{result: &review.Result{Verdict: "passed", Summary: "Passed"}},
			{result: &review.Result{Verdict: "failed", Summary: "Failed"}},
			{},
		} {
			a.review = state
			tt.Frame()
			tt.Frame()
			// At the narrowest, the mode gives way to the controls at the end.
			if got, ok := tt.Find("Changes"); !ok || width > 640 && got != mode {
				t.Fatalf("mode switch moved at %d: %v → %v", width, mode, got)
			}
		}
		a.review = reviewState{running: true, phase: "Starting…", cancel: func() { cancelled = true }}
		tt.Frame()
		tt.Advance(12 * time.Second)
		tt.Frame()
		if !tt.HasText("Starting… 12s") {
			t.Fatal("missing elapsed review time", tt.Texts())
		}
		a.review.phase = "Inspecting…"
		tt.Frame()
		tt.Frame() // the toolbar laid out for the stage's width
		cancelled = false
		if !tt.HasText("Inspecting… 12s") {
			t.Fatal("no phase", tt.Texts())
		}
		if err := tt.Click("Stop Review"); err != nil || !cancelled {
			t.Fatalf("progress button did not cancel: %v", err)
		}
		if got, ok := tt.Find("Changes"); !ok || width > 640 && got != mode {
			t.Fatal("progress moved the mode switch")
		}
		a.review = reviewState{result: &review.Result{Verdict: "passed", Summary: "Passed"}}
		a.lastCodex = codexReview
		tt.Advance(time.Second)
		tt.Frame()
		if !tt.HasText("Passed") || tt.HasText("Inspecting… 13s") {
			t.Fatal("completed review still shows progress", tt.Texts())
		}
	}
}

func TestReviewNothingToDo(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	testutil.Git(t, repo, "push")
	a, tt := testApp(t, repo, nil)
	a.startReview("")
	tt.Frame()
	if a.reviewSource.ID != "" || a.review.running || a.review.err != "" || a.review.result != nil {
		t.Fatalf("review ran with nothing to review: %+v", a.review)
	}
	a.review = reviewState{sourceID: "old", result: &review.Result{Verdict: "passed"}}
	a.updateReviewSource(review.Snapshot{}, "")
	tt.Frame()
	if _, ok := tt.Find("Ask Codex"); !ok || tt.HasText("Passed") {
		t.Fatal("obsolete result still invites a review", tt.Texts())
	}
}

func TestReviewSnapshot(t *testing.T) {
	a, out := realApp(t)
	var result review.Result
	if err := json.Unmarshal([]byte(testutil.FailingReview), &result); err != nil {
		t.Fatal(err)
	}
	a.review = reviewState{result: &result, scope: "Uncommitted changes"}
	a.lastCodex = codexReview
	conv := a.newConversation("Review", "Uncommitted changes", reviewTemplate().request)
	conv.Turns[0].Review = &result
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetScale(2)
	tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 68})
	for _, dark := range []bool{false, true} {
		tt.SetDark(dark)
		settle(tt)
		suffix := "-light.png"
		if dark {
			suffix = "-dark.png"
		}
		save(t, out+suffix, trafficLights(tt.Image(), 2))
	}
}

func TestReviewRejectsDirtySubmodules(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	source := testutil.Repo(t)
	testutil.Git(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", source, "vendor/with space")
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Add the submodule")
	// These settings must not hide changes from the review's fingerprint.
	testutil.Git(t, repo, "config", "submodule.vendor/with space.ignore", "all")
	testutil.Git(t, repo, "config", "diff.ignoreSubmodules", "all")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.ID == "" {
		t.Fatal("clean submodule should not prevent reviewing the unpushed commit")
	}
	sub := filepath.Join(repo, "vendor/with space")
	write := func(name, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(sub, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	checkRejected := func() {
		t.Helper()
		current, err := review.ReadSnapshot(repo)
		if err == nil || !strings.Contains(err.Error(), "submodules") || current.ID != "" {
			t.Fatalf("accepted dirty submodule: %+v, %v", current, err)
		}
		a := newApp(repo)
		a.review = reviewState{sourceID: snapshot.ID, result: &review.Result{Verdict: "passed"}}
		a.updateReviewSource(current, err.Error())
		if !a.review.stale {
			t.Fatal("retained a passing verdict for a dirty submodule")
		}
	}
	write("main.go", "package main // first change\n")
	checkRejected()
	write("main.go", "package main // further change\n")
	checkRejected()
	testutil.Git(t, sub, "reset", "--hard", "HEAD")
	write("untracked.txt", "new file\n")
	checkRejected()
	write("untracked.txt", "changed again\n")
	checkRejected()
	if err := os.Remove(filepath.Join(sub, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	if reviewTestSnapshot(t, repo).ID != snapshot.ID {
		t.Fatal("clean submodule did not restore the snapshot")
	}
	run := func(context.Context, string, review.Snapshot) (review.Result, error) {
		write("untracked.txt", "changed during review\n")
		return review.Result{Status: "complete", Summary: "No defects", Findings: []review.Finding{}}, nil
	}
	if _, err := review.Run(context.Background(), repo, snapshot, run); err == nil {
		t.Fatal("accepted a verdict after the submodule became dirty")
	}
}

func TestReviewIncludesIgnoredSubmoduleCommits(t *testing.T) {
	for _, setting := range []string{"diff.ignoreSubmodules", "submodule.vendor.ignore"} {
		t.Run(setting, func(t *testing.T) {
			repo, _ := testutil.RemoteRepo(t)
			testutil.Git(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", testutil.Repo(t), "vendor")
			testutil.Git(t, repo, "add", "--all")
			testutil.Git(t, repo, "commit", "-m", "Add the submodule")
			testutil.Git(t, repo, "push")
			testutil.Git(t, repo, "config", setting, "all")
			testutil.Git(t, repo, "config", "diff.submodule", "short")
			sub := filepath.Join(repo, "vendor")
			commit := func(text string) string {
				t.Helper()
				if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
				testutil.Git(t, sub, "-c", "user.name=Peek", "-c", "user.email=peek@example.com", "-c", "commit.gpgsign=false", "commit", "-am", "Change the implementation")
				return testutil.Git(t, sub, "rev-parse", "HEAD")
			}
			checkScope := func(snapshot review.Snapshot, scope string) {
				t.Helper()
				if snapshot.Scope != scope || snapshot.ID == "" {
					t.Fatalf("submodule commit omitted from %s review: %+v", scope, snapshot)
				}
			}
			first := commit("package main // first revision\n")
			before := reviewTestSnapshot(t, repo)
			checkScope(before, "Uncommitted changes")
			second := commit("package main // second revision\n")
			after := reviewTestSnapshot(t, repo)
			checkScope(after, "Uncommitted changes")
			if before.ID == after.ID {
				t.Fatal("different submodule commits kept the same snapshot")
			}
			a := newApp(repo)
			a.review = reviewState{sourceID: before.ID, result: &review.Result{Verdict: "passed"}}
			a.updateReviewSource(after, "")
			if !a.review.stale {
				t.Fatal("kept an outdated passing verdict")
			}
			run := func(context.Context, string, review.Snapshot) (review.Result, error) {
				testutil.Git(t, sub, "checkout", "--detach", first)
				return review.Result{Status: "complete", Summary: "No defects", Findings: []review.Finding{}}, nil
			}
			if _, err := review.Run(context.Background(), repo, after, run); !errors.Is(err, review.ErrChanged) {
				t.Fatalf("accepted a verdict after switching submodule commits: %v", err)
			}
			testutil.Git(t, sub, "checkout", "--detach", second)
			testutil.Git(t, repo, "add", "vendor")
			checkScope(reviewTestSnapshot(t, repo), "Uncommitted changes")
			testutil.Git(t, repo, "commit", "-m", "Update the submodule")
			checkScope(reviewTestSnapshot(t, repo), "Unpushed commits")
		})
	}
}

func reviewTestSnapshot(t *testing.T, repo string) review.Snapshot {
	t.Helper()
	snapshot, err := review.ReadSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

// The branch's changes, shown, are what a review is of, though pushed;
// the work tree, shown, has its own, and each keeps its result.
func TestReviewBranchChanges(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Update the files")
	testutil.Git(t, repo, "push", "-q", "origin", "main")
	main := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	if err := os.WriteFile(filepath.Join(repo, "run.go"), []byte("package main\n\nfunc run() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add run")
	testutil.Git(t, repo, "push", "-q", "-u", "origin", "feature")
	testutil.FakeReviewCodex(t, testutil.PassingReview)
	prompt := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", prompt)

	a, tt := testApp(t, repo, nil)
	settle(tt)
	if cm, ok := a.current(); !ok || !git.IsRange(cm.Hash) {
		t.Fatalf("shown %+v, want the branch's changes", cm)
	}
	if target, _ := a.reviewTarget(); target.Scope != review.BranchScope || target.Base != main {
		t.Fatalf("target %+v, want the branch's changes since %s", target, main)
	}
	askCodex(t, tt, "Review")
	settle(tt)
	if a.review.scope != review.BranchScope || !tt.HasText("Passed") {
		t.Fatalf("review %+v, %q", a.review, tt.Texts())
	}
	if got, _ := os.ReadFile(prompt); !strings.Contains(string(got), "Scope: Branch changes.") || !strings.Contains(string(got), "Base: "+main) {
		t.Fatalf("prompt %q, want the branch's range", got)
	}

	// The work tree has nothing to review; the branch's result waits.
	a.commitRow = 0
	settle(tt)
	if tt.HasText("Passed") || a.review.result == nil {
		t.Fatalf("branch's result shown for the work tree: %q", tt.Texts())
	}
	a.commitRow = 1
	settle(tt)
	if !tt.HasText("Passed") {
		t.Fatalf("branch's result gone: %q", tt.Texts())
	}

	// Uncommitted changes leave it be; another commit outdates it.
	os.WriteFile(filepath.Join(repo, "draft.txt"), []byte("draft\n"), 0o644)
	a.refresh()
	settle(tt)
	if a.review.stale {
		t.Fatal("uncommitted changes outdated the branch's review")
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add a draft")
	a.refresh()
	settle(tt)
	if !a.review.stale || tt.HasText("Passed") {
		t.Fatalf("a commit kept the branch's review: %+v", a.review)
	}
}

// The Review template, edited, still reviews, and asks what the user
// added.
func TestReviewOwnRequest(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, testutil.PassingReview)
	prompt := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", prompt)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Templates"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Review"); err != nil {
		t.Fatal(err)
	}
	a.reply += " Mind the docs."
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.review.result == nil || !tt.HasText("Passed") {
		t.Fatalf("review %+v, texts %q", a.review, tt.Texts())
	}
	got, _ := os.ReadFile(prompt)
	if !strings.Contains(string(got), "Return only the required JSON object.") || !strings.HasSuffix(strings.TrimSpace(string(got)), "Mind the docs.") {
		t.Fatalf("prompt %q, want the review with the user's request", got)
	}
}
