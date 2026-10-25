package main

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

func reviewTestPNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

func TestReviewPassesContextWithoutFileContents(t *testing.T) {
	fakeReviewCodex(t, passingReview)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	for _, test := range []struct {
		name string
		data []byte
	}{
		{"image.png", reviewTestPNG(t)},
		{"invalid.txt", []byte{0xff, 0xfe, 'x'}},
		{"nul.bin", []byte{'a', 0, 'b'}},
		{"unicode.txt", []byte("上下文只在仓库中读取")},
	} {
		t.Run(test.name, func(t *testing.T) {
			repo := testRepo(t)
			file := filepath.Join(repo, test.name)
			if err := os.WriteFile(file, test.data, 0o644); err != nil {
				t.Fatal(err)
			}
			snapshot := reviewTestSnapshot(t, repo)
			index := pushTestGit(t, repo, "diff", "--cached")
			if _, err := reviewRepository(context.Background(), repo, snapshot, codexReview); err != nil {
				t.Fatal(err)
			}
			prompt, err := os.ReadFile(promptFile)
			if err != nil {
				t.Fatal(err)
			}
			if !utf8.Valid(prompt) || bytes.IndexByte(prompt, 0) >= 0 || bytes.Contains(prompt, test.data) {
				t.Fatal("file contents leaked into the prompt")
			}
			for _, want := range []string{"Uncommitted changes", snapshot.head, "--ignore-submodules=none", "untracked files directly"} {
				if !strings.Contains(string(prompt), want) {
					t.Fatalf("prompt missing review context %q", want)
				}
			}
			if got := pushTestGit(t, repo, "diff", "--cached"); got != index {
				t.Fatal("review modified the index")
			}
			changed := bytes.Clone(test.data)
			changed[len(changed)-1] ^= 1
			if err := os.WriteFile(file, changed, 0o644); err != nil {
				t.Fatal(err)
			}
			if reviewTestSnapshot(t, repo).id == snapshot.id {
				t.Fatal("same-size content change did not invalidate the snapshot")
			}
		})
	}
}

func TestReviewContextPinsCommitRange(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	snapshot := reviewTestSnapshot(t, repo)
	fakeReviewCodex(t, passingReview)
	promptFile := filepath.Join(t.TempDir(), "prompt.txt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", promptFile)
	if _, err := reviewRepository(context.Background(), repo, snapshot, codexReview); err != nil {
		t.Fatal(err)
	}
	prompt, err := os.ReadFile(promptFile)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(prompt, []byte("Base: "+snapshot.base)) || !bytes.Contains(prompt, []byte("Head: "+snapshot.head)) || bytes.Contains(prompt, []byte("package main")) {
		t.Fatalf("unexpected commit review prompt: %s", prompt)
	}
}

func TestReviewFingerprintFollowsRenamedFilesAndIndex(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	name := "1 renamed\nwith spaces.go"
	pushTestGit(t, repo, "mv", "main.go", name)
	before := reviewTestSnapshot(t, repo)
	if err := os.WriteFile(filepath.Join(repo, name), []byte("package main // renamed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	after := reviewTestSnapshot(t, repo)
	if before.id == after.id {
		t.Fatal("renamed file contents were not fingerprinted")
	}
	pushTestGit(t, repo, "add", name)
	if reviewTestSnapshot(t, repo).id == after.id {
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
				repo, _ := pushTestRepo(t)
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
				pushTestGit(t, repo, "add", "--all")
				pushTestGit(t, repo, "commit", "-m", "Add config")
				if err := os.RemoveAll(path); err != nil {
					t.Fatal(err)
				}
				if replacement == "deleted and recreated file" {
					pushTestGit(t, repo, "add", "--all")
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
					pushTestGit(t, repo, "add", "--all")
				}
				snapshot := reviewTestSnapshot(t, repo)
				run := func(context.Context, string, reviewSnapshot) (reviewResult, error) {
					return reviewResult{Verdict: "passed", Summary: "Reviewed", Findings: []reviewFinding{}}, nil
				}
				if _, err := reviewRepository(context.Background(), repo, snapshot, run); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(changed, []byte("replacement two\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				updated := reviewTestSnapshot(t, repo)
				if updated.id == snapshot.id {
					t.Fatal("replacement content change did not invalidate the review")
				}
				// The second edit leaves porcelain status unchanged, including
				// after an initially staged replacement acquires unstaged edits.
				if err := os.WriteFile(changed, []byte("replacement six\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				if reviewTestSnapshot(t, repo).id == updated.id {
					t.Fatal("same-status content change did not invalidate the review")
				}
			})
		}
	}
}
