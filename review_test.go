package main

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

const passingReview = `{"verdict":"passed","summary":"No actionable defects found.","findings":[]}`
const failingReview = `{"verdict":"failed","summary":"Empty inputs now crash.","findings":[{"priority":2,"title":"Handle empty inputs","body":"Restore the guard so an empty list does not divide by zero.","path":"calc.py","line":2}]}`

func reviewTestSnapshot(t *testing.T, repo string) reviewSnapshot {
	t.Helper()
	snapshot, err := readReviewSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReviewSnapshots(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "main.go")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := pushTestGit(t, repo, "diff", "--cached")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.scope != "Uncommitted changes" || snapshot.id == "" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.head != pushTestGit(t, repo, "rev-parse", "HEAD") || snapshot.base != "" {
		t.Fatalf("unexpected review context: %+v", snapshot)
	}
	if pushTestGit(t, repo, "diff", "--cached") != index {
		t.Fatal("review changed the index")
	}
	if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("# Fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reviewTestSnapshot(t, repo).id == snapshot.id {
		t.Fatal("editing an untracked file did not invalidate the snapshot")
	}
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	snapshot = reviewTestSnapshot(t, repo)
	if snapshot.scope != "Unpushed commits" || snapshot.head != pushTestGit(t, repo, "rev-parse", "HEAD") || snapshot.base != pushTestGit(t, repo, "rev-parse", "origin/main") {
		t.Fatalf("wrong commit snapshot: %+v", snapshot)
	}
	pushTestGit(t, repo, "push")
	if reviewTestSnapshot(t, repo).id != "" {
		t.Fatal("already pushed commits should not be reviewed")
	}
	pushTestGit(t, repo, "switch", "-c", "new-feature")
	if reviewTestSnapshot(t, repo).id != "" {
		t.Fatal("publishing an unchanged branch has no code to review")
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("new feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Add the feature")
	snapshot = reviewTestSnapshot(t, repo)
	if snapshot.scope != "Unpushed commits" || snapshot.head != pushTestGit(t, repo, "rev-parse", "HEAD") || snapshot.base != pushTestGit(t, repo, "rev-parse", "origin/main") {
		t.Fatalf("wrong new branch context: %+v", snapshot)
	}
}

func TestReviewEmptyRemote(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	remote := t.TempDir()
	pushTestGit(t, remote, "init", "--bare")
	pushTestGit(t, repo, "remote", "add", "empty", remote)
	pushTestGit(t, repo, "config", "branch.main.pushRemote", "empty")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.scope != "Unpushed commits" || snapshot.base != pushTestGit(t, repo, "hash-object", "-t", "tree", "/dev/null") || snapshot.head != pushTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("initial push should review the whole tree: %+v", snapshot)
	}
}

func TestReviewRejectsStaleResults(t *testing.T) {
	repo := testRepo(t)
	snapshot := reviewTestSnapshot(t, repo)
	result := reviewResult{Verdict: "passed", Summary: "No defects", Findings: []reviewFinding{}}
	run := func(context.Context, string, reviewSnapshot) (reviewResult, error) {
		if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("changed during review\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
	if _, err := reviewRepository(context.Background(), repo, snapshot, run); !errors.Is(err, errReviewChanged) {
		t.Fatalf("accepted stale result: %v", err)
	}
	run = func(context.Context, string, reviewSnapshot) (reviewResult, error) {
		t.Fatal("should not review an already outdated snapshot")
		return result, nil
	}
	if _, err := reviewRepository(context.Background(), repo, snapshot, run); !errors.Is(err, errReviewChanged) {
		t.Fatal(err)
	}
}

func TestReviewValidation(t *testing.T) {
	for _, body := range []string{passingReview, failingReview} {
		var result reviewResult
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		if err := result.validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range []reviewResult{
		{},
		{Verdict: "passed", Summary: "No defects"},
		{Verdict: "failed", Summary: "Failed", Findings: []reviewFinding{}},
		{Verdict: "inconclusive", Summary: "Could not read the code", Findings: []reviewFinding{}},
		{Verdict: "passed", Summary: "Passed", Findings: []reviewFinding{{Title: "Unresolved defect"}}},
	} {
		if result.validate() == nil {
			t.Fatalf("accepted invalid review %+v", result)
		}
	}
}

func fakeReviewCodex(t *testing.T, response string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "response.json")
	if err := os.WriteFile(file, []byte(response), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PEEK_TEST_REVIEW_RESPONSE", file)
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) shift; output="$1" ;;
    --sandbox) shift; test "$1" = read-only || exit 2 ;;
    --json) json=1 ;;
    --cd) shift; if [ -n "$PEEK_TEST_CODEX_CWD" ]; then printf '%s' "$1" > "$PEEK_TEST_CODEX_CWD"; fi ;;
  esac
  shift
done
if [ -n "$PEEK_TEST_REVIEW_PROMPT" ]; then
  cat > "$PEEK_TEST_REVIEW_PROMPT"
else
  cat >/dev/null
fi
if [ -n "$PEEK_TEST_CODEX_EVENTS" ]; then
  test "$json" = 1 || exit 3
  cat "$PEEK_TEST_CODEX_EVENTS"
  attempts=0
  while [ ! -f "$PEEK_TEST_CODEX_ACK" ] && [ "$attempts" -lt 100 ]; do
    sleep 0.01
    attempts=$((attempts + 1))
  done
  test -f "$PEEK_TEST_CODEX_ACK" || exit 4
fi
cp "$PEEK_TEST_REVIEW_RESPONSE" "$output"
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestReviewUI(t *testing.T) {
	repo := testRepo(t)
	fakeReviewCodex(t, passingReview)
	a, tt := testApp(t, repo, nil)
	tt.SetClipboard("Keep this clipboard")
	if err := tt.Click("Review"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.review.running || !tt.HasText("Passed") || a.review.open {
		t.Fatalf("unexpected result: %+v, %q", a.review, tt.Texts())
	}
	if tt.Clipboard() != "Keep this clipboard" {
		t.Fatal("passing review changed the clipboard")
	}
	if err := tt.Click("Passed"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("No actionable defects found.") {
		t.Fatal("missing review details")
	}
	if err := tt.Click("Close"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	tt.Frame()
	if !tt.HasText("Review again") || tt.HasText("Passed") {
		t.Fatal("stale verdict still shown", tt.Texts())
	}
	if err := os.WriteFile(os.Getenv("PEEK_TEST_REVIEW_RESPONSE"), []byte(failingReview), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Review again"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Not passed") {
		t.Fatal("missing failing verdict", tt.Texts())
	}
	wantClipboard := "Review not passed\nUncommitted changes\n\nEmpty inputs now crash.\n\n[P2] Handle empty inputs\ncalc.py:2\nRestore the guard so an empty list does not divide by zero."
	if got := tt.Clipboard(); got != wantClipboard {
		t.Fatalf("review not copied in full: %q", got)
	}
	tt.SetClipboard("Copied something else")
	if err := tt.Click("Not passed"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("calc.py:2") || !tt.HasText("[P2] Handle empty inputs") {
		t.Fatal("missing findings", tt.Texts())
	}
	if err := os.WriteFile(os.Getenv("PEEK_TEST_REVIEW_RESPONSE"), []byte("not JSON"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Review again"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Review error") || tt.HasText("Passed") {
		t.Fatal("invalid result treated as a verdict", tt.Texts())
	}
	if tt.Clipboard() != "Copied something else" {
		t.Fatal("redraw or review error overwrote the clipboard")
	}
}

func TestReviewCancelAndLayout(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []commit{{Subject: "Uncommitted Changes"}}
	a.loadedRow, a.commitRow = 0, 0
	a.canPush = true
	for _, width := range []int{640, 1200} {
		tt := ui.NewTester(a.view, width, 800)
		tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 78})
		tt.Frame()
		mode, ok := tt.Find("Changes")
		if !ok {
			t.Fatal("missing mode switch")
		}
		cancelled := false
		a.review = reviewState{running: true, cancel: func() { cancelled = true }}
		tt.Frame()
		if err := tt.Click("Reviewing…"); err != nil {
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
			{result: &reviewResult{Verdict: "passed", Summary: "Passed"}},
			{result: &reviewResult{Verdict: "failed", Summary: "Failed"}},
			{},
		} {
			a.review = state
			tt.Frame()
			if got, ok := tt.Find("Changes"); !ok || got != mode {
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
		cancelled = false
		if err := tt.Click("Inspecting… 12s"); err != nil || !cancelled {
			t.Fatalf("progress button did not cancel: %v", err)
		}
		if got, ok := tt.Find("Changes"); !ok || got != mode {
			t.Fatal("progress moved the mode switch")
		}
		a.review = reviewState{result: &reviewResult{Verdict: "passed", Summary: "Passed"}}
		tt.Advance(time.Second)
		tt.Frame()
		if !tt.HasText("Passed") || tt.HasText("Inspecting… 13s") {
			t.Fatal("completed review still shows progress", tt.Texts())
		}
	}
}

func TestCodexReviewSmoke(t *testing.T) {
	if os.Getenv("PEEK_CODEX_SMOKE") != "1" {
		t.Skip("set PEEK_CODEX_SMOKE=1 to check the installed Codex CLI")
	}
	repo := t.TempDir()
	pushTestGit(t, repo, "init", "-b", "main")
	file := filepath.Join(repo, "calc.py")
	if err := os.WriteFile(file, []byte("def average(values):\n    return sum(values) / len(values) if values else 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pushTestGit(t, repo, "add", ".")
	pushTestGit(t, repo, "-c", "user.name=Peek", "-c", "user.email=peek@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Handle empty inputs")
	if err := os.WriteFile(file, []byte("def average(values):\n    return sum(values) / len(values)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "asset.png"), reviewTestPNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := reviewTestSnapshot(t, repo)
	var phases []string
	run := func(ctx context.Context, repo string, snapshot reviewSnapshot) (reviewResult, error) {
		return codexReviewProgress(ctx, repo, snapshot, func(phase, detail string) {
			phases = append(phases, phase)
		})
	}
	result, err := reviewRepository(context.Background(), repo, snapshot, run)
	if err != nil {
		t.Fatal(err)
	}
	if result.Verdict != "failed" || len(result.Findings) == 0 {
		t.Fatalf("missed empty-input regression: %+v", result)
	}
	if len(phases) == 0 {
		t.Fatal("no live progress received from Codex")
	}
	t.Log("Progress:", phases)
	t.Log(result.Summary)
}

func TestReviewNothingToDo(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	pushTestGit(t, repo, "push")
	a, tt := testApp(t, repo, nil)
	_ = tt.Click("Review")
	a.startReview(ui.Services{})
	tt.Frame()
	if a.reviewSource.id != "" || a.review.running || a.review.err != "" || a.review.result != nil {
		t.Fatalf("review ran with nothing to review: %+v", a.review)
	}
	a.review = reviewState{sourceID: "old", result: &reviewResult{Verdict: "passed"}}
	a.updateReviewSource(reviewSnapshot{}, "")
	tt.Frame()
	if !tt.HasText("Review") || tt.HasText("Review again") || tt.HasText("Passed") {
		t.Fatal("obsolete result still invites a review", tt.Texts())
	}
}

func TestReviewSnapshot(t *testing.T) {
	a, out := realApp(t)
	var result reviewResult
	if err := json.Unmarshal([]byte(failingReview), &result); err != nil {
		t.Fatal(err)
	}
	a.review = reviewState{result: &result, scope: "Uncommitted changes", open: true}
	tt := ui.NewTester(a.view, 1280, 800)
	tt.SetScale(2)
	tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 78})
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
