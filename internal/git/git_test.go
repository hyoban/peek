package git

import (
	"maps"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"slices"
	"testing"
)

func TestGitHubPage(t *testing.T) {
	for url, want := range map[string]string{
		"git@github.com:hyoban/peek.git":         "https://github.com/hyoban/peek",
		"git@github.com:hyoban/peek":             "https://github.com/hyoban/peek",
		"https://github.com/hyoban/peek.git":     "https://github.com/hyoban/peek",
		"https://github.com/hyoban/peek/":        "https://github.com/hyoban/peek",
		"ssh://git@github.com/hyoban/peek.git":   "https://github.com/hyoban/peek",
		"git@gitlab.com:hyoban/peek.git":         "",
		"https://github.com/hyoban":              "",
		"https://github.com.evil.com/a/b":        "",
		"https://example.com/github.com/a/b.git": "",
	} {
		if got := githubPage(url); got != want {
			t.Errorf("githubPage(%q) = %q, want %q", url, got, want)
		}
	}
}

// The branch's page is on the remote it tracks, none where it tracks none.
func TestBranchGitHubURL(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.Git(t, repo, "remote", "add", "origin", "git@github.com:hyoban/peek.git")
	testutil.Git(t, repo, "remote", "add", "fork", "https://github.com/someone/peek.git")
	testutil.Git(t, repo, "checkout", "-q", "-b", "feat/a#b")
	if got := BranchGitHubURL(repo); got != "" {
		t.Errorf("untracked branch's page %q, want none", got)
	}
	testutil.Git(t, repo, "config", "branch.feat/a#b.remote", "fork")
	testutil.Git(t, repo, "config", "branch.feat/a#b.merge", "refs/heads/feat/a#b")
	if got, want := BranchGitHubURL(repo), "https://github.com/someone/peek/tree/feat/a%23b"; got != want {
		t.Errorf("tracked branch's page %q, want %q", got, want)
	}
	testutil.Git(t, repo, "checkout", "-q", "--detach")
	if got := BranchGitHubURL(repo); got != "" {
		t.Errorf("detached HEAD's page %q, want none", got)
	}
}

// Untracked files named as pathspecs' magic, or outside a sparse
// checkout, count as others do.
func TestLineCountsLiteralAndSparse(t *testing.T) {
	repo := testutil.Repo(t)
	if _, err := git(repo, "sparse-checkout", "set", "--no-cone", "/main.go", "/old.txt"); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(repo, ":notes.txt"), []byte("a\nb\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "*"), []byte("a\n"), 0o644)
	changes, err := LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, ch := range changes {
		got[ch.Path] = ch.Added
	}
	want := map[string]int{"main.go": 2, "old.txt": 0, "docs/new.md": 1, ":notes.txt": 2, "*": 1}
	if !maps.Equal(got, want) {
		t.Errorf("added %v, want %v", got, want)
	}
}

// Counting the untracked files runs none of the repository's hooks and
// writes nothing in it, a split index's shared files neither.
func TestLineCountsIsolated(t *testing.T) {
	repo := testutil.Repo(t)
	hooks := filepath.Join(repo, ".git", "hooks")
	os.MkdirAll(hooks, 0o755)
	marker := filepath.Join(t.TempDir(), "ran")
	os.WriteFile(filepath.Join(hooks, "post-index-change"), []byte("#!/bin/sh\ntouch "+marker+"\n"), 0o755)
	if _, err := git(repo, "config", "core.splitIndex", "true"); err != nil {
		t.Fatal(err)
	}
	before, _ := filepath.Glob(filepath.Join(repo, ".git", "sharedindex.*"))
	changes, err := LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	if i := slices.IndexFunc(changes, func(ch Change) bool { return ch.Path == "docs/new.md" }); i < 0 || changes[i].Added != 1 {
		t.Errorf("docs/new.md not counted: %+v", changes)
	}
	if _, err := os.Stat(marker); err == nil {
		t.Error("post-index-change ran")
	}
	if after, _ := filepath.Glob(filepath.Join(repo, ".git", "sharedindex.*")); len(after) != len(before) {
		t.Errorf("shared index files %v, were %v", after, before)
	}
}

func TestParseNumstat(t *testing.T) {
	out := "3\t1\tmain.go\x00-\t-\ticon.png\x002\t0\t\x00old.go\x00new.go\x00"
	got := parseNumstat(out)
	want := map[string][2]int{"main.go": {3, 1}, "icon.png": {0, 0}, "new.go": {2, 0}}
	if !maps.Equal(got, want) {
		t.Errorf("parseNumstat = %v, want %v", got, want)
	}
}

func TestLoadCommitsBody(t *testing.T) {
	dir := testutil.Repo(t)
	if _, err := git(dir, "-c", "user.name=Ada", "-c", "user.email=ada@example.com",
		"commit", "-q", "-a", "-m", "Second commit", "-m", "Why it changed.\n\nMore lines."); err != nil {
		t.Fatal(err)
	}
	commits, err := LoadCommits(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits) != 2 {
		t.Fatalf("got %d commits, want 2", len(commits))
	}
	if got := commits[0]; got.Subject != "Second commit" || got.Body != "Why it changed.\n\nMore lines." {
		t.Errorf("got %q / %q", got.Subject, got.Body)
	}
	if got := commits[1]; got.Subject != "Initial commit" || got.Body != "" || got.Author != "Ada" {
		t.Errorf("got %+v", got)
	}
}

func TestLoadCommitsCoAuthors(t *testing.T) {
	dir := testutil.Repo(t)
	msg := "Pair on it\n\nWhy it changed.\n\nCo-authored-by: Bob Lee <bob@example.com>\nco-authored-by: Ada <ADA@example.com>\nCo-authored-by: Cy <cy@example.com>\nCo-authored-by: Bob Again <bob@example.com>\n"
	if _, err := gitWith(dir, nil, msg, "-c", "user.name=Ada", "-c", "user.email=ada@example.com",
		"commit", "-q", "-a", "-F", "-"); err != nil {
		t.Fatal(err)
	}
	commits, err := LoadCommits(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := commits[0]
	want := []Person{{"Bob Lee", "bob@example.com"}, {"Cy", "cy@example.com"}}
	if !slices.Equal(got.CoAuthors, want) {
		t.Errorf("co-authors %+v, want %+v", got.CoAuthors, want)
	}
	if got.Body != "Why it changed." {
		t.Errorf("body %q", got.Body)
	}
	if len(commits[1].CoAuthors) != 0 {
		t.Errorf("first commit %+v", commits[1])
	}
}

func TestCoAuthorLinesInProseStay(t *testing.T) {
	dir := testutil.Repo(t)
	msg := "Squash\n\n* First\n\nCo-authored-by: Dee <dee@example.com>\n\n* Second, more prose\n\nCo-authored-by: Bob <bob@example.com>\n"
	if _, err := gitWith(dir, nil, msg, "-c", "user.name=Ada", "-c", "user.email=ada@example.com",
		"commit", "-q", "-a", "-F", "-"); err != nil {
		t.Fatal(err)
	}
	commits, err := LoadCommits(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	got := commits[0]
	if !slices.Equal(got.CoAuthors, []Person{{"Bob", "bob@example.com"}}) {
		t.Errorf("co-authors %+v", got.CoAuthors)
	}
	if want := "* First\n\nCo-authored-by: Dee <dee@example.com>\n\n* Second, more prose"; got.Body != want {
		t.Errorf("body %q, want %q", got.Body, want)
	}
}

func TestWithoutCoAuthors(t *testing.T) {
	for _, tc := range []struct{ body, want string }{
		{"Co-authored-by: Bob <bob@example.com>", ""},
		{"Why.\n\nSigned-off-by: Ada <ada@example.com>\nCo-authored-by: Bob <BOB@example.com>", "Why.\n\nSigned-off-by: Ada <ada@example.com>"},
		{"Why.\n\nCo-authored-by: Someone Else <else@example.com>", "Why.\n\nCo-authored-by: Someone Else <else@example.com>"},
	} {
		if got := withoutCoAuthors(tc.body, []string{"bob@example.com"}); got != tc.want {
			t.Errorf("withoutCoAuthors(%q) = %q, want %q", tc.body, got, tc.want)
		}
	}
}
