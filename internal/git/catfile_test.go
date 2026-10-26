package git

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"peek/internal/testutil"
)

// Files read through the processes kept running read as git show prints
// them: binary ones, those with a newline in their names, the commits made
// since, and many at once; a path that is not a file fails, and one too
// long tells so.
func TestShowFile(t *testing.T) {
	repo := testutil.Repo(t)
	commit := func(msg string) {
		testutil.Git(t, repo, "add", "-A")
		testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", msg)
	}
	files := map[string]string{
		"plain.txt":      "a\nb\n",
		"binary.dat":     "a\x00b\n\x00",
		"empty.txt":      "",
		"no-end.txt":     "last",
		"new\nline.txt":  "named oddly\n",
		"dir/nested.txt": strings.Repeat("x", 100_000),
	}
	for p, text := range files {
		os.MkdirAll(filepath.Join(repo, filepath.Dir(p)), 0o755)
		os.WriteFile(filepath.Join(repo, p), []byte(text), 0o644)
	}
	commit("Add files")
	for p, want := range files {
		got, err := FileText(repo, "HEAD", p)
		if err != nil || got != want {
			t.Errorf("%q: %q, %v, want %q", p, got, err, want)
		}
	}
	if _, err := FileText(repo, "HEAD", "gone.txt"); err == nil {
		t.Error("a file not in the commit read")
	}
	if _, err := FileText(repo, "HEAD", "dir"); err == nil {
		t.Error("a folder read as a file")
	}
	if _, err := FileTextAtMost(repo, "HEAD", "dir/nested.txt", 1000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("a file past the limit: %v", err)
	}
	// What follows a failed read is read as it is.
	if got, _ := FileText(repo, "HEAD", "plain.txt"); got != files["plain.txt"] {
		t.Errorf("after the failures, %q", got)
	}
	os.WriteFile(filepath.Join(repo, "plain.txt"), []byte("changed\n"), 0o644)
	commit("Change")
	if got, _ := FileText(repo, "HEAD", "plain.txt"); got != "changed\n" {
		t.Errorf("after a commit, HEAD's file reads %q", got)
	}
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			p := []string{"plain.txt", "binary.dat", "dir/nested.txt"}[i%3]
			want := files[p]
			if p == "plain.txt" {
				want = "changed\n"
			}
			if got, err := FileText(repo, "HEAD", p); err != nil || got != want {
				t.Errorf("read %d of %s at once: %v", i, p, err)
			}
		})
	}
	wg.Wait()
}

// A process that went away is left for another, the read made anew.
func TestShowFileProcessGone(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.Git(t, repo, "add", "-A")
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Add")
	if _, err := FileText(repo, "HEAD", "main.go"); err != nil {
		t.Fatal(err)
	}
	catFiles.Lock()
	for _, c := range catFiles.idle[repo] {
		c.cmd.Process.Kill()
		c.cmd.Wait()
	}
	catFiles.Unlock()
	for i := range 3 {
		if got, err := FileText(repo, "HEAD", "main.go"); err != nil || !strings.HasPrefix(got, "package main") {
			t.Fatalf("read %d after the process went: %q, %v", i, got, err)
		}
	}
}
