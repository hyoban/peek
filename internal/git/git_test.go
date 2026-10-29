package git

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"slices"
	"strings"
	"testing"
	"time"
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

// The branch's page is on the remote it tracks, none where it tracks none,
// and the default branch's is the repository's.
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
	testutil.Git(t, repo, "update-ref", "refs/remotes/fork/develop", "HEAD")
	testutil.Git(t, repo, "symbolic-ref", "refs/remotes/fork/HEAD", "refs/remotes/fork/develop")
	testutil.Git(t, repo, "config", "branch.feat/a#b.merge", "refs/heads/develop")
	if got, want := BranchGitHubURL(repo), "https://github.com/someone/peek"; got != want {
		t.Errorf("default branch's page %q, want %q", got, want)
	}
	testutil.Git(t, repo, "symbolic-ref", "--delete", "refs/remotes/fork/HEAD")
	testutil.Git(t, repo, "config", "branch.feat/a#b.merge", "refs/heads/main")
	testutil.Git(t, repo, "update-ref", "refs/remotes/fork/main", "HEAD")
	if got, want := BranchGitHubURL(repo), "https://github.com/someone/peek"; got != want {
		t.Errorf("main's page without the remote's HEAD %q, want %q", got, want)
	}
	// gh pr checkout names a fork's remote by its URL.
	testutil.Git(t, repo, "config", "branch.feat/a#b.remote", "git@github.com:other/peek.git")
	testutil.Git(t, repo, "config", "branch.feat/a#b.merge", "refs/heads/fix/it")
	if got, want := BranchGitHubURL(repo), "https://github.com/other/peek/tree/fix/it"; got != want {
		t.Errorf("fork's branch's page %q, want %q", got, want)
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

// Untracked files more than a batch of them count all, each its own.
func TestLineCountsBatched(t *testing.T) {
	repo := testutil.Repo(t)
	const n = 2500
	for i := range n {
		os.WriteFile(filepath.Join(repo, fmt.Sprintf("f%d.txt", i)), []byte(strings.Repeat("x\n", i%5+1)), 0o644)
	}
	changes, err := LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, ch := range changes {
		var i int
		if _, err := fmt.Sscanf(ch.Path, "f%d.txt", &i); err != nil {
			continue
		}
		seen++
		if ch.Added != i%5+1 {
			t.Errorf("%s added %d, want %d", ch.Path, ch.Added, i%5+1)
		}
	}
	if seen != n {
		t.Errorf("%d files listed, want %d", seen, n)
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

func TestLoadCommitsTags(t *testing.T) {
	dir := testutil.Repo(t)
	testutil.Git(t, dir, "tag", "v1")
	testutil.Git(t, dir, "tag", "-a", "v1.0.0", "-m", "Release")
	testutil.Git(t, dir, "branch", "topic") // a branch is no tag
	testutil.Git(t, dir, "commit", "-q", "--allow-empty", "-m", "Untagged")
	commits, err := LoadCommits(dir, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(commits[0].Tags) != 0 {
		t.Errorf("untagged commit's tags %q", commits[0].Tags)
	}
	got := slices.Sorted(slices.Values(commits[1].Tags))
	if want := []string{"v1", "v1.0.0"}; !slices.Equal(got, want) {
		t.Errorf("tags %q, want %q", got, want)
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

// Untracked files keep the lines counted while they stay as they were,
// and are counted again once written, to the same size even.
func TestUntrackedCountsKept(t *testing.T) {
	repo := testutil.Repo(t)
	p := filepath.Join(repo, "docs", "new.md")
	added := func() int {
		changes, err := LoadWorktreeChanges(repo)
		if err != nil {
			t.Fatal(err)
		}
		for _, ch := range changes {
			if ch.Path == "docs/new.md" {
				return ch.Added
			}
		}
		t.Fatal("docs/new.md not listed")
		return 0
	}
	os.WriteFile(p, []byte("a\nb\n"), 0o644)
	old := time.Now().Add(-time.Hour)
	os.Chtimes(p, old, old)
	if n := added(); n != 2 {
		t.Fatalf("%d lines added, want 2", n)
	}
	if n := added(); n != 2 {
		t.Fatalf("%d lines added counted again, want 2", n)
	}
	os.WriteFile(p, []byte("abc\n"), 0o644) // as large, a line
	if n := added(); n != 1 {
		t.Errorf("%d lines added once rewritten, want 1", n)
	}
}

// Untracked files counted here count as git counts them: lines with and
// without a last newline, empty and binary files, links, files git takes
// for big, and those attributes apply to, which git counts.
func TestCountDirectAsGit(t *testing.T) {
	repo := testutil.Repo(t)
	files := map[string]string{
		"lines.txt":    "a\nb\nc\n",
		"no-end.txt":   "a\nb",
		"empty.txt":    "",
		"crlf.txt":     "a\r\nb\r\n",
		"cr.txt":       "a\rb\r",
		"binary.dat":   "a\x00b\n",
		"late-nul.dat": strings.Repeat("x\n", 4050) + "\x00", // text: past 8000 bytes
		"big.txt":      strings.Repeat("line\n", 2000),       // over the threshold
		"unicode.txt":  "héllo\n世界\n",
		"attr.bin":     "a\nb\n",
	}
	for p, text := range files {
		os.WriteFile(filepath.Join(repo, p), []byte(text), 0o644)
	}
	os.Symlink("lines.txt", filepath.Join(repo, "link"))
	os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.bin binary\n"), 0o644)
	testutil.Git(t, repo, "config", "core.bigFileThreshold", "9000")
	paths := []string{"link"}
	for p := range files {
		paths = append(paths, p)
	}
	slices.Sort(paths)
	want, err := untrackedBatch(repo, paths)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]int{}
	rest := countDirect(repo, paths, got)
	if !slices.Equal(rest, []string{"attr.bin"}) {
		t.Errorf("left for git %q, want attr.bin alone", rest)
	}
	for _, p := range paths {
		if _, ok := got[p]; ok && got[p] != want[p] {
			t.Errorf("%s: counted %v, git counts %v", p, got[p], want[p])
		}
	}
	if want["late-nul.dat"] != [2]int{4051, 0} || want["big.txt"] != [2]int{} {
		t.Errorf("git counts %v and %v: the cases are not what they test", want["late-nul.dat"], want["big.txt"])
	}
}

// The checks list the failing first, then those running, passing and
// skipped, a check run again by its latest run alone.
func TestReadChecks(t *testing.T) {
	at := func(m int) time.Time { return time.Date(2026, 1, 1, 0, m, 0, 0, time.UTC) }
	checks := readChecks([]ghCheck{
		{Type: "CheckRun", Name: "build", Workflow: "CI", Status: "COMPLETED", Conclusion: "SUCCESS", StartedAt: at(1)},
		{Type: "CheckRun", Name: "test", Workflow: "CI", Status: "COMPLETED", Conclusion: "FAILURE", StartedAt: at(1)},
		{Type: "CheckRun", Name: "test", Workflow: "CI", Status: "IN_PROGRESS", StartedAt: at(2)},
		{Type: "CheckRun", Name: "label", Workflow: "Triage", Status: "COMPLETED", Conclusion: "SKIPPED"},
		{Type: "CheckRun", Name: "lint", Workflow: "CI", Status: "COMPLETED", Conclusion: "CANCELLED"},
		{Type: "StatusContext", Context: "deploy", State: "PENDING"},
	})
	var got []string
	for _, ch := range checks {
		got = append(got, fmt.Sprint(ch.Name, ch.State))
	}
	want := []string{"lint0", "deploy1", "test1", "build2", "label3"}
	if !slices.Equal(got, want) {
		t.Fatalf("checks %v, want %v", got, want)
	}
	pr := &BranchPull{Checks: checks}
	if s, ok := pr.ChecksState(); !ok || s != CheckFailed {
		t.Fatalf("state %v, %v", s, ok)
	}
}
