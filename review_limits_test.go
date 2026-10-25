package main

import (
	"context"
	"crypto/rand"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestReviewRejectsDirtySubmodules(t *testing.T) {
	repo, _ := pushTestRepo(t)
	source := testRepo(t)
	pushTestGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", source, "vendor/with space")
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Add the submodule")
	// These settings must not hide changes from the review's fingerprint.
	pushTestGit(t, repo, "config", "submodule.vendor/with space.ignore", "all")
	pushTestGit(t, repo, "config", "diff.ignoreSubmodules", "all")
	snapshot := reviewTestSnapshot(t, repo)
	if snapshot.id == "" {
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
		current, err := readReviewSnapshot(repo)
		if err == nil || !strings.Contains(err.Error(), "submodules") || current.id != "" {
			t.Fatalf("accepted dirty submodule: %+v, %v", current, err)
		}
		a := newApp(repo)
		a.review = reviewState{sourceID: snapshot.id, result: &reviewResult{Verdict: "passed"}}
		a.updateReviewSource(current, err.Error())
		if !a.review.stale {
			t.Fatal("retained a passing verdict for a dirty submodule")
		}
	}
	write("main.go", "package main // first change\n")
	checkRejected()
	write("main.go", "package main // further change\n")
	checkRejected()
	pushTestGit(t, sub, "reset", "--hard", "HEAD")
	write("untracked.txt", "new file\n")
	checkRejected()
	write("untracked.txt", "changed again\n")
	checkRejected()
	if err := os.Remove(filepath.Join(sub, "untracked.txt")); err != nil {
		t.Fatal(err)
	}
	if reviewTestSnapshot(t, repo).id != snapshot.id {
		t.Fatal("clean submodule did not restore the snapshot")
	}
	run := func(context.Context, string, reviewSnapshot) (reviewResult, error) {
		write("untracked.txt", "changed during review\n")
		return reviewResult{Verdict: "passed", Summary: "No defects", Findings: []reviewFinding{}}, nil
	}
	if _, err := reviewRepository(context.Background(), repo, snapshot, run); err == nil {
		t.Fatal("accepted a verdict after the submodule became dirty")
	}
}

func TestReviewLargeFiles(t *testing.T) {
	for _, scenario := range []string{"unstaged", "staged", "binary", "untracked", "combined", "unpushed"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _ := pushTestRepo(t)
			name := "main.go"
			data := []byte(strings.Repeat("a line\n", maxReviewMetadata/7+1))
			if scenario == "binary" {
				data = make([]byte, 2*maxReviewMetadata)
				if _, err := rand.Read(data); err != nil {
					t.Fatal(err)
				}
				data[0] = 0
			}
			if scenario == "untracked" {
				name = "large.txt"
			}
			if scenario == "combined" {
				data = []byte(strings.Repeat("staged\n", 60000))
			}
			if err := os.WriteFile(filepath.Join(repo, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if scenario == "staged" || scenario == "combined" || scenario == "unpushed" {
				pushTestGit(t, repo, "add", "--all")
			}
			if scenario == "combined" {
				if err := os.WriteFile(filepath.Join(repo, name), []byte(strings.Repeat("working\n", 60000)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unpushed" {
				pushTestGit(t, repo, "commit", "-m", "Add the large file")
			}
			snapshot, err := readReviewSnapshot(repo)
			if err != nil || snapshot.id == "" {
				t.Fatalf("could not fingerprint large %s changes: id=%q, err=%v", scenario, snapshot.id, err)
			}
		})
	}
}

func TestReviewGitStopsOversizedOutput(t *testing.T) {
	dir := t.TempDir()
	// Keep running after producing the output: merely stopping the reader
	// without terminating its producer would wait for the command timeout.
	script := `#!/bin/sh
i=0
while [ "$i" -lt 4096 ]; do
  printf '%01024d' 0
  i=$((i+1))
done
while :; do :; done
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	out, err := reviewGit(dir, 1024, "diff")
	if !errors.Is(err, errReviewTooLarge) || out != "" {
		t.Fatalf("returned truncated output as a review: %q, %v", out, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("did not terminate the oversized producer promptly: %v", elapsed)
	}
}

func TestReviewIncludesIgnoredSubmoduleCommits(t *testing.T) {
	for _, setting := range []string{"diff.ignoreSubmodules", "submodule.vendor.ignore"} {
		t.Run(setting, func(t *testing.T) {
			repo, _ := pushTestRepo(t)
			pushTestGit(t, repo, "-c", "protocol.file.allow=always", "submodule", "add", testRepo(t), "vendor")
			pushTestGit(t, repo, "add", "--all")
			pushTestGit(t, repo, "commit", "-m", "Add the submodule")
			pushTestGit(t, repo, "push")
			pushTestGit(t, repo, "config", setting, "all")
			pushTestGit(t, repo, "config", "diff.submodule", "short")
			sub := filepath.Join(repo, "vendor")
			commit := func(text string) string {
				t.Helper()
				if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
				pushTestGit(t, sub, "-c", "user.name=Peek", "-c", "user.email=peek@example.com", "-c", "commit.gpgsign=false", "commit", "-am", "Change the implementation")
				return pushTestGit(t, sub, "rev-parse", "HEAD")
			}
			checkScope := func(snapshot reviewSnapshot, scope string) {
				t.Helper()
				if snapshot.scope != scope || snapshot.id == "" {
					t.Fatalf("submodule commit omitted from %s review: %+v", scope, snapshot)
				}
			}
			first := commit("package main // first revision\n")
			before := reviewTestSnapshot(t, repo)
			checkScope(before, "Uncommitted changes")
			second := commit("package main // second revision\n")
			after := reviewTestSnapshot(t, repo)
			checkScope(after, "Uncommitted changes")
			if before.id == after.id {
				t.Fatal("different submodule commits kept the same snapshot")
			}
			a := newApp(repo)
			a.review = reviewState{sourceID: before.id, result: &reviewResult{Verdict: "passed"}}
			a.updateReviewSource(after, "")
			if !a.review.stale {
				t.Fatal("kept an outdated passing verdict")
			}
			run := func(context.Context, string, reviewSnapshot) (reviewResult, error) {
				pushTestGit(t, sub, "checkout", "--detach", first)
				return reviewResult{Verdict: "passed", Summary: "No defects", Findings: []reviewFinding{}}, nil
			}
			if _, err := reviewRepository(context.Background(), repo, after, run); !errors.Is(err, errReviewChanged) {
				t.Fatalf("accepted a verdict after switching submodule commits: %v", err)
			}
			pushTestGit(t, sub, "checkout", "--detach", second)
			pushTestGit(t, repo, "add", "vendor")
			checkScope(reviewTestSnapshot(t, repo), "Uncommitted changes")
			pushTestGit(t, repo, "commit", "-m", "Update the submodule")
			checkScope(reviewTestSnapshot(t, repo), "Unpushed commits")
		})
	}
}
