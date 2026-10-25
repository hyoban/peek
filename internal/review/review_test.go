package review

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
)

func reviewTestSnapshot(t *testing.T, repo string) Snapshot {
	t.Helper()
	snapshot, err := ReadSnapshot(repo)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}

func TestReviewSnapshots(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "main.go")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main\n\nfunc main() { println(1) }\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	index := testutil.Git(t, repo, "diff", "--cached")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.Scope != "Uncommitted changes" || snapshot.ID == "" {
		t.Fatalf("unexpected snapshot: %+v", snapshot)
	}
	if snapshot.Head != testutil.Git(t, repo, "rev-parse", "HEAD") || snapshot.Base != "" {
		t.Fatalf("unexpected review context: %+v", snapshot)
	}
	if testutil.Git(t, repo, "diff", "--cached") != index {
		t.Fatal("review changed the index")
	}
	if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("# Fix\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if reviewTestSnapshot(t, repo).ID == snapshot.ID {
		t.Fatal("editing an untracked file did not invalidate the snapshot")
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	snapshot = reviewTestSnapshot(t, repo)
	if snapshot.Scope != "Unpushed commits" || snapshot.Head != testutil.Git(t, repo, "rev-parse", "HEAD") || snapshot.Base != testutil.Git(t, repo, "rev-parse", "origin/main") {
		t.Fatalf("wrong commit snapshot: %+v", snapshot)
	}
	testutil.Git(t, repo, "push")
	if reviewTestSnapshot(t, repo).ID != "" {
		t.Fatal("already pushed commits should not be reviewed")
	}
	testutil.Git(t, repo, "switch", "-c", "new-feature")
	if reviewTestSnapshot(t, repo).ID != "" {
		t.Fatal("publishing an unchanged branch has no code to review")
	}
	if err := os.WriteFile(filepath.Join(repo, "feature.txt"), []byte("new feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Add the feature")
	snapshot = reviewTestSnapshot(t, repo)
	if snapshot.Scope != "Unpushed commits" || snapshot.Head != testutil.Git(t, repo, "rev-parse", "HEAD") || snapshot.Base != testutil.Git(t, repo, "rev-parse", "origin/main") {
		t.Fatalf("wrong new branch context: %+v", snapshot)
	}
}

func TestReviewEmptyRemote(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	remote := t.TempDir()
	testutil.Git(t, remote, "init", "--bare")
	testutil.Git(t, repo, "remote", "add", "empty", remote)
	testutil.Git(t, repo, "config", "branch.main.pushRemote", "empty")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.Scope != "Unpushed commits" || snapshot.Base != testutil.Git(t, repo, "hash-object", "-t", "tree", "/dev/null") || snapshot.Head != testutil.Git(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("initial push should review the whole tree: %+v", snapshot)
	}
}

func TestReviewRejectsStaleResults(t *testing.T) {
	repo := testutil.Repo(t)
	snapshot := reviewTestSnapshot(t, repo)
	result := Result{Status: "complete", Summary: "No defects", Findings: []Finding{}}
	run := func(context.Context, string, Snapshot) (Result, error) {
		if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("changed during review\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return result, nil
	}
	if _, err := Run(context.Background(), repo, snapshot, run); !errors.Is(err, ErrChanged) {
		t.Fatalf("accepted stale result: %v", err)
	}
	run = func(context.Context, string, Snapshot) (Result, error) {
		t.Fatal("should not review an already outdated snapshot")
		return result, nil
	}
	if _, err := Run(context.Background(), repo, snapshot, run); !errors.Is(err, ErrChanged) {
		t.Fatal(err)
	}
}

func TestReviewValidation(t *testing.T) {
	for _, body := range []string{testutil.PassingReview, testutil.FailingReview} {
		var result Result
		if err := json.Unmarshal([]byte(body), &result); err != nil {
			t.Fatal(err)
		}
		if err := result.validate(); err != nil {
			t.Fatal(err)
		}
	}
	for _, result := range []Result{
		{},
		{Status: "complete", Summary: "No defects"},
		{Status: "passed", Summary: "Passed", Findings: []Finding{}},
		{Status: "inconclusive", Summary: "Could not read the code", Findings: []Finding{}},
		{Status: "complete", Summary: "Found", Findings: []Finding{{Title: "Unresolved defect"}}},
	} {
		if result.validate() == nil {
			t.Fatalf("accepted invalid review %+v", result)
		}
	}
}

// The findings' priorities judge the verdict: minor ones alone pass.
func TestReviewVerdictByPriority(t *testing.T) {
	finding := func(priority int) Finding {
		return Finding{Priority: priority, Title: "Finding", Body: "Body", Path: "a.go", Line: 1}
	}
	for _, tc := range []struct {
		findings []Finding
		verdict  string
	}{
		{[]Finding{}, "passed"},
		{[]Finding{finding(3), finding(3)}, "passed"},
		{[]Finding{finding(3), finding(2)}, "failed"},
		{[]Finding{finding(0)}, "failed"},
	} {
		result := Result{Status: "complete", Summary: "Reviewed", Findings: tc.findings, Verdict: "failed"}
		if err := result.validate(); err != nil || result.Verdict != tc.verdict {
			t.Errorf("%d findings: verdict %q, %v; want %q", len(tc.findings), result.Verdict, err, tc.verdict)
		}
	}
}

func TestCodexReviewSmoke(t *testing.T) {
	if os.Getenv("PEEK_CODEX_SMOKE") != "1" {
		t.Skip("set PEEK_CODEX_SMOKE=1 to check the installed Codex CLI")
	}
	repo := t.TempDir()
	testutil.Git(t, repo, "init", "-b", "main")
	file := filepath.Join(repo, "calc.py")
	if err := os.WriteFile(file, []byte("def average(values):\n    return sum(values) / len(values) if values else 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "-c", "user.name=Peek", "-c", "user.email=peek@example.com", "-c", "commit.gpgsign=false", "commit", "-m", "Handle empty inputs")
	if err := os.WriteFile(file, []byte("def average(values):\n    return sum(values) / len(values)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "asset.png"), testutil.PNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	snapshot := reviewTestSnapshot(t, repo)
	var phases []string
	run := func(ctx context.Context, repo string, snapshot Snapshot) (Result, error) {
		return Codex(ctx, repo, snapshot, "", func(phase, detail string) {
			phases = append(phases, phase)
		})
	}
	result, err := Run(context.Background(), repo, snapshot, run)
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

// The branch's changes are a snapshot apart, which only commits move.
func TestBranchSnapshot(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Update the files")
	testutil.Git(t, repo, "push", "-q", "origin", "main")
	base := testutil.Git(t, repo, "rev-parse", "HEAD")
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	testutil.Git(t, repo, "commit", "-q", "--allow-empty", "-m", "Empty")
	testutil.Git(t, repo, "push", "-q", "-u", "origin", "feature")
	snapshot, err := ReadBranchSnapshot(repo)
	if err != nil || snapshot.ID == "" || snapshot.Scope != BranchScope || snapshot.Base != base || snapshot.Head != testutil.Git(t, repo, "rev-parse", "HEAD") {
		t.Fatalf("snapshot %+v, %v", snapshot, err)
	}
	if !strings.Contains(snapshot.context(), "git diff --no-ext-diff --no-textconv --ignore-submodules=none "+base+" "+snapshot.Head) {
		t.Fatalf("context %q", snapshot.context())
	}
	os.WriteFile(filepath.Join(repo, "draft.txt"), []byte("draft\n"), 0o644)
	run := func(context.Context, string, Snapshot) (Result, error) {
		return Result{Status: "complete", Summary: "No defects", Findings: []Finding{}}, nil
	}
	if _, err := Run(context.Background(), repo, snapshot, run); err != nil {
		t.Fatalf("uncommitted changes failed the branch's review: %v", err)
	}
	moved := func(context.Context, string, Snapshot) (Result, error) {
		testutil.Git(t, repo, "commit", "-q", "--allow-empty", "-m", "Another")
		return Result{Status: "complete", Summary: "No defects", Findings: []Finding{}}, nil
	}
	if _, err := Run(context.Background(), repo, snapshot, moved); !errors.Is(err, ErrChanged) {
		t.Fatalf("a commit during the review: %v", err)
	}
}
