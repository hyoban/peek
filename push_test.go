package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func pushTestGit(t *testing.T, repo string, args ...string) string {
	t.Helper()
	out, err := pushGit(context.Background(), repo, args...)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func pushTestRepo(t *testing.T) (string, string) {
	t.Helper()
	repo, remote := testRepo(t), t.TempDir()
	for key, value := range map[string]string{"user.name": "Ada", "user.email": "ada@example.com", "commit.gpgsign": "false", "core.hooksPath": filepath.Join(repo, ".git", "hooks")} {
		pushTestGit(t, repo, "config", key, value)
	}
	pushTestGit(t, remote, "init", "--bare", "-b", "main")
	pushTestGit(t, remote, "config", "core.hooksPath", filepath.Join(remote, "hooks"))
	pushTestGit(t, repo, "remote", "add", "origin", remote)
	pushTestGit(t, repo, "push", "-u", "origin", "main")
	return repo, remote
}

func testMessage(context.Context, string, commitSnapshot) (string, error) {
	return "Update the files", nil
}
func ignoreProgress(string) {}

func TestPushAllChanges(t *testing.T) {
	repo, remote := pushTestRepo(t)
	if err := os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("ignored.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "ignored.txt"), []byte("private"), 0o644); err != nil {
		t.Fatal(err)
	}
	called := 0
	initial := pushTestGit(t, repo, "rev-parse", "HEAD")
	generate := func(ctx context.Context, directory string, snapshot commitSnapshot) (string, error) {
		called++
		if directory != repo || snapshot.head != initial || snapshot.tree != pushTestGit(t, repo, "write-tree") {
			t.Fatal("generator received the wrong repository or snapshot")
		}
		diff := pushTestGit(t, directory, "diff", snapshot.head, snapshot.tree)
		history := pushTestGit(t, directory, "log", "-8", "--format=%s", snapshot.head)
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
	status, err := pushRepository(context.Background(), repo, generate, ignoreProgress)
	if err != nil || status != "Shipped to origin/main" {
		t.Fatalf("%q, %v", status, err)
	}
	if got := pushTestGit(t, repo, "status", "--porcelain"); got != "" {
		t.Errorf("dirty after push: %s", got)
	}
	if got := pushTestGit(t, remote, "log", "-1", "--format=%s", "main"); got != "Update the files" {
		t.Fatal(got)
	}
	if got := pushTestGit(t, remote, "ls-tree", "-r", "--name-only", "main"); !strings.Contains(got, "docs/new.md") || strings.Contains(got, "old.txt") || strings.Contains(got, "ignored.txt") {
		t.Fatal(got)
	}
	if _, err := pushRepository(context.Background(), repo, generate, ignoreProgress); err != nil {
		t.Fatal(err)
	}
	if called != 1 {
		t.Fatalf("generated %d times, want once", called)
	}
}

func TestPushProtectedBranch(t *testing.T) {
	repo, remote := pushTestRepo(t)
	initial := pushTestGit(t, remote, "rev-parse", "main")
	hook := "#!/bin/sh\nwhile read old new ref; do\n if [ \"$ref\" = refs/heads/main ]; then\n echo 'GH006: Protected branch update failed' >&2\n exit 1\n fi\ndone\n"
	if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	status, err := pushRepository(context.Background(), repo, testMessage, ignoreProgress)
	if err != nil {
		t.Fatal(err)
	}
	branch := pushTestGit(t, repo, "branch", "--show-current")
	if !strings.HasPrefix(branch, "peek/push-") || !strings.Contains(status, branch) {
		t.Fatalf("%q, %q", branch, status)
	}
	if pushTestGit(t, remote, "rev-parse", "main") != initial {
		t.Fatal("changed protected branch")
	}
	if pushTestGit(t, remote, "rev-parse", branch) != pushTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatal("new branch was not pushed")
	}
	if got := pushTestGit(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/"+branch {
		t.Fatal(got)
	}
}

func TestPushFailuresPreserveWork(t *testing.T) {
	for _, scenario := range []string{"generation", "index changed", "remote rejected", "non fast forward", "no remote", "merge", "cancelled"} {
		t.Run(scenario, func(t *testing.T) {
			repo, remote := pushTestRepo(t)
			initial := pushTestGit(t, repo, "rev-parse", "HEAD")
			generate := messageGenerator(testMessage)
			ctx := context.Background()
			switch scenario {
			case "generation":
				generate = func(context.Context, string, commitSnapshot) (string, error) { return "", errors.New("not logged in") }
			case "index changed":
				generate = func(context.Context, string, commitSnapshot) (string, error) {
					pushTestGit(t, repo, "reset", "HEAD", "main.go")
					return "Wrong snapshot", nil
				}
			case "remote rejected":
				if err := os.WriteFile(filepath.Join(remote, "hooks", "pre-receive"), []byte("#!/bin/sh\necho 'secret detected' >&2\nexit 1\n"), 0o755); err != nil {
					t.Fatal(err)
				}
			case "non fast forward":
				clone := t.TempDir()
				pushTestGit(t, clone, "clone", remote, ".")
				pushTestGit(t, clone, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "Remote advanced")
				pushTestGit(t, clone, "push")
			case "no remote":
				pushTestGit(t, repo, "remote", "remove", "origin")
			case "merge":
				if err := os.WriteFile(filepath.Join(repo, ".git", "MERGE_HEAD"), []byte(initial), 0o644); err != nil {
					t.Fatal(err)
				}
			case "cancelled":
				var cancel context.CancelFunc
				ctx, cancel = context.WithCancel(ctx)
				cancel()
			}
			if status, err := pushRepository(ctx, repo, generate, ignoreProgress); err == nil {
				t.Fatalf("unexpected success: %s", status)
			}
			if got := pushTestGit(t, repo, "branch", "--show-current"); got != "main" {
				t.Fatalf("switched to %s", got)
			}
			committed := scenario == "remote rejected" || scenario == "non fast forward"
			if unchanged := pushTestGit(t, repo, "rev-parse", "HEAD") == initial; unchanged == committed {
				t.Fatalf("commit state wrong: committed=%v unchanged=%v", committed, unchanged)
			}
			if _, err := os.Stat(filepath.Join(repo, "docs/new.md")); err != nil {
				t.Fatal("lost new file", err)
			}
			if scenario == "remote rejected" {
				if err := os.Remove(filepath.Join(remote, "hooks", "pre-receive")); err != nil {
					t.Fatal(err)
				}
				generate = func(context.Context, string, commitSnapshot) (string, error) {
					t.Fatal("retry must reuse commit")
					return "", nil
				}
				if _, err := pushRepository(context.Background(), repo, generate, ignoreProgress); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestPushDetachedAndFirstCommit(t *testing.T) {
	for _, unborn := range []bool{false, true} {
		repo, remote := pushTestRepo(t)
		if unborn {
			pushTestGit(t, repo, "switch", "--orphan", "new-history", "--discard-changes")
			if err := os.WriteFile(filepath.Join(repo, "first.txt"), []byte("first\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else {
			pushTestGit(t, repo, "checkout", "--detach")
		}
		if _, err := pushRepository(context.Background(), repo, testMessage, ignoreProgress); err != nil {
			t.Fatal(err)
		}
		branch := pushTestGit(t, repo, "branch", "--show-current")
		if pushTestGit(t, remote, "rev-parse", branch) != pushTestGit(t, repo, "rev-parse", "HEAD") {
			t.Fatal("missing remote commit")
		}
	}
}

func TestPushRemoteSelection(t *testing.T) {
	repo, _ := pushTestRepo(t)
	fork := t.TempDir()
	pushTestGit(t, fork, "init", "--bare")
	pushTestGit(t, repo, "remote", "add", "fork", fork)
	pushTestGit(t, repo, "config", "branch.main.pushRemote", "fork")
	if _, err := pushRepository(context.Background(), repo, testMessage, ignoreProgress); err != nil {
		t.Fatal(err)
	}
	if pushTestGit(t, fork, "rev-parse", "main") != pushTestGit(t, repo, "rev-parse", "HEAD") {
		t.Fatal("did not use pushRemote")
	}
	if got := pushTestGit(t, repo, "rev-parse", "--abbrev-ref", "@{upstream}"); got != "origin/main" {
		t.Fatalf("changed pull upstream to %s", got)
	}
}

func TestPushUI(t *testing.T) {
	repo, _ := pushTestRepo(t)
	fakeCodex(t)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Ship"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.push.running || a.push.failed || !tt.HasText("Shipped") {
		t.Fatalf("push state %+v; texts %q", a.push, tt.Texts())
	}
	if a.commits[0].Summary != "No changes" || a.commits[1].Subject != "Update the files" {
		t.Fatalf("stale history: %+v", a.commits)
	}
	tt.Advance(2 * time.Second)
	if !tt.HasText("Shipped") {
		t.Fatal("success label disappeared too soon")
	}
	tt.Advance(time.Second)
	if !tt.HasText("Ship") || tt.HasText("Shipped") {
		t.Fatal("success label did not expire", tt.Texts())
	}
	if a.canPush {
		t.Fatal("Ship still enabled after syncing")
	}
	_ = tt.Click("Ship")
	a.startPush()
	tt.Frame()
	if a.push.status != "" {
		t.Fatal("Ship ran with nothing to push")
	}
	cancelled := false
	a.push = pushState{running: true, status: "Generating commit message with Codex…", cancel: func() { cancelled = true }}
	tt.Frame()
	if err := tt.Click("Generating…"); err != nil {
		t.Fatal(err)
	}
	if !cancelled {
		t.Fatal("not cancelled")
	}
	if err := os.WriteFile(filepath.Join(repo, "retry.txt"), []byte("retry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	tt.Frame()
	if !a.canPush {
		t.Fatal("Ship did not enable for new changes")
	}
	a.push = pushState{failed: true, status: "Test failure"}
	tt.Frame()
	tt.Advance(4 * time.Second)
	if !tt.HasText("Ship failed") {
		t.Fatal(tt.Texts())
	}
	for range 3 {
		tt.Frame()
	}
	if err := tt.Click("Ship failed"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Shipped") {
		t.Fatal("retry did not finish", tt.Texts())
	}
}

func TestPushButtonLayouts(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []commit{{Subject: "Uncommitted Changes"}}
	a.loadedRow, a.commitRow = 0, 0
	for _, width := range []int{640, 1200} {
		tt := ui.NewTester(a.view, width, 800)
		tt.SetTitleBar(ui.TitleBar{Height: titleBarHeight, Left: 68})
		settle(tt)
		if !tt.HasText("Ship") {
			t.Fatalf("no Ship button at width %d", width)
		}
		mode, ok := tt.Find("Changes")
		if !ok {
			t.Fatal("mode switch missing")
		}
		for _, state := range []pushState{
			{running: true, status: "Generating commit message with Codex…"},
			{running: true, status: "Shipping origin/main…"},
			{failed: true, status: "Test failure"},
			{status: "Shipped to origin/main"},
			{},
		} {
			a.push = state
			tt.Frame()
			if got, ok := tt.Find("Changes"); !ok || got != mode {
				t.Fatalf("mode switch moved at width %d for %q: %v → %v", width, state.status, mode, got)
			}
		}
	}
}

func fakeCodex(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	script := `#!/bin/sh
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) shift; output="$1" ;;
    --sandbox) shift; test "$1" = read-only || exit 2 ;;
  esac
  shift
done
cat >/dev/null
printf '%s' '{"message":"Update the files"}' > "$output"
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestPushAvailability(t *testing.T) {
	repo, _ := pushTestRepo(t)
	pushTestGit(t, repo, "add", "--all")
	pushTestGit(t, repo, "commit", "-m", "Update the files")
	check := func(want bool) {
		t.Helper()
		if got := hasUnpushedCommits(repo); got != want {
			t.Fatalf("hasUnpushedCommits = %v, want %v", got, want)
		}
	}
	check(true) // clean working tree, with an unpushed commit
	pushTestGit(t, repo, "push")
	check(false)

	pushTestGit(t, repo, "reset", "--hard", "HEAD~1")
	check(false) // behind alone is not something to push
	pushTestGit(t, repo, "switch", "-c", "feature")
	check(true) // a branch that has not been published
	pushTestGit(t, repo, "push", "-u", "origin", "feature")
	check(false)

	fork := t.TempDir()
	pushTestGit(t, fork, "init", "--bare")
	pushTestGit(t, repo, "remote", "add", "fork", fork)
	pushTestGit(t, repo, "config", "branch.feature.pushRemote", "fork")
	check(true) // origin is up to date, but the actual push destination is not
	pushTestGit(t, repo, "push", "fork", "feature")
	check(false)

	pushTestGit(t, repo, "checkout", "--detach")
	check(false)
	pushTestGit(t, repo, "commit", "--allow-empty", "-m", "A detached change")
	check(true)
}
