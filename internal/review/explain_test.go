package review

import (
	"context"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
)

func TestCommitSnapshot(t *testing.T) {
	repo := testutil.Repo(t)
	head := testutil.Git(t, repo, "rev-parse", "HEAD")
	snapshot, err := CommitSnapshot(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	// The first commit is from the empty tree.
	if snapshot.Base != testutil.Git(t, repo, "hash-object", "-t", "tree", "/dev/null") || snapshot.Head != head || snapshot.ID == "" {
		t.Fatalf("first commit's snapshot %+v", snapshot)
	}
	testutil.Git(t, repo, "commit", "-q", "--allow-empty", "-m", "Second")
	second := testutil.Git(t, repo, "rev-parse", "HEAD")
	if snapshot, err = CommitSnapshot(repo, second); err != nil || snapshot.Base != head {
		t.Fatalf("second commit's snapshot %+v, %v", snapshot, err)
	}
}

func TestExplain(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"explanation":"  It **starts** the program.  "}`)
	promptFile := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	repo := testutil.Repo(t)
	head := testutil.Git(t, repo, "rev-parse", "HEAD")
	snapshot, err := CommitSnapshot(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	text, err := Explain(context.Background(), repo, snapshot, func(phase, _ string) { phases = append(phases, phase) })
	if err != nil || text != "It **starts** the program." {
		t.Fatalf("explanation %q, %v", text, err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(prompt), "git diff --no-ext-diff --no-textconv "+snapshot.Base+" "+head+" --") {
		t.Errorf("prompt %s", prompt)
	}
	testutil.FakeReviewCodex(t, `{"explanation":" "}`)
	if _, err := Explain(context.Background(), repo, snapshot, nil); err == nil {
		t.Error("an empty explanation is no error")
	}
}
