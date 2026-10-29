package app

import (
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestWorktreeEntry(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	if len(a.commits) != 2 || a.commits[0].Hash != git.Worktree || a.commits[1].Subject != "Initial commit" {
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

	before, after := git.FileTexts(repo, git.Worktree, a.changes["main.go"])
	if before != "package main\n" || after != "package main\n\nfunc main() {}\n" {
		t.Errorf("main.go texts %q → %q", before, after)
	}
	if before, after := git.FileTexts(repo, git.Worktree, a.changes["docs/new.md"]); before != "" || after != "# New\n" {
		t.Errorf("docs/new.md texts %q → %q", before, after)
	}
}

func TestFilesMode(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
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
	if !tt.HasText("Uncommitted Changes") {
		t.Error("the history does not show in files mode")
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
		t.Error("the history does not show in changes mode")
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
	repo := testutil.Repo(t)
	for i := range 40 {
		os.WriteFile(filepath.Join(repo, fmt.Sprintf("z%02d.txt", i)), []byte(strings.Repeat("line\n", 20)), 0o644)
	}
	a, tt := testApp(t, repo, nil)
	var order []string
	for _, d := range a.diffs {
		order = append(order, d.change.Path)
	}
	if len(order) != 43 || order[0] != "main.go" || order[1] != "docs/new.md" || order[2] != "old.txt" {
		t.Fatalf("diff order %q, want code, then docs, each in the tree's order", order[:min(len(order), 4)])
	}
	if !tt.HasText("main.go") || !tt.HasText("old.txt") {
		t.Errorf("headers missing, texts %q", tt.Texts())
	}
	if a.diffs[0].state == nil {
		t.Error("the diff of main.go in view did not load")
	}
	if a.diffs[42].state != nil || a.diffs[42].loading {
		t.Error("the diff of the last file loaded before it came into view")
	}

	a.changeTree.show("z39.txt")
	settle(tt)
	if a.file != "z39.txt" || a.diffs[42].state == nil {
		t.Errorf("file %q after choosing z39.txt in the tree; its diff loaded: %v", a.file, a.diffs[42].state != nil)
	}
	if first, last := a.diffList.Visible(); headerRow(42) < first || headerRow(42) > last {
		t.Errorf("rows %d–%d in view, want z39.txt's header %d", first, last, headerRow(42))
	}
	if got := a.changeTree.chosen(); got != "z39.txt" {
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

// A click on a folder chooses it and leaves it as it is, as AppKit's
// outline views keep choosing and opening apart.
func TestClickFolderChoosesIt(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), func(a *app) { a.mode = modeFiles })
	a.allTree.state.Open.Clear() // as a commit's files first show
	tt.Frame()
	if n := a.allTree.state.Rows(); n != 2 {
		t.Fatalf("%d rows with docs closed, want docs and main.go", n)
	}
	if err := tt.Click("docs"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "docs" || a.allTree.state.Rows() != 2 {
		t.Errorf("after a click on docs: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.allTree.chosen() != "main.go" || a.openFile != "main.go" {
		t.Errorf("a click on main.go chose %q and opened %q", a.allTree.chosen(), a.openFile)
	}
}

// A click on a folder's arrow opens and closes it, and leaves the choice
// as it is.
func TestArrowOpensWithoutChoosing(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), func(a *app) { a.mode = modeFiles })
	if err := tt.Click("main.go"); err != nil { // the choice elsewhere
		t.Fatal(err)
	}
	a.allTree.state.Open.Clear()
	tt.Frame()
	label, ok := tt.Find("docs")
	if !ok {
		t.Fatal("no docs row")
	}
	arrowX := label.X - 6 - 16 - 8 // past the gap, the icon, to the arrow's middle
	tt.ClickAt(arrowX, label.Y+label.H/2)
	tt.Frame()
	if !a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "main.go" {
		t.Errorf("after a click on docs' arrow: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	tt.ClickAt(arrowX, label.Y+label.H/2)
	tt.Frame()
	if a.allTree.state.Open.Has("docs") || a.allTree.chosen() != "main.go" {
		t.Errorf("after a second click on the arrow: open %v, chosen %q", a.allTree.state.Open.Has("docs"), a.allTree.chosen())
	}
	if a.openFile != "main.go" {
		t.Errorf("opening a folder closed the file shown, %q", a.openFile)
	}
}

// The button to GitHub shows where a remote is there, origin first.
func TestGitHubButton(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if a.github != "" || tt.HasText("Open on GitHub") {
		t.Fatal("GitHub button without a remote")
	}
	testutil.Git(t, repo, "remote", "add", "upstream", "https://github.com/egoist/peek.git")
	testutil.Git(t, repo, "remote", "add", "origin", "git@github.com:hyoban/peek.git")
	a.refresh()
	settle(tt)
	if a.github != "https://github.com/hyoban/peek" {
		t.Errorf("github %q, want origin's page", a.github)
	}
}

func TestReadDiffs(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
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

	// A click on a read file's bar shows its diff, still read, and
	// another folds it; Viewed folds it again.
	docs := a.diffs[1]
	if err := tt.Click("new.md"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !docs.read || docs.folded() {
		t.Errorf("after a click on its bar new.md read %v, folded %v; want read and shown", docs.read, docs.folded())
	}
	if err := tt.Click("new.md"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if !docs.folded() {
		t.Error("a second click on its bar did not fold new.md")
	}
	// Its Viewed box, on the bar, marks it and does not open it too.
	name, _ := tt.Find("new.md")
	box, ok := tt.Find("Viewed")
	if !ok {
		t.Fatal("no Viewed box")
	}
	tt.ClickAt(box.X+box.W/2, name.Y+name.H/2)
	settle(tt)
	if docs.read || docs.flipped {
		t.Errorf("after its Viewed box new.md read %v, flipped %v; want neither", docs.read, docs.flipped)
	}
	docs.flipped = true
	a.setRead(docs, true)
	if !docs.folded() {
		t.Error("marking new.md viewed again did not fold it")
	}

	// Choosing a read file in the tree shows its diff, still read.
	a.changeTree.show("docs/new.md")
	settle(tt)
	if a.file != "docs/new.md" || !docs.read || docs.folded() {
		t.Errorf("after choosing new.md in the tree, file %q, read %v, folded %v; want it read and shown", a.file, docs.read, docs.folded())
	}

	// A click on the bar of a file not read folds it too, still not read,
	// and choosing it in the tree shows it again.
	code := a.diffs[0]
	a.scrollToFile("main.go")
	settle(tt)
	box, ok = tt.Find("Viewed") // main.go's, at the top
	if !ok {
		t.Fatal("no Viewed box")
	}
	tt.ClickAt(box.X-120, box.Y+box.H/2)
	settle(tt)
	if code.read || !code.folded() {
		t.Errorf("after a click on its bar main.go read %v, folded %v; want folded and not read", code.read, code.folded())
	}
	a.changeTree.show("main.go")
	settle(tt)
	if code.read || code.folded() {
		t.Errorf("after choosing main.go in the tree, read %v, folded %v; want it shown", code.read, code.folded())
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
	repo := testutil.Repo(t)
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

func TestImportsFolded(t *testing.T) {
	for _, tc := range []struct {
		path, text string
		want       []int
	}{
		{"main.go", "package main\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc main() {\n\tfmt.Println(os.Args)\n}\n", []int{2}},
		{"one.go", "package main\n\nimport \"fmt\"\n\nfunc main() { fmt.Println() }\n", nil},
		{"app.ts", "import {\n  a,\n  b,\n} from './x'\nimport c from './c'\n\nconsole.log(a, b, c)\n", []int{0}},
		{"app.js", "import a from './a'\nimport b from './b'\n\nconsole.log(a, b)\n", []int{0}},
		{"app.py", "from x import (\n    a,\n    b,\n)\n\nprint(a, b)\n", []int{0}},
	} {
		if got := newCode(tc.path, tc.text).Folded(); !slices.Equal(got, tc.want) {
			t.Errorf("%s folded %v, want %v", tc.path, got, tc.want)
		}
	}
}

// Choosing another commit in files mode keeps the files and the text of
// the one shown until the new ones come, not to flash a spinner and an
// empty pane.
func TestFilesKeptWhileCommitLoads(t *testing.T) {
	a := newApp(testutil.Repo(t))
	a.inline, a.mode = true, modeFiles
	a.loadCommits()
	blank := false
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		if a.openFile != "" && (a.allTree.dirs == nil || a.code == nil) {
			blank = true
		}
	}, 1200, 800)
	settle(tt)
	if a.openFile == "" || a.code == nil {
		t.Fatalf("open file %q (code %v) before choosing a commit", a.openFile, a.code != nil)
	}
	blank = false
	a.commitRow = 1
	settle(tt)
	if blank {
		t.Error("the files or the text went blank while the commit loaded")
	}
	if !a.allTree.has("old.txt") || a.openFile != "main.go" || a.code.Buffer.String() != "package main\n" {
		t.Errorf("open %q, files %v, want main.go of the commit", a.openFile, a.allTree.dirs)
	}
}

// Choosing another commit in files mode keeps the folders open and the
// file chosen in every pass, not to flash the tree closed and unchosen.
func TestFilesTreeKeptAcrossCommits(t *testing.T) {
	repo := testutil.Repo(t)
	for name, text := range map[string]string{"a/one.go": "package a\n", "b/two.go": "package b\n", "docs/new.md": "# New\n"} {
		os.MkdirAll(filepath.Dir(filepath.Join(repo, name)), 0o755)
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	commit := func(msg string) {
		testutil.Git(t, repo, "add", "-A")
		testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", msg)
	}
	commit("Add packages")
	os.WriteFile(filepath.Join(repo, "b", "three.go"), []byte("package b\n"), 0o644)
	commit("Add three")
	a := newApp(repo)
	a.inline, a.mode = true, modeFiles
	a.loadCommits()
	var bad []string
	tt := ui.NewTester(func(c *ui.Context) {
		a.view(c)
		if a.filesFor >= 0 && a.allTree.dirs != nil && (!a.allTree.state.Open.Has("a") || a.allTree.chosen() != "b/two.go") {
			bad = append(bad, fmt.Sprintf("row %d chose %q, a open %v", a.commitRow, a.allTree.chosen(), a.allTree.state.Open.Has("a")))
		}
	}, 1200, 800)
	settle(tt)
	a.allTree.state.Open.Add("a")
	a.openInFiles(a.commits[a.loadedRow].Hash, "b/two.go")
	settle(tt)
	bad = nil
	for row := 1; row <= 2; row++ { // Add three, then Add packages
		a.commitRow = row
		settle(tt)
	}
	if len(bad) > 0 {
		t.Errorf("the tree changed as the commits loaded: %v", bad)
	}
	if a.filesFor != 2 || a.allTree.has("b/three.go") {
		t.Errorf("files for row %d, three.go %v, want Add packages' files", a.filesFor, a.allTree.has("b/three.go"))
	}
}

func TestLineCounts(t *testing.T) {
	repo := testutil.Repo(t)
	changes, err := git.LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string][2]int{}
	for _, ch := range changes {
		got[ch.Path] = [2]int{ch.Added, ch.Deleted}
	}
	want := map[string][2]int{"main.go": {2, 0}, "old.txt": {0, 1}, "docs/new.md": {1, 0}}
	if !maps.Equal(got, want) {
		t.Errorf("line counts %v, want %v", got, want)
	}
	// Counted, the file git does not track stays so, the index as it was.
	if staged := testutil.Git(t, repo, "diff", "--cached", "--name-only"); staged != "" {
		t.Errorf("index changed: %q", staged)
	}
	if others := testutil.Git(t, repo, "ls-files", "--others", "--exclude-standard"); others != "docs/new.md" {
		t.Errorf("untracked %q, want docs/new.md", others)
	}
	_, tt := testApp(t, repo, nil)
	for _, s := range []string{"+3", "−1"} {
		if !tt.HasText(s) {
			t.Errorf("no total %q in %q", s, tt.Texts())
		}
	}
}

// Paths git would quote, a rename's and a link's count as git counts them.
func TestLineCountsOddPaths(t *testing.T) {
	repo := testutil.Repo(t)
	run := func(args ...string) {
		testutil.Git(t, repo, append([]string{"-c", "user.name=Ada", "-c", "user.email=ada@example.com"}, args...)...)
	}
	os.WriteFile(filepath.Join(repo, "é.txt"), []byte("one\n"), 0o644)
	run("add", "-A")
	run("commit", "-q", "-m", "Second")
	os.WriteFile(filepath.Join(repo, "é.txt"), []byte("one\ntwo\n"), 0o644)
	run("mv", "main.go", "app.go")
	os.Symlink("docs/new.md", filepath.Join(repo, "link"))
	if err := syscall.Mkfifo(filepath.Join(repo, "fifo"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Text git diffs as binary, by its attributes or its driver's.
	// And bytes git diffs as text, by theirs; text git stores converted,
	// and a file past git's threshold for big ones.
	os.WriteFile(filepath.Join(repo, ".gitattributes"), []byte("*.svg -diff\n*.lock diff=lock\n*.dat diff\n*.raw diff=raw\n*.u16 working-tree-encoding=UTF-16LE\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "two.u16"), []byte("a\x00\n\x00b\x00\n\x00"), 0o644)
	run("config", "core.bigFileThreshold", "1k")
	os.WriteFile(filepath.Join(repo, "big.log"), []byte(strings.Repeat("line\n", 1000)), 0o644)
	run("config", "diff.lock.binary", "true")
	run("config", "diff.raw.binary", "false")
	os.WriteFile(filepath.Join(repo, "a.dat"), []byte("a\x00b\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "b.raw"), []byte("a\x00b\nc\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "icon.svg"), []byte("<svg>\n</svg>\n"), 0o644)
	os.WriteFile(filepath.Join(repo, "deps.lock"), []byte("a\nb\n"), 0o644)
	changes, err := git.LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]git.Change{}
	for _, ch := range changes {
		got[ch.Path] = ch
	}
	if ch := got["é.txt"]; ch.Status != "M" || ch.Added != 1 {
		t.Errorf("é.txt %+v, want 1 added", ch)
	}
	if ch := got["app.go"]; ch.Status != "R" || ch.Old != "main.go" {
		t.Errorf("app.go %+v, want renamed from main.go", ch)
	}
	if ch := got["link"]; ch.Added != 1 {
		t.Errorf("link %+v, want its target's one line", ch)
	}
	for _, p := range []string{"icon.svg", "deps.lock", "big.log"} {
		if ch := got[p]; ch.Added != 0 {
			t.Errorf("%s %+v, want none as git diffs it as binary", p, ch)
		}
	}
	for p, want := range map[string]int{".gitattributes": 5, "a.dat": 1, "b.raw": 2, "two.u16": 2} {
		if ch := got[p]; ch.Added != want {
			t.Errorf("%s %+v, want %d added", p, ch, want)
		}
	}
	if ch := got["fifo"]; ch.Added != 0 {
		t.Errorf("fifo %+v, want none", ch)
	}
}

func TestAuthors(t *testing.T) {
	cm := git.Commit{Author: "Ada", CoAuthors: []git.Person{{Name: "Bob Lee", Email: "bob@example.com"}, {Name: "Cy", Email: "cy@example.com"}}}
	if s := authors(cm); s != "Ada and 2 others" {
		t.Errorf("authors %q", s)
	}
	if s := authors(git.Commit{Author: "Ada"}); s != "Ada" {
		t.Errorf("authors %q", s)
	}
}

// A Markdown file shows its source until its preview is asked for, which
// stays the file's alone.
func TestMarkdownPreview(t *testing.T) {
	repo := testutil.Repo(t)
	if err := os.WriteFile(filepath.Join(repo, "docs", "pic.png"), testutil.PNG(t), 0o644); err != nil {
		t.Fatal(err)
	}
	a, tt := testApp(t, repo, func(a *app) { a.mode = modeFiles })
	a.allTree.show("docs/new.md")
	tt.Frame()
	if a.openFile != "docs/new.md" || a.preview == nil || a.previewing() {
		t.Fatalf("open %q (preview %v, previewing %v), want docs/new.md's source", a.openFile, a.preview != nil, a.previewing())
	}
	if err := tt.Click("Show Preview"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.previewing() || !tt.HasText("Show Source") {
		t.Fatal("a click on Show Preview did not show the preview")
	}
	a.allTree.show("main.go")
	tt.Frame()
	if a.preview != nil || a.previewing() || tt.HasText("Show Preview") {
		t.Error("main.go has a preview")
	}
	a.allTree.show("docs/new.md")
	tt.Frame()
	if !a.previewing() {
		t.Error("docs/new.md went back to its source")
	}

	for link, want := range map[string]string{
		"pic.png": "docs/pic.png", "./pic.png": "docs/pic.png", "/docs/pic.png": "docs/pic.png", "pic.png?raw=1": "docs/pic.png",
		"../../pic.png": "", "https://example.com/pic.png": "", "new.md": "",
	} {
		if got, _ := picturePath("docs/new.md", link); got != want {
			t.Errorf("picture %q at %q, want %q", link, got, want)
		}
	}
	if _, err := a.preview.opts.LoadImage("pic.png"); err != nil {
		t.Fatal(err)
	}

	// Edited, the file and its picture show anew in the preview.
	md := a.preview
	if a.reloadFiles(); a.preview != md {
		t.Error("the preview showed anew without a change")
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "new.md"), []byte("# Newer\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.reloadFiles(); a.preview == md || a.preview.text != "# Newer\n" {
		t.Error("the preview did not follow the edited text")
	}
	md = a.preview
	if _, err := md.opts.LoadImage("pic.png"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "docs", "pic.png"), append(testutil.PNG(t), 0), 0o644); err != nil {
		t.Fatal(err)
	}
	if a.reloadFiles(); a.preview == md {
		t.Error("the preview did not follow the edited picture")
	}
}

func TestHTMLImages(t *testing.T) {
	picture := `<p align="center">
<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/a-dark.png 2x, docs/b.png">
  <img alt="A" src="docs/a-light.png" width="400">
</picture>
</p>`
	for _, tc := range []struct {
		block    string
		dark     bool
		want     []htmlImage
		centered bool
	}{
		{picture, false, []htmlImage{{"docs/a-light.png", "A", 400}}, true},
		{picture, true, []htmlImage{{"docs/a-dark.png", "A", 400}}, true},
		{`<a href="x"><img src="b.svg"></a> <img src="c.png">`, false, []htmlImage{{"b.svg", "", 0}, {"c.png", "", 0}}, false},
		{`<details><summary>More</summary><img src="b.svg"></details>`, false, nil, false},
		{`<p>Words <img src="b.svg"></p>`, false, nil, false},
	} {
		got, centered := htmlImages(tc.block, tc.dark)
		if !slices.Equal(got, tc.want) || centered != tc.centered {
			t.Errorf("htmlImages(%q, dark %v) = %v, %v; want %v, %v", tc.block, tc.dark, got, centered, tc.want, tc.centered)
		}
	}
}

func TestSplitRefs(t *testing.T) {
	for text, want := range map[string][]string{
		"ci: run checks (#44610)":    {"ci: run checks (", "#44610", ")"},
		"#12 and #3":                 {"#12", " and ", "#3"},
		"no refs":                    {"no refs"},
		"a#1 &#123; owner/repo#9 #x": {"a#1 &#123; owner/repo#9 #x"},
	} {
		if got := splitRefs(text); !slices.Equal(got, want) {
			t.Errorf("splitRefs(%q) = %q, want %q", text, got, want)
		}
	}
}

// The filter at the bottom of the sidebar keeps the files whose paths hold
// its words, in their folders opened, and as it clears the folders close
// again and the file chosen is chosen again.
func TestFilterFiles(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), func(a *app) { a.mode = modeFiles })
	tree := &a.allTree
	tree.state.Open.Clear()
	tt.Frame()
	if err := tt.Click("main.go"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	_, all := tree.filterCount()
	tree.filter = "NEW"
	settle(tt)
	if !tree.state.Open.Has("docs") || tree.state.Rows() != 2 || tree.state.Item(1) != "docs/new.md" {
		t.Fatalf("rows %d, open docs %v", tree.state.Rows(), tree.state.Open.Has("docs"))
	}
	if !tt.HasText(fmt.Sprintf("1 of %d files", all)) || a.openFile != "main.go" {
		t.Fatalf("open %q, texts %q", a.openFile, tt.Texts())
	}
	tree.filter = ""
	settle(tt)
	if tree.state.Open.Has("docs") || tree.chosen() != "main.go" {
		t.Fatalf("after clearing: docs open %v, chosen %q", tree.state.Open.Has("docs"), tree.chosen())
	}
	tree.filter = "nothing like it"
	settle(tt)
	if !tt.HasText("No Results") || !tt.HasText("Filter Files") {
		t.Fatal(tt.Texts())
	}

	// Filter Files puts the keys in the field, and Down moves them to the
	// tree's first file.
	tree.filter = ""
	settle(tt)
	a.filterFiles()
	tt.Frame()
	tt.Frame()
	tt.Type("new")
	settle(tt)
	if tree.filter != "new" {
		t.Fatalf("typed into the filter: %q", tree.filter)
	}
	tt.Key(0, ui.KeyDown)
	settle(tt)
	if tree.chosen() != "docs/new.md" || a.openFile != "docs/new.md" {
		t.Fatalf("Down chose %q, opened %q", tree.chosen(), a.openFile)
	}
}
