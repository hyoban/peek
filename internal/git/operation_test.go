package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"peek/internal/testutil"
	"testing"
)

// A merge stopped on a conflict is named for the branch it merges in and
// counts the files left to resolve; once it is committed, none shows.
func TestReadOperationMerge(t *testing.T) {
	repo := testutil.Repo(t)
	run := func(args ...string) {
		testutil.Git(t, repo, append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.com", "-c", "commit.gpgsign=false"}, args...)...)
	}
	write := func(name, text string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if op := ReadOperation(repo); op != (Operation{}) {
		t.Fatalf("no operation: got %+v", op)
	}
	run("add", "--all")
	run("commit", "-q", "-m", "base")
	run("switch", "-q", "-c", "topic")
	write("main.go", "package main // topic\n")
	run("commit", "-q", "-am", "topic")
	run("switch", "-q", "main")
	write("main.go", "package main // main\n")
	run("commit", "-q", "-am", "main")
	cmd := exec.Command("git", "-C", repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "merge", "topic")
	if out, err := cmd.CombinedOutput(); err == nil {
		t.Fatalf("the merge should conflict:\n%s", out)
	}
	if op := ReadOperation(repo); op != (Operation{Title: "Merging topic", Conflicts: 1}) {
		t.Fatalf("conflicted merge: got %+v", op)
	}
	write("main.go", "package main // both\n")
	run("add", "main.go")
	if op := ReadOperation(repo); op != (Operation{Title: "Merging topic"}) {
		t.Fatalf("resolved merge: got %+v", op)
	}
	run("commit", "-q", "--no-edit")
	if op := ReadOperation(repo); op != (Operation{}) {
		t.Fatalf("committed merge: got %+v", op)
	}
}
