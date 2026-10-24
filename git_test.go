package main

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// testRepo makes a repository of one commit, then changes one file,
// deletes another and adds one git does not track.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		if _, err := git(dir, append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.com"}, args...)...); err != nil {
			t.Fatal(err)
		}
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

func TestWorktreeEntry(t *testing.T) {
	repo := testRepo(t)
	a, tt := testApp(t, repo, nil)
	if len(a.commits) != 2 || a.commits[0].Hash != worktree || a.commits[1].Subject != "Initial commit" {
		t.Fatalf("commits %+v, want the work tree first", a.commits)
	}
	if a.commits[0].Summary != "3 files changed" {
		t.Errorf("work tree summary %q", a.commits[0].Summary)
	}
	if !tt.HasText("Uncommitted Changes") {
		t.Errorf("texts %q", tt.Texts())
	}
	for _, p := range []string{"main.go", "old.txt", "docs/new.md"} {
		if _, ok := a.changes[p]; !ok {
			t.Errorf("%s missing from changes %v", p, a.changes)
		}
	}
	if s := a.changes["old.txt"].Status; s != "D" {
		t.Errorf("old.txt status %q, want D", s)
	}
	if s := a.changes["docs/new.md"].Status; s != "A" {
		t.Errorf("docs/new.md status %q, want A", s)
	}

	before, after := fileTexts(repo, worktree, a.changes["main.go"])
	if before != "package main\n" || after != "package main\n\nfunc main() {}\n" {
		t.Errorf("main.go texts %q → %q", before, after)
	}
	if before, after := fileTexts(repo, worktree, a.changes["docs/new.md"]); before != "" || after != "# New\n" {
		t.Errorf("docs/new.md texts %q → %q", before, after)
	}
}

func TestFilesMode(t *testing.T) {
	a, tt := testApp(t, testRepo(t), nil)
	shown := a.file
	if shown == "" {
		t.Fatal("no file shown in changes mode")
	}
	if err := tt.Click("Files"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.mode != modeFiles {
		t.Fatalf("mode %d after clicking Files", a.mode)
	}
	if tt.HasText("Uncommitted Changes") {
		t.Error("the history shows in files mode")
	}
	if !a.showHistory {
		t.Error("files mode turned the history toggle off; it should only hide the column")
	}
	if !a.allTree.has("main.go") || !a.allTree.has("docs/new.md") || a.allTree.has("old.txt") {
		t.Errorf("work tree files %v, want main.go and docs/new.md without old.txt", a.allTree.dirs)
	}
	if a.openFile != shown || a.code == nil {
		t.Fatalf("open file %q (code %v), want %q carried over", a.openFile, a.code != nil, shown)
	}
	if got := a.allTree.chosen(); got != shown {
		t.Errorf("tree chose %q, want %q", got, shown)
	}
	if err := tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("Uncommitted Changes") {
		t.Error("the history did not come back with the changes")
	}
	a.setMode(modeFiles)
	tt.Frame()

	// The commit before the changes has old.txt and the first main.go.
	a.commitRow = 1
	tt.Frame()
	if !a.allTree.has("old.txt") {
		t.Errorf("commit files %v, want old.txt", a.allTree.dirs)
	}
	a.allTree.show("main.go")
	tt.Frame()
	if a.openFile != "main.go" || a.code == nil || a.code.Buffer.String() != "package main\n" {
		t.Errorf("open %q, want main.go of the commit", a.openFile)
	}
}

func TestStackedDiffs(t *testing.T) {
	repo := testRepo(t)
	for i := range 30 {
		os.WriteFile(filepath.Join(repo, fmt.Sprintf("z%02d.txt", i)), []byte(strings.Repeat("line\n", 20)), 0o644)
	}
	a, tt := testApp(t, repo, nil)
	var order []string
	for _, d := range a.diffs {
		order = append(order, d.change.Path)
	}
	if len(order) != 33 || order[0] != "main.go" || order[1] != "docs/new.md" || order[2] != "old.txt" {
		t.Fatalf("diff order %q, want code, then docs, each in the tree's order", order[:min(len(order), 4)])
	}
	if !tt.HasText("main.go") || !tt.HasText("old.txt") {
		t.Errorf("headers missing, texts %q", tt.Texts())
	}
	if a.diffs[0].state == nil {
		t.Error("the diff of main.go in view did not load")
	}
	if a.diffs[32].state != nil || a.diffs[32].loading {
		t.Error("the diff of the last file loaded before it came into view")
	}

	a.changeTree.show("z29.txt")
	settle(tt)
	if a.file != "z29.txt" || a.diffs[32].state == nil {
		t.Errorf("file %q after choosing z29.txt in the tree; its diff loaded: %v", a.file, a.diffs[32].state != nil)
	}
	if first, last := a.diffList.Visible(); headerRow(32) < first || headerRow(32) > last {
		t.Errorf("rows %d–%d in view, want z29.txt's header %d", first, last, headerRow(32))
	}
	if got := a.changeTree.chosen(); got != "z29.txt" {
		t.Errorf("tree chose %q after the scroll settled", got)
	}

	// The user scrolls the diffs: the tree follows the file at the top.
	a.diffList.ScrollTo(headerRow(10), ui.Start)
	settle(tt)
	want := a.diffs[10].change.Path
	if a.file != want || a.changeTree.chosen() != want {
		t.Errorf("after scrolling to %s, file %q and tree %q", want, a.file, a.changeTree.chosen())
	}
}

func TestClickFolderTogglesIt(t *testing.T) {
	a, tt := testApp(t, testRepo(t), func(a *app) { a.mode = modeFiles })
	a.allTree.state.Open.Clear() // as a commit's files first show
	tt.Frame()
	if n := a.allTree.state.Rows(); n != 2 {
		t.Fatalf("%d rows with docs closed, want docs and main.go", n)
	}
	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "docs" || a.allTree.state.Rows() != 3 {
		t.Errorf("after a click on docs: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "docs" {
		t.Errorf("after a second click on docs: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.allTree.chosen() != "main.go" || a.openFile != "main.go" {
		t.Errorf("a click on main.go chose %q and opened %q", a.allTree.chosen(), a.openFile)
	}
}

// A click on a folder's arrow does what a click on its row does.
func TestArrowClickActsAsRowClick(t *testing.T) {
	a, tt := testApp(t, testRepo(t), func(a *app) { a.mode = modeFiles })
	if err := tt.Click("main.go"); err != nil { // the choice elsewhere
		t.Fatal(err)
	}
	a.allTree.state.Open.Clear()
	a.allTree.remember()
	tt.Frame()
	label, ok := tt.Find("docs")
	if !ok {
		t.Fatal("no docs row")
	}
	arrowX := label.X - 6 - 16 - 8 // past the gap, the icon, to the arrow's middle
	tt.ClickAt(arrowX, label.Y+label.H/2)
	tt.Frame()
	if !a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "docs" {
		t.Errorf("after a click on docs' arrow: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	tt.ClickAt(arrowX, label.Y+label.H/2)
	tt.Frame()
	if a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "docs" {
		t.Errorf("after a second click on the arrow: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	if a.openFile != "main.go" {
		t.Errorf("choosing a folder closed the file shown, %q", a.openFile)
	}
}

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

// The button to GitHub shows where a remote is there, origin first.
func TestGitHubButton(t *testing.T) {
	repo := testRepo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if a.github != "" || tt.HasText("Open on GitHub") {
		t.Fatal("GitHub button without a remote")
	}
	git(repo, "remote", "add", "upstream", "https://github.com/egoist/peek.git")
	git(repo, "remote", "add", "origin", "git@github.com:hyoban/peek.git")
	a.refresh()
	settle(tt)
	if a.github != "https://github.com/hyoban/peek" {
		t.Errorf("github %q, want origin's page", a.github)
	}
}

func TestReadDiffs(t *testing.T) {
	a, tt := testApp(t, testRepo(t), nil)
	settle(tt)
	read := func() []bool {
		var r []bool
		for _, d := range a.diffs {
			r = append(r, d.read)
		}
		return r
	}
	// main.go, docs/new.md, old.txt: tests and docs start read.
	if got := read(); fmt.Sprint(got) != "[false true true]" {
		t.Fatalf("read %v, want the docs read", got)
	}
	if open := a.changeTree.state.Open; !open.Has(groupPrefix+"Code") || open.Has(groupPrefix+"Docs") {
		t.Errorf("open %v, want Code open and Docs closed", slices.Collect(open.All()))
	}
	if !tt.HasText("Viewed") {
		t.Errorf("no Viewed box, texts %q", tt.Texts())
	}

	// The marks stay with the commit while another shows.
	a.setRead(a.diffs[0], true)
	a.setRead(a.diffs[2], false)
	a.commitRow = 1
	settle(tt)
	if len(a.diffs) == 0 || a.diffs[0].read {
		t.Errorf("the first commit's main.go read %v", read())
	}
	a.commitRow = 0
	settle(tt)
	if got := read(); fmt.Sprint(got) != "[true true false]" {
		t.Errorf("read %v after coming back, want the marks kept", got)
	}
}

func TestReadByGroup(t *testing.T) {
	repo := testRepo(t)
	os.WriteFile(filepath.Join(repo, "docs", "more.md"), []byte("# More\n"), 0o644)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	// main.go, then the docs: docs/more.md, docs/new.md, old.txt.
	read := func() string {
		var r []bool
		for _, d := range a.diffs {
			r = append(r, d.read)
		}
		return fmt.Sprint(r)
	}
	if !tt.HasText("3/3") {
		t.Errorf("no count of the docs read, texts %q", tt.Texts())
	}

	if err := tt.RightClick("Docs"); err != nil {
		t.Fatal(err)
	}
	if got := fmt.Sprint(tt.Menu()); got != "[Mark as Viewed Mark as Not Viewed]" {
		t.Errorf("menu %s", got)
	}
	if err := tt.ChooseMenuItem("Mark as Not Viewed"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if got := read(); got != "[false false false false]" {
		t.Errorf("read %s after marking Docs not viewed", got)
	}
	if !tt.HasText("0/3") {
		t.Errorf("no count of none read, texts %q", tt.Texts())
	}

	// A folder marks its files only.
	a.changeTree.state.Open.Add(groupPrefix + "Docs")
	settle(tt)
	if err := tt.RightClick("docs"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Mark as Viewed"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if got := read(); got != "[false true true false]" {
		t.Errorf("read %s after marking the docs folder viewed", got)
	}
	if !tt.HasText("2/3") {
		t.Errorf("no count of two read, texts %q", tt.Texts())
	}
}
