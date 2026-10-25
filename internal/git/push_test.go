package git

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
)

func testMessage(context.Context, string, Snapshot) (string, error) {
	return "Update the files", nil
}

func ignoreProgress(string) {}

func TestPushAllChanges(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := 0
	initial := testutil.Git(t, repo, "rev-parse", "HEAD")
	generate := func(ctx context.Context, directory string, snapshot Snapshot) (string, error) {
		called++
		if directory != repo || snapshot.Head != initial || snapshot.Tree != testutil.Git(t, repo, "write-tree") {
			t.Fatal("generator received the wrong repository or snapshot")
		}
		diff := testutil.Git(t, directory, "diff", snapshot.Head, snapshot.Tree)
		history := testutil.Git(t, directory, "log", "-8", "--format=%s", snapshot.Head)
		for _, name := range []string{"main.go", "old.txt", "docs/new.md"} {
			if !strings.Contains(diff, name) {
				t.Errorf("diff missing %s", name)
			}
		}
		if strings.Contains(diff, "private") || !strings.Contains(history, "Initial commit") {
			t.Errorf("unexpected prompt: %s\n%s", history, diff)
		}
		return testMessage(ctx, directory, snapshot)
	}
	status, err := Push(context.Background(), repo, generate, ignoreProgress)
	if err != nil || status != "Shipped to origin/main" {
		t.Fatalf("%q, %v", status, err)
	}
	if got := testutil.Git(t, repo, "status", "--porcelain"); got != "" {
		t.Errorf("dirty after push: %s", got)
	}
	if got := testutil.Git(t, remote, "log", "-1", "--format=%s", "main"); got != "Update the files" {
		t.Fatal(got)
	}
	if got := testutil.Git(t, remote, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(got, "docs/new.md") || strings.Contains(got, "old.txt") || strings.Contains(got, "ignored.txt") {
		t.Fatal(got)
	}
	if _, err := Push(context.Background(), repo, generate, ignoreProgress); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("generated %d times, want once", called)
	}
}

func TestPushProtectedBranch(t *testing.T) {
	repo, remote := testutil.RemoteRepo(t)
	initial := testutil.Git(t, remote, "rev-parse", "main")
	hook := "#!/bin/sh\nwhile read old new ref; do\n if [ \"$ref\" = refs/heads/main ]; then\n echo 'GH006: Protected branch update failed' >&2\n exit 1\n fi\ndone\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	status, err := Push(context.Background(), repo, testMessage, ignoreProgress)
	if err != nil {
		t.Fatal(err)
	}
	branch := testutil.Git(t, repo, "branch", "--show-current")
	if !strings.HasPrefix(branch, "peek/push-") || !strings.Contains(status, branch) {
		t.Fatalf("%q, %q", branch, status)
	}
	if testutil.Git(t, remote, "rev-parse", "main") != initial {
		t.Fatal("changed protected branch")
	}
	if testutil.Git(t, remote, "rev-parse", branch) != testutil.Git(t, repo, "rev-parse", "HEAD") {
		t.Fatal("new branch was not pushed")
	}
	if got := testutil.Git(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/"+branch {
		t.Fatal(got)
	}
}

func TestPushFailuresPreserveWork(t *testing.T) {
	for _, scenario := range []string{"generation", "index changed", "remote rejected", "non fast forward", "no remote", "merge", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			repo, remote := testutil.RemoteRepo(t)
			initial := testutil.Git(t, repo, "rev-parse", "HEAD")
			generate := MessageGenerator(testMessage)
			ctx := context.Background()
			switch scenario {
			case "generation":
				generate = func(context.Context, string, Snapshot) (string, error) { return "", errors.New("not logged in") }
			case "index changed":
				generate = func(context.Context, string, Snapshot) (string, error) {
					testutil.Git(t, repo, "reset", "HEAD", "main.go")
					return "Wrong snapshot", nil
				}
			case "remote rejected":
				if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\necho 'secret detected' >&2\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "non fast forward":
				clone := t.TempDir()
				testutil.Git(t, clone, "clone", remote, ".")
				testutil.Git(t, clone, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Remote advanced")
				testutil.Git(t, clone, "push")
			case "no remote":
				testutil.Git(t, repo, "remote", "remove", "origin")
			case "merge":
				if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte(initial), 0o644); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if status, err := Push(ctx, repo, generate, ignoreProgress); err == nil {
				t.Fatalf("unexpected success: %s", status)
			}
			if got := testutil.Git(t, repo, "branch", "--show-current"); got != "main" {
				t.Fatalf("switched to %s", got)
			}
			committed := scenario == "remote rejected" || scenario == "non fast forward"
			if unchanged := testutil.Git(t, repo, "rev-parse", "HEAD") == initial; unchanged == committed {
				t.Fatalf("commit state wrong: committed=%v unchanged=%v", committed, unchanged)
			}
			if _, err := os.Stat(filepath.Join(repo, "docs/new.md")); err != nil {
				t.Fatal("lost new file", err)
			}
			if scenario == "remote rejected" {
				if err := os.Remove(filepath.Join(remote, "hooks", "pre-receive")); err != nil {
					t.Fatal(err)
				}
				generate = func(context.Context, string, Snapshot) (string, error) {
					t.Fatal("retry must reuse commit")
					return "", nil
				}
				if _, err := Push(context.Background(), repo, generate, ignoreProgress); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPushDetachedAndFirstCommit(t *testing.T) {
	for _, unborn := range []bool{false, true} {
		repo, remote := testutil.RemoteRepo(t)
		if unborn {
			testutil.Git(t, repo, "switch", "--orphan", "new-history", "--discard-changes")
			if err := os.WriteFile(filepath.Join(repo, "first.txt"), []byte("first\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			testutil.Git(t, repo, "checkout", "--detach")
		}
		if _, err := Push(context.Background(), repo, testMessage, ignoreProgress); err != nil {
			t.Fatal(err)
		}
		branch := testutil.Git(t, repo, "branch", "--show-current")
		if testutil.Git(t, remote, "rev-parse", branch) != testutil.Git(t, repo, "rev-parse", "HEAD") {
			t.Fatal("missing remote commit")
		}
	}
}

func TestPushRemoteSelection(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	fork := t.TempDir()
	testutil.Git(t, fork, "init", "--bare")
	testutil.Git(t, repo, "remote", "add", "fork", fork)
	testutil.Git(t, repo, "config", "branch.main.pushRemote", "fork")
	if _, err := Push(context.Background(), repo, testMessage, ignoreProgress); err != nil {
		t.Fatal(err)
	}
	if testutil.Git(t, fork, "rev-parse", "main") != testutil.Git(t, repo, "rev-parse", "HEAD") {
		t.Fatal("did not use pushRemote")
	}
	if got := testutil.Git(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/main" {
		t.Fatalf("changed pull upstream to %s", got)
	}
}

func TestPushAvailability(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-m", "Update the files")
	check := func(want bool) {
		t.Helper()
		if got := HasUnpushedCommits(repo); got != want {
			t.Fatalf("HasUnpushedCommits = %v, want %v", got, want)
		}
	}
	check(true) // clean working tree, with an unpushed commit
	testutil.Git(t, repo, "push")
	check(false)

	testutil.Git(t, repo, "reset", "--hard", "HEAD~1")
	check(false) // behind alone is not something to push
	testutil.Git(t, repo, "switch", "-c", "feature")
	check(true) // a branch that has not been published
	testutil.Git(t, repo, "push", "-u", "origin", "feature")
	check(false)

	fork := t.TempDir()
	testutil.Git(t, fork, "init", "--bare")
	testutil.Git(t, repo, "remote", "add", "fork", fork)
	testutil.Git(t, repo, "config", "branch.feature.pushRemote", "fork")
	check(true) // origin is up to date, but the actual push destination is not
	testutil.Git(t, repo, "push", "fork", "feature")
	check(false)

	testutil.Git(t, repo, "checkout", "--detach")
	check(false)
	testutil.Git(t, repo, "commit", "--allow-empty", "-m", "A detached change")
	check(true)
}
