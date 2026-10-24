package main

import (
	"os"
	"path/filepath"
	"testing"
)

// A refresh follows the files: the diffs shown change in place, new files
// join the list, and a commit moves the history without losing the place.
func TestRefresh(t *testing.T) {
	repo := testRepo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	d := a.diffs[a.diffIndex("main.go")]
	if d.state == nil {
		t.Fatal("main.go diff not loaded")
	}
	write := func(name, text string) {
		p := filepath.Join(repo, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	write("main.go", "package main\n\nfunc main() { println() }\n")
	a.refresh()
	settle(tt)
	if a.diffs[a.diffIndex("main.go")] != d || d.state.New.String() != "package main\n\nfunc main() { println() }\n" {
		t.Errorf("main.go diff not updated in place: %q", d.state.New.String())
	}

	write("src/added.go", "package src\n")
	a.refresh()
	settle(tt)
	if _, ok := a.changes["src/added.go"]; !ok || a.diffIndex("src/added.go") < 0 {
		t.Errorf("new file missing from changes %v", a.changes)
	}
	if a.commits[0].Summary != "4 files changed" {
		t.Errorf("work tree summary %q", a.commits[0].Summary)
	}
	if a.diffs[a.diffIndex("main.go")] != d {
		t.Error("main.go diff made anew though it is the same change")
	}

	// Choose the first commit, then commit everything: the work tree's
	// entry stays with no changes, a commit comes, and the first stays
	// chosen.
	a.commitRow = 1
	tt.Frame()
	first := a.commits[1].Hash
	if _, err := git(repo, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Second"); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	settle(tt)
	if len(a.commits) != 3 || a.commits[0].Hash != worktree || a.commits[0].Summary != "No changes" || a.commits[1].Subject != "Second" {
		t.Fatalf("commits %+v, want the clean work tree, then Second over the first", a.commits)
	}
	if cm, ok := a.current(); !ok || cm.Hash != first || a.commitRow != a.loadedRow {
		t.Errorf("shown %+v (row %d, chosen %d), want the first commit", cm, a.loadedRow, a.commitRow)
	}
}

// A refresh in the files mode keeps the file open and shows its new text.
func TestRefreshFiles(t *testing.T) {
	repo := testRepo(t)
	a, tt := testApp(t, repo, nil)
	a.setMode(modeFiles)
	settle(tt)
	a.allTree.show("main.go")
	settle(tt)
	if a.openFile != "main.go" || a.code == nil {
		t.Fatalf("open file %q", a.openFile)
	}
	code := a.code
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package main // edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	settle(tt)
	if a.code != code || code.Buffer.String() != "package main // edited\n" {
		t.Errorf("text %q, want the edit in the same editor", code.Buffer.String())
	}
	if got := a.allTree.chosen(); got != "main.go" {
		t.Errorf("tree chose %q, want main.go", got)
	}
}

// A clean work tree still has its entry, whose files the files mode lists.
func TestCleanWorktree(t *testing.T) {
	repo := testRepo(t)
	if _, err := git(repo, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := git(repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Second"); err != nil {
		t.Fatal(err)
	}
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if cm, ok := a.current(); !ok || cm.Hash != worktree || cm.Summary != "No changes" {
		t.Fatalf("shown %+v, want the clean work tree", cm)
	}
	if !tt.HasText("No changes") {
		t.Errorf("texts %q", tt.Texts())
	}
	a.setMode(modeFiles)
	settle(tt)
	if !a.allTree.has("main.go") || !a.allTree.has("docs/new.md") {
		t.Errorf("work tree files %v", a.allTree.dirs)
	}
}
