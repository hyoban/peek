package codex

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"peek/internal/filekind"
	"peek/internal/git"
	"peek/internal/testutil"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommitMessageEmbedsSmallChanges(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"message":"Update the files"}`)
	promptFile, cwdFile := filepath.Join(t.TempDir(), "prompt"), filepath.Join(t.TempDir(), "cwd")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	t.Setenv("PEEK_TEST_CODEX_CWD", cwdFile)
	large := strings.Repeat("contents read only from the repository\n", 10000)
	for _, tc := range []struct {
		name         string
		first, large bool
	}{{"small change", false, false}, {"large change", false, true}, {"first commit", true, false}} {
		t.Run(tc.name, func(t *testing.T) {
			repo, _ := testutil.RemoteRepo(t)
			if tc.first {
				testutil.Git(t, repo, "switch", "--orphan", "first", "--discard-changes")
			}
			files := map[string][]byte{"small.txt": []byte("a small embedded line\n"), "image.png": testutil.PNG(t), "invalid.txt": {'o', 'k', 0xff, 0xfe, '\n'}}
			if tc.large {
				files["large.txt"] = []byte(large)
			}
			for name, data := range files {
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
			if !utf8.Valid(prompt) || bytes.IndexByte(prompt, 0) >= 0 || bytes.Contains(prompt, testutil.PNG(t)) {
				t.Fatal("binary or invalid bytes in the prompt")
			}
			want := []string{"HEAD commit: " + snapshot.Head + "\n", "Staged tree: " + snapshot.Tree}
			if tc.first {
				want = append(want, "first commit")
			} else {
				want = append(want, "Recent commit subjects:\nInitial commit")
			}
			if tc.large {
				want = append(want, "Summary:\n", "large.txt", "+a small embedded line", "diff is cut here", "--ignore-submodules=none", "git show "+snapshot.Tree)
				if bytes.Count(prompt, []byte("contents read only from the repository")) > groupCaps[filekind.Docs]/39 {
					t.Fatal("a file's diff was embedded past its cap")
				}
			} else {
				want = append(want, "+a small embedded line", "do not run commands")
			}
			for _, w := range want {
				if !bytes.Contains(prompt, []byte(w)) {
					t.Fatalf("prompt missing %q:\n%s", w, prompt)
				}
			}
			cwd, err := os.ReadFile(cwdFile)
			if err != nil || string(cwd) != repo {
				t.Fatalf("wrong Codex repository: %q, %v", cwd, err)
			}
			if (snapshot.Head == "") != tc.first || snapshot.Tree != testutil.Git(t, repo, "rev-parse", "HEAD^{tree}") {
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

// The staged .gitattributes decides, though it is new in this commit.
func TestCommitMessageFollowsStagedAttributes(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"message":"Update the files"}`)
	promptFile := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	repo, _ := testutil.RemoteRepo(t)
	for name, data := range map[string]string{".gitattributes": "api/*.go linguist-generated\n", "api/client.go": "package api // marked\n", "main.go": "package main // code\n"} {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, name), []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := git.Push(context.Background(), repo, CommitMessage, func(string) {}); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	bulk := bytes.Index(prompt, []byte("## Lockfiles and generated files"))
	if bulk < 0 || bytes.Index(prompt, []byte("// marked")) < bulk || bytes.Index(prompt, []byte("// code")) > bulk {
		t.Fatalf("staged attributes not followed:\n%s", prompt)
	}
}

// Git before 2.40 rejects check-attr --source; Ship then groups by the
// common rules rather than failing.
func TestCommitMessageWithoutCheckAttrSource(t *testing.T) {
	testutil.FakeReviewCodex(t, `{"message":"Update the files"}`)
	promptFile := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	repo, _ := testutil.RemoteRepo(t)
	real, err := exec.LookPath("git")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	script := "#!/bin/sh\nfor a in \"$@\"; do [ \"$a\" = check-attr ] && { echo 'error: unknown option source' >&2; exit 129; }; done\nexec " + real + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // code\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := git.Push(context.Background(), repo, CommitMessage, func(string) {}); err != nil {
		t.Fatal(err)
	}
	if prompt, err := os.ReadFile(promptFile); err != nil || !bytes.Contains(prompt, []byte("## Code\n")) || !bytes.Contains(prompt, []byte("// code")) {
		t.Fatalf("diffs not grouped without check-attr: %v\n%s", err, prompt)
	}
}
