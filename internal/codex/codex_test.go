package codex

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommitMessagePassesContextWithoutFileContents(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"message":"Update the files"}`)
	promptFile, cwdFile := filepath.Join(t.TempDir(), "prompt"), filepath.Join(t.TempDir(), "cwd")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	t.Setenv("PEEK_TEST_CODEX_CWD", cwdFile)
	for _, first := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing history", true: "first commit"}[first], func(t *testing.T) {
			repo, _ := testutil.RemoteRepo(t)
			if first {
				testutil.Git(t, repo, "switch", "--orphan", "first", "--discard-changes")
			}
			large := strings.Repeat("contents read only from the repository\n", 10000)
			for name, data := range map[string][]byte{"large.txt": []byte(large), "image.png": testutil.PNG(t), "invalid.bin": {0xff, 0xfe, 0}} {
				if err := os.WriteFile(filepath.Join(repo, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var snapshot git.Snapshot
			generate := func(ctx context.Context, directory string, captured git.Snapshot) (string, error) {
				snapshot = captured
				return CommitMessage(ctx, directory, captured)
			}
			if _, err := git.Push(context.Background(), repo, generate, func(string) {}); err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(promptFile)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(prompt) || bytes.IndexByte(prompt, 0) >= 0 || bytes.Contains(prompt, []byte("contents read only from the repository")) || bytes.Contains(prompt, testutil.PNG(t)) || bytes.Contains(prompt, []byte("Initial commit")) {
				t.Fatal("file contents or commit history leaked into the prompt")
			}
			for _, want := range []string{"HEAD commit: " + snapshot.Head + "\n", "Staged tree: " + snapshot.Tree, "git log", "--ignore-submodules=none", "first commit"} {
				if !bytes.Contains(prompt, []byte(want)) {
					t.Fatalf("prompt missing %q", want)
				}
			}
			cwd, err := os.ReadFile(cwdFile)
			if err != nil || string(cwd) != repo {
				t.Fatalf("wrong Codex repository: %q, %v", cwd, err)
			}
			if (snapshot.Head == "") != first || snapshot.Tree != testutil.Git(t, repo, "rev-parse", "HEAD^{tree}") {
				t.Fatal("committed snapshot does not match generation context")
			}
		})
	}
}

// Opt in to a tiny real CLI request; the regular tests use FakeCodex and
// local bare remotes, so they need neither a login nor network access.
func TestCodexSmoke(t *testing.T) {
	if os.Getenv("PEEK_CODEX_SMOKE") != "1" {
		t.Skip("set PEEK_CODEX_SMOKE=1 to check the installed Codex CLI")
	}
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	snapshot := git.Snapshot{Head: testutil.Git(t, repo, "rev-parse", "HEAD"), Tree: testutil.Git(t, repo, "write-tree")}
	message, err := CommitMessage(context.Background(), repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(message) == "" {
		t.Fatal("empty message")
	}
	if testutil.Git(t, repo, "rev-parse", "HEAD") != snapshot.Head || testutil.Git(t, repo, "write-tree") != snapshot.Tree {
		t.Fatal("Codex modified HEAD or the index")
	}
	t.Log(message)
}
