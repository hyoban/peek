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

func TestAsk(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"answer":"  It **starts** the program.  "}`)
	promptFile := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	repo := testutil.Repo(t)
	head := testutil.Git(t, repo, "rev-parse", "HEAD")
	snapshot, err := CommitSnapshot(repo, head)
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	text, _, err := Ask(context.Background(), repo, "", snapshot, "Explain the changes.", func(phase, _ string) { phases = append(phases, phase) }, nil)
	if err != nil || text != "It **starts** the program." {
		t.Fatalf("answer %q, %v", text, err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(prompt), "Explain the changes.\n") ||
		!strings.Contains(string(prompt), "git diff --no-ext-diff --no-textconv "+snapshot.Base+" "+head+" --") {
		t.Errorf("prompt %s", prompt)
	}
	testutil.FakeReviewCodex(t, `{"answer":" "}`)
	if _, _, err := Ask(context.Background(), repo, "", snapshot, "Explain the changes.", nil, nil); err == nil {
		t.Error("an empty answer is no error")
	}
}
