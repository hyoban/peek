package app

import (
	"fmt"
	"os"
	"path/filepath"
	"peek/internal/filekind"
	"peek/internal/git"
	"peek/internal/testutil"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A refresh follows the files: the diffs shown change in place, new files
// join the list, and a commit moves the history without losing the place.
func TestRefresh(t *testing.T) {
	repo := testutil.Repo(t)
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

	// Lines added to a file changed already change its counts alone: the
	// diff and the tree stay, the group of docs opened by hand with them.
	docs := groupPrefix + filekind.Names[filekind.Docs]
	a.changeTree.state.Open.Add(docs)
	write("main.go", "package main\n\nfunc main() { println() }\n\nfunc other() {}\n")
	a.refresh()
	settle(tt)
	if a.diffs[a.diffIndex("main.go")] != d || !a.changeTree.state.Open.Has(docs) {
		t.Error("main.go diff or the tree made anew for new line counts")
	}
	if ch := a.changes["main.go"]; ch.Added != 4 || d.change.Added != 4 {
		t.Errorf("main.go counts %+v and %+v, want 4 added", ch, d.change)
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
	testutil.Git(t, repo, "add", "-A")
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Second")
	a.refresh()
	settle(tt)
	if len(a.commits) != 3 || a.commits[0].Hash != git.Worktree || a.commits[0].Summary != "No changes" || a.commits[1].Subject != "Second" {
		t.Fatalf("commits %+v, want the clean work tree, then Second over the first", a.commits)
	}
	if cm, ok := a.current(); !ok || cm.Hash != first || a.commitRow != a.loadedRow {
		t.Errorf("shown %+v (row %d, chosen %d), want the first commit", cm, a.loadedRow, a.commitRow)
	}
}

// A refresh in the files mode keeps the file open and shows its new text.
func TestRefreshFiles(t *testing.T) {
	repo := testutil.Repo(t)
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
	repo := testutil.Repo(t)
	testutil.Git(t, repo, "add", "-A")
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Second")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if cm, ok := a.current(); !ok || cm.Hash != git.Worktree || cm.Summary != "No changes" {
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

// A branch of commits of its own lists its changes since it left main,
// which show first where nothing is uncommitted, follow its new commits
// in place, and show again once it is checked out.
func TestBranchChanges(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Update the files")
	testutil.Git(t, repo, "push", "-q", "origin", "main")
	testutil.Git(t, repo, "switch", "-q", "-c", "feature")
	commit := func(name, text string) {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
		testutil.Git(t, repo, "add", "--all")
		testutil.Git(t, repo, "commit", "-q", "-m", "Change "+name)
	}
	commit("main.go", "package main\n\nfunc main() { run() }\n")
	commit("run.go", "package main\n\nfunc run() {}\n")

	a, tt := testApp(t, repo, nil)
	settle(tt)
	cm, ok := a.current()
	if !ok || !git.IsRange(cm.Hash) || a.loadedRow != 1 || cm.Summary != "2 commits since main" {
		t.Fatalf("shown %+v at row %d, want the branch's changes", cm, a.loadedRow)
	}
	if len(a.changes) != 2 || a.changes["run.go"].Status != "A" || a.changes["main.go"].Status != "M" {
		t.Fatalf("changes %+v, want main.go and run.go", a.changes)
	}
	d := a.diffs[a.diffIndex("main.go")]
	if d.state == nil || d.state.Old.String() != "package main\n\nfunc main() {}\n" || d.state.New.String() != "package main\n\nfunc main() { run() }\n" {
		t.Fatal("main.go diff not from where the branch left main")
	}
	if !tt.HasText("Branch Changes") || !tt.HasText("4 commits") {
		t.Errorf("history %q, want the branch's entry and 4 commits", tt.Texts())
	}

	// Another commit: the branch's entry stays shown, with it.
	commit("main.go", "package main\n\nfunc main() { run(); run() }\n")
	a.refresh()
	settle(tt)
	if cm, ok := a.current(); !ok || !git.IsRange(cm.Hash) || cm.Summary != "3 commits since main" {
		t.Fatalf("shown %+v after a commit, want the branch's changes", cm)
	}
	if a.diffs[a.diffIndex("main.go")] != d || d.state.New.String() != "package main\n\nfunc main() { run(); run() }\n" {
		t.Errorf("main.go diff not updated in place: %q", d.state.New.String())
	}

	// main has none; the branch, checked out again, shows its own.
	a.switchTo(switchTarget{branch: "main"})
	settle(tt)
	if cm, ok := a.current(); !ok || cm.Hash != git.Worktree || len(a.commits) > 0 && a.rowOf(git.Range("x", "y")) >= 0 {
		t.Fatalf("shown %+v on main, want the work tree and no branch entry", cm)
	}
	a.switchTo(switchTarget{branch: "feature"})
	settle(tt)
	if cm, ok := a.current(); !ok || !git.IsRange(cm.Hash) {
		t.Fatalf("shown %+v on feature checked out, want the branch's changes", cm)
	}

	// Uncommitted changes show first, though.
	a.switchTo(switchTarget{branch: "main"})
	settle(tt)
	os.WriteFile(filepath.Join(repo, "draft.txt"), []byte("draft\n"), 0o644)
	a.switchTo(switchTarget{branch: "feature"})
	settle(tt)
	if cm, ok := a.current(); !ok || cm.Hash != git.Worktree {
		t.Fatalf("shown %+v with a draft, want the work tree", cm)
	}
}

// A refresh that brings new files keeps the change tree's folders as the
// user left them and the file shown chosen in every pass, opening only the
// new folders, the new groups of tests and docs closed as they start.
func TestRefreshKeepsChangeTree(t *testing.T) {
	repo := testutil.Repo(t)
	a := newApp(repo)
	a.inline = true
	a.loadCommits()
	docs, code, tests := groupPrefix+"Docs", groupPrefix+"Code", groupPrefix+"Tests"
	var bad []string
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		if a.changeTree.dirs != nil && (!a.changeTree.state.Open.Has(docs) || a.changeTree.chosen() != a.file) {
			bad = append(bad, fmt.Sprintf("chose %q, docs open %v", a.changeTree.chosen(), a.changeTree.state.Open.Has(docs)))
		}
	}, 1200, 800)
	settle(tt)
	a.changeTree.state.Open.Add(docs)
	a.changeTree.show("main.go")
	settle(tt)
	bad = nil
	for _, name := range []string{"z/util.go", "z/util_test.go"} {
		os.MkdirAll(filepath.Join(repo, "z"), 0o755)
		if err := os.WriteFile(filepath.Join(repo, name), []byte("package z\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a.refresh()
	settle(tt)
	if len(bad) > 0 {
		t.Errorf("the tree changed as the files came: %v", bad)
	}
	if !a.changeTree.state.Open.Has(code+"/z") || a.changeTree.state.Open.Has(tests) || !a.changeTree.has("z/util_test.go") {
		t.Errorf("new folders: z open %v, tests open %v", a.changeTree.state.Open.Has(code+"/z"), a.changeTree.state.Open.Has(tests))
	}
}

// Changes only in folders git ignores, as a build's output, leave the
// history as it is; any other, or one git's own folder tells of, reads it.
func TestIgnoredOnly(t *testing.T) {
	repo := testutil.Repo(t)
	os.WriteFile(filepath.Join(repo, ".gitignore"), []byte("/build/\n*.log\n"), 0o644)
	os.MkdirAll(filepath.Join(repo, "build", "out"), 0o755)
	hints := func(paths ...string) []mygo.FileEvent {
		var evs []mygo.FileEvent
		for _, p := range paths {
			evs = append(evs, mygo.FileEvent{Op: mygo.FileHint, Path: p, IsDir: true})
		}
		return evs
	}
	for _, tc := range []struct {
		paths []string
		want  bool
	}{
		{[]string{"build"}, true},
		{[]string{"build", "build/out"}, true},
		{[]string{"build", "docs"}, false},
		{[]string{"."}, false},
		{[]string{".git"}, false},
		{[]string{".git/refs/heads"}, false},
	} {
		if got := ignoredOnly(repo, hints(tc.paths...)); got != tc.want {
			t.Errorf("ignoredOnly(%q) = %v, want %v", tc.paths, got, tc.want)
		}
	}
	if ignoredOnly(repo, []mygo.FileEvent{{Op: mygo.FileRemove, Path: "."}}) {
		t.Error("the project gone counts as ignored")
	}
}

// The history marks the commits not pushed yet, and names the remote's
// branch on the commit it is at.
func TestHistoryMarksUnpushed(t *testing.T) {
	repo, _ := testutil.RemoteRepo(t)
	testutil.Git(t, repo, "commit", "--allow-empty", "-m", "Local change")
	h := readHistory(repo)
	var local, pushed git.Commit
	for _, cm := range h.commits {
		switch cm.Subject {
		case "Local change":
			local = cm
		case "Initial commit":
			pushed = cm
		}
	}
	if !local.Unpushed || local.Remote != "" || pushed.Unpushed || pushed.Remote != "origin/main" {
		t.Fatalf("local %+v, pushed %+v", local, pushed)
	}
}
