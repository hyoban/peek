package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommitMessagePassesContextWithoutFileContents(t *testing.T) {
	fakeReviewCodex(t, `{"message":"Update the files"}`)
	promptFile, cwdFile := filepath.Join(t.TempDir(), "prompt"), filepath.Join(t.TempDir(), "cwd")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	t.Setenv("PEEK_TEST_CODEX_CWD", cwdFile)
	for _, first := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing history", true: "first commit"}[first], func(t *testing.T) {
			repo, _ := pushTestRepo(t)
			if first {
				pushTestGit(t, repo, "switch", "--orphan", "first", "--discard-changes")
			}
			large := strings.Repeat("contents read only from the repository\n", 10000)
			for name, data := range map[string][]byte{"large.txt": []byte(large), "image.png": reviewTestPNG(t), "invalid.bin": {0xff, 0xfe, 0}} {
				if err := os.WriteFile(filepath.Join(repo, name), data, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var snapshot commitSnapshot
			generate := func(ctx context.Context, directory string, captured commitSnapshot) (string, error) {
				snapshot = captured
				return codexCommitMessage(ctx, directory, captured)
			}
			if _, err := pushRepository(context.Background(), repo, generate, ignoreProgress); err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(promptFile)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(prompt) || bytes.IndexByte(prompt, 0) >= 0 || bytes.Contains(prompt, []byte("contents read only from the repository")) || bytes.Contains(prompt, reviewTestPNG(t)) || bytes.Contains(prompt, []byte("Initial commit")) {
				t.Fatal("file contents or commit history leaked into the prompt")
			}
			for _, want := range []string{"HEAD commit: " + snapshot.head + "\n", "Staged tree: " + snapshot.tree, "git log", "--ignore-submodules=none", "first commit"} {
				if !bytes.Contains(prompt, []byte(want)) {
					t.Fatalf("prompt missing %q", want)
				}
			}
			cwd, err := os.ReadFile(cwdFile)
			if err != nil || string(cwd) != repo {
				t.Fatalf("wrong Codex repository: %q, %v", cwd, err)
			}
			if (snapshot.head == "") != first || snapshot.tree != pushTestGit(t, repo, "rev-parse", "HEAD^{tree}") {
				t.Fatal("committed snapshot does not match generation context")
			}
		})
	}
}

// Opt in to a tiny real CLI request; the regular tests use fakeCodex and
// local bare remotes, so they need neither a login nor network access.
func TestCodexSmoke(t *testing.T) {
	if os.Getenv("PEEK_CODEX_SMOKE") != "1" {
		t.Skip("set PEEK_CODEX_SMOKE=1 to check the installed Codex CLI")
	}
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	snapshot := commitSnapshot{head: pushTestGit(t, repo, "rev-parse", "HEAD"), tree: pushTestGit(t, repo, "write-tree")}
	message, err := codexCommitMessage(context.Background(), repo, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(message) == "" {
		t.Fatal("empty message")
	}
	if pushTestGit(t, repo, "rev-parse", "HEAD") != snapshot.head || pushTestGit(t, repo, "write-tree") != snapshot.tree {
		t.Fatal("Codex modified HEAD or the index")
	}
	t.Log(message)
}
