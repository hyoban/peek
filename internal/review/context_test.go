package review

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestReviewPassesContextWithoutFileContents(t *testing.T) {
	testutil.FakeReviewCodex(t, testutil.PassingReview)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"image.png", testutil.PNG(t)},
		{"invalid.txt", []byte{0xff, 0xfe, 'x'}},
		{"nul.bin", []byte{'a', 0, 'b'}},
		{"unicode.txt", []byte("上下文只在仓库中读取")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testutil.Repo(t)
			file := filepath.Join(repo, test.name)
			if err := os.WriteFile(file, test.data, 0o644); err != nil {
				t.Fatal(err)
			}
			snapshot := reviewTestSnapshot(t, repo)
			index := testutil.Git(t, repo, "diff", "--cached")
			if _, err := Run(context.Background(), repo, snapshot, codexReview); err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(promptFile)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(prompt) || bytes.IndexByte(prompt, 0) >= 0 || bytes.Contains(prompt, test.data) {
				t.Fatal("file contents leaked into the prompt")
			}
			for _, want := range []string{"Uncommitted changes", snapshot.Head, "--ignore-submodules=none", "untracked files directly"} {
				if !strings.Contains(string(prompt), want) {
					t.Fatalf("prompt missing review context %q", want)
				}
			}
			if got := testutil.Git(t, repo, "diff", "--cached"); got != index {
				t.Fatal("review modified the index")
			}
			changed := bytes.Clone(test.data)
			changed[len(changed)-1] ^= 1
			if err := os.WriteFile(file, changed, 0o644); err != nil {
				t.Fatal(err)
			}
			if reviewTestSnapshot(t, repo).ID == snapshot.ID {
				t.Fatal("same-size content change did not invalidate the snapshot")
			}
		})
	}
}

func TestReviewContextPinsCommitRange(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	snapshot := reviewTestSnapshot(t, repo)
	testutil.FakeReviewCodex(t, testutil.PassingReview)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	if _, err := Run(context.Background(), repo, snapshot, codexReview); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prompt, []byte("Base: "+snapshot.Base)) || !bytes.Contains(prompt, []byte("Head: "+snapshot.Head)) || bytes.Contains(prompt, []byte("package main")) {
		t.Fatalf("unexpected commit review prompt: %s", prompt)
	}
}

func TestReviewFingerprintFollowsRenamedFilesAndIndex(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	name := "1 renamed\nwith spaces.go"
	testutil.Git(t, repo, "mv", "main.go", name)
	before := reviewTestSnapshot(t, repo)
	if err := os.WriteFile(filepath.Join(repo, name), []byte("package main // renamed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := reviewTestSnapshot(t, repo)
	if before.ID == after.ID {
		t.Fatal("renamed file contents were not fingerprinted")
	}
	testutil.Git(t, repo, "add", name)
	if reviewTestSnapshot(t, repo).ID == after.ID {
		t.Fatal("staged content change was not fingerprinted")
	}
}

func TestReviewFingerprintFileDirectoryReplacement(t *testing.T) {
	for _, replacement := range []string{"file to directory", "directory to file", "deleted and recreated file"} {
		for _, staged := range []bool{false, true} {
			name := replacement + "/unstaged"
			if staged {
				name = replacement + "/staged"
			}
			t.Run(name, func(t *testing.T) {
				repo, _ := testutil.RemoteRepo(t)
				path := filepath.Join(repo, "config")
				original := path
				if replacement == "directory to file" {
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
					original = filepath.Join(path, "settings.json")
				}
				if err := os.WriteFile(original, []byte("original\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				testutil.Git(t, repo, "add", "--all")
				testutil.Git(t, repo, "commit", "-m", "Add config")
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if replacement == "deleted and recreated file" {
					testutil.Git(t, repo, "add", "--all")
				}
				changed := path
				if replacement == "file to directory" {
					if err := os.Mkdir(path, 0o755); err != nil {
						t.Fatal(err)
					}
					changed = filepath.Join(path, "settings.json")
				}
				if err := os.WriteFile(changed, []byte("replacement one\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if staged {
					testutil.Git(t, repo, "add", "--all")
				}
				snapshot := reviewTestSnapshot(t, repo)
				run := func(context.Context, string, Snapshot) (Result, error) {
					return Result{Status: "complete", Summary: "Reviewed", Findings: []Finding{}}, nil
				}
				if _, err := Run(context.Background(), repo, snapshot, run); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(changed, []byte("replacement two\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				updated := reviewTestSnapshot(t, repo)
				if updated.ID == snapshot.ID {
					t.Fatal("replacement content change did not invalidate the review")
				}
				// The second edit leaves porcelain status unchanged, including
				// after an initially staged replacement acquires unstaged edits.
				if err := os.WriteFile(changed, []byte("replacement six\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if reviewTestSnapshot(t, repo).ID == updated.ID {
					t.Fatal("same-status content change did not invalidate the review")
				}
			})
		}
	}
}
