// Package testutil holds what the tests of several packages share: the
// repositories they run over, and stand-ins for Codex and gh.
package testutil

import (
	"bytes"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Git runs git in the repository, failing the test where it fails, and
// returns what it printed, trimmed.
func Git(t *testing.T, repo string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

// Repo makes a repository of one commit, then changes one file,
// deletes another and adds one git does not track.
func Repo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		Git(t, dir, append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.com"}, args...)...)
	}
	write := func(name, text string) {
		p := filepath.Join(dir, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q", "-b", "main")
	write("main.go", "package main\n")
	write("old.txt", "gone soon\n")
	run("add", ".")
	run("commit", "-q", "-m", "Initial commit")
	write("main.go", "package main\n\nfunc main() {}\n")
	os.Remove(filepath.Join(dir, "old.txt"))
	write("docs/new.md", "# New\n")
	return dir
}

// RemoteRepo makes Repo's repository with a bare remote, origin, that
// has its commit; it returns both.
func RemoteRepo(t *testing.T) (string, string) {
	t.Helper()
	repo, remote := Repo(t), t.TempDir()
	for key, value := range map[string]string{"user.name": "Ada", "user.email": "ada@example.com", "commit.gpgsign": "false", "core.hooksPath": filepath.Join(repo, ".git", "hooks")} {
		Git(t, repo, "config", key, value)
	}
	Git(t, remote, "init", "--bare", "-b", "main")
	Git(t, remote, "config", "core.hooksPath", filepath.Join(remote, "hooks"))
	Git(t, repo, "remote", "add", "origin", remote)
	Git(t, repo, "push", "-u", "origin", "main")
	return repo, remote
}

// FakeCodex puts a codex on the PATH that writes a commit message.
func FakeCodex(t *testing.T) {
	t.Helper()
	FakeReviewCodex(t, `{"changes":["Update the files"],"subject":"Update the files"}`)
}

// FakeGH puts a gh on the PATH that lists one pull request, takes it for
// the branch's own, its merging blocked by a review, a required check and
// a conversation, and checks it out as a branch of its name, recording
// what it was asked; its default repository is github's, the page of one.
func FakeGH(t *testing.T, github string) string {
	t.Helper()
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	script := `#!/bin/sh
echo "$@" >> "` + log + `"
case "$1 $2" in
  "repo view") echo "` + github + `" ;;
  "pr list") printf '%s' '[{"number":7,"title":"Add the switcher","headRefName":"switcher","isDraft":false,"author":{"login":"ada"}}]' ;;
  "pr view") printf '%s' '{"number":7,"title":"Add the switcher","body":"Lists the branches.","url":"` + github + `/pull/7","state":"OPEN","isDraft":false,"author":{"login":"ada"},"reviewDecision":"REVIEW_REQUIRED","baseRefName":"main","mergeable":"MERGEABLE","mergeStateStatus":"BLOCKED","statusCheckRollup":[{"__typename":"CheckRun","name":"test","workflowName":"CI","status":"COMPLETED","conclusion":"FAILURE","detailsUrl":"` + github + `/actions/runs/1"},{"__typename":"CheckRun","name":"lint","workflowName":"CI","status":"IN_PROGRESS","conclusion":""},{"__typename":"StatusContext","context":"deploy","state":"SUCCESS","targetUrl":"https://example.com"}]}' ;;
  "pr checkout") git switch -q -c "pr-$3" ;;
  "api graphql") printf '%s' '{"data":{"repository":{"pullRequest":{"reviewThreads":{"nodes":[{"isResolved":false},{"isResolved":true}]},"commits":{"nodes":[{"commit":{"statusCheckRollup":{"contexts":{"nodes":[{"__typename":"CheckRun","name":"test","isRequired":true,"checkSuite":{"workflowRun":{"workflow":{"name":"CI"}}}},{"__typename":"CheckRun","name":"lint","isRequired":false,"checkSuite":{"workflowRun":{"workflow":{"name":"CI"}}}}]}}}}]}}}}}' ;;
  *) exit 1 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "gh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return log
}

// RemoteWork pushes a commit to the remote from another clone, as a
// teammate would.
func RemoteWork(t *testing.T, remote, file string) {
	t.Helper()
	other := t.TempDir()
	Git(t, other, "clone", "-q", remote, ".")
	if err := os.WriteFile(filepath.Join(other, file), []byte(file+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	Git(t, other, "add", "--all")
	Git(t, other, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Remote work")
	Git(t, other, "push", "-q", "origin", "main")
}

func PNG(t *testing.T) []byte {
	t.Helper()
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 2))); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

const PassingReview = `{"status":"complete","summary":"No actionable defects found.","findings":[]}`

const FailingReview = `{"status":"complete","summary":"Empty inputs now crash.","findings":[{"priority":2,"title":"Handle empty inputs","body":"Restore the guard so an empty list does not divide by zero.","path":"calc.py","line":2}]}`

func FakeReviewCodex(t *testing.T, response string) {
	t.Helper()
	dir := t.TempDir()
	file := filepath.Join(dir, "response.json")
	if err := os.WriteFile(file, []byte(response), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PEEK_TEST_REVIEW_RESPONSE", file)
	script := `#!/bin/sh
if [ -n "$PEEK_TEST_CODEX_ARGS" ]; then printf '%s\n' "$*" >> "$PEEK_TEST_CODEX_ARGS"; fi
# The app server answers its requests a line each, streaming the response's
# answer, the prompt kept as exec keeps it.
if [ "$1" = app-server ]; then
  thread="${PEEK_TEST_CODEX_THREAD:-thread-test}"
  while IFS= read -r line; do
    id=$(printf '%s\n' "$line" | sed -n 's/.*"id":\([0-9][0-9]*\).*/\1/p')
    case "$line" in
      *'"method":"initialize"'*) printf '{"id":%s,"result":{}}\n' "$id" ;;
      *'"method":"thread/start"'*)
        case "$line" in *'"sandbox":"read-only"'*) ;; *) exit 2 ;; esac
        printf '{"id":%s,"result":{"thread":{"id":"%s"}}}\n' "$id" "$thread" ;;
      *'"method":"thread/resume"'*)
        if [ -n "$PEEK_TEST_CODEX_ARGS" ]; then
          printf 'app-server resume %s\n' "$(printf '%s\n' "$line" | sed -n 's/.*"threadId":"\([^"]*\)".*/\1/p')" >> "$PEEK_TEST_CODEX_ARGS"
        fi
        printf '{"id":%s,"result":{"thread":{"id":"%s"}}}\n' "$id" "$thread" ;;
      *'"method":"turn/start"'*)
        if [ -n "$PEEK_TEST_REVIEW_PROMPT" ]; then
          printf '%b' "$(printf '%s\n' "$line" | sed -n 's/.*"text":"\(.*\)","type":"text".*/\1/p')" > "$PEEK_TEST_REVIEW_PROMPT"
        fi
        printf '{"id":%s,"result":{}}\n' "$id"
        answer=$(sed -n 's/.*"answer":"\(.*\)"}.*/\1/p' "$PEEK_TEST_REVIEW_RESPONSE")
        printf '{"method":"item/started","params":{"item":{"type":"agentMessage"}}}\n'
        printf '{"method":"item/agentMessage/delta","params":{"delta":"%s"}}\n' "$answer"
        printf '{"method":"turn/completed","params":{"turn":{"status":"completed"}}}\n' ;;
    esac
  done
  exit 0
fi
while [ "$#" -gt 0 ]; do
  case "$1" in
    --output-last-message) shift; output="$1" ;;
    --sandbox) shift; test "$1" = read-only || exit 2 ;;
    --json) json=1 ;;
    --cd) shift; if [ -n "$PEEK_TEST_CODEX_CWD" ]; then printf '%s' "$1" > "$PEEK_TEST_CODEX_CWD"; fi ;;
  esac
  shift
done
if [ -n "$PEEK_TEST_REVIEW_PROMPT" ]; then
  cat > "$PEEK_TEST_REVIEW_PROMPT"
else
  cat >/dev/null
fi
if [ -n "$PEEK_TEST_CODEX_THREAD" ] && [ "$json" = 1 ]; then
  printf '{"type":"thread.started","thread_id":"%s"}\n' "$PEEK_TEST_CODEX_THREAD"
fi
if [ -n "$PEEK_TEST_CODEX_EVENTS" ]; then
  test "$json" = 1 || exit 3
  cat "$PEEK_TEST_CODEX_EVENTS"
  attempts=0
  while [ ! -f "$PEEK_TEST_CODEX_ACK" ] && [ "$attempts" -lt 100 ]; do
    sleep 0.01
    attempts=$((attempts + 1))
  done
  test -f "$PEEK_TEST_CODEX_ACK" || exit 4
fi
cp "$PEEK_TEST_REVIEW_RESPONSE" "$output"
`
	if err := os.WriteFile(filepath.Join(dir, "codex"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
}
