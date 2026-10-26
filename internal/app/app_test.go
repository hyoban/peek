package app

import (
	"image"
	"image/png"
	"os"
	"peek/internal/git"
	"peek/internal/testutil"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// testApp opens the app over the repository in a tester, its loads run
// at once; setup, when given, changes the app before its first frame.
func testApp(t *testing.T, repo string, setup func(*app)) (*app, *ui.Tester) {
	t.Helper()
	a := newApp(repo)
	a.inline = true
	if setup != nil {
		setup(a)
	}
	a.loadCommits()
	return a, ui.NewTester(a.view, 1200, 800)
}

// realApp opens the app over the repository PEEK_REPO names, for tests
// that draw it into PEEK_OUT, and skips them where those are not set.
func realApp(t *testing.T) (*app, string) {
	repo, out := os.Getenv("PEEK_REPO"), os.Getenv("PEEK_OUT")
	if repo == "" || out == "" {
		t.Skip("PEEK_REPO and PEEK_OUT not set")
	}
	a := openApp(repo)
	a.inline = true
	a.loadCommits()
	return a, out
}

// settle draws the frames the view asks for, then lets scroll bars fade
// and glides end.
func settle(tt *ui.Tester) {
	tt.Frame()
	tt.Advance(3 * time.Second)
}

func save(t *testing.T, name string, img image.Image) {
	f, err := os.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
}

// The view runs without a window in tests, which click as a user would.
func TestPanelsToggle(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []git.Commit{{Hash: "abc", Short: "abc", Subject: "Initial commit", Author: "Ada"}}
	a.loadedRow = 0 // nothing to load without a window
	a.commitRow = 0
	tt := ui.NewTester(a.view, 1200, 800)
	if !tt.HasText("1 commit") {
		t.Fatalf("history not shown, texts %q", tt.Texts())
	}
	if err := tt.Click("Hide History"); err != nil {
		t.Fatal(err)
	}
	// The column slides out, as Xcode's inspector does.
	tt.Advance(100 * time.Millisecond)
	if a.showHistory || a.historyShown <= 0 || a.historyShown >= 1 || !tt.HasText("1 commit") {
		t.Errorf("history not sliding out after toggling it off, shown %v, texts %q", a.historyShown, tt.Texts())
	}
	settle(tt)
	if tt.HasText("1 commit") {
		t.Errorf("history still shown after toggling it off, texts %q", tt.Texts())
	}
	if err := tt.Click("Hide Sidebar"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.showTree {
		t.Error("file tree still shown after toggling it off")
	}
	if !tt.HasText("No changes") || tt.HasText("Changed Files") {
		t.Errorf("diff pane missing, texts %q", tt.Texts())
	}
}

func TestFold(t *testing.T) {
	var tree pathTree
	tree.set(fold([]string{"src/b.go", "README.md", "src/ui/a.go"}))
	if got := tree.dirs[""]; len(got) != 2 || got[0] != "src" || got[1] != "README.md" {
		t.Errorf("roots %q, want [src README.md]", got)
	}
	if s := tree.dirs["src"]; len(s) != 2 || s[0] != "src/ui" {
		t.Errorf("src entries %q, want src/ui first", s)
	}
	if first := tree.files()[0]; first != "src/ui/a.go" {
		t.Errorf("first file %q, want src/ui/a.go", first)
	}
}

// replace keeps the folders open and knows the chosen file's row before
// the outline builds the new tree's rows, which then put it there.
func TestReplaceTree(t *testing.T) {
	tree := newPathTree()
	tree.set(fold([]string{"a/one.go", "b/two.go", "main.go"}))
	tt := ui.NewTester(func(c *ui.Context) {
		tree.view(c, "Files", func(string) ui.SourceListItem { return ui.SourceListItem{} })
	}, 400, 400)
	tree.state.Open.Add("a")
	tree.show("b/two.go")
	tt.Frame()
	tree.sync()
	tree.replace(fold([]string{"a/one.go", "a/zero.go", "b/two.go", "main.go"}))
	if !tree.state.Open.Has("a") || !tree.state.Open.Has("b") || tree.row != 4 {
		t.Fatalf("after replace: open %v, row %d", slices.Collect(tree.state.Open.All()), tree.row)
	}
	tt.Frame()
	if got := tree.chosen(); got != "b/two.go" {
		t.Errorf("the outline's row %d holds %q, want b/two.go", tree.row, got)
	}
}

// show chooses the file before the outline builds its row, and clearing
// the filter keeps it chosen, so that no pass shows the tree unchosen.
func TestShowChoosesAtOnce(t *testing.T) {
	tree := newPathTree()
	tree.set(fold([]string{"a/one.go", "b/two.go", "main.go"}))
	tt := ui.NewTester(func(c *ui.Context) {
		tree.applyFilter()
		tree.view(c, "Files", func(string) ui.SourceListItem { return ui.SourceListItem{} })
	}, 400, 400)
	tt.Frame()
	tree.show("b/two.go")
	if tree.row != 2 {
		t.Errorf("show chose row %d, want 2 before the pass", tree.row)
	}
	tt.Frame()
	tree.filter = "two"
	tt.Frame()
	tree.sync()
	tree.filter = ""
	tree.applyFilter()
	if tree.row != 2 {
		t.Errorf("clearing the filter chose row %d, want 2 before the pass", tree.row)
	}
	tt.Frame()
	if got := tree.chosen(); got != "b/two.go" {
		t.Errorf("the outline's row %d holds %q, want b/two.go", tree.row, got)
	}
}

func TestCompactFolders(t *testing.T) {
	var tree pathTree
	tree.set(fold([]string{
		"apps/android/app/src/main/kotlin/cc/x/data/Api.kt",
		"apps/android/app/src/main/kotlin/cc/x/ui/Screen.kt",
		"apps/android/README.md",
	}))
	if got := tree.dirs[""]; len(got) != 1 || got[0] != "apps/android" || tree.labels["apps/android"] != "apps/android" {
		t.Errorf("roots %q, want apps/android folded", got)
	}
	deep := "apps/android/app/src/main/kotlin/cc/x"
	if got := tree.dirs["apps/android"]; len(got) != 2 || got[0] != deep || got[1] != "apps/android/README.md" {
		t.Errorf("apps/android entries %q", got)
	}
	if l := tree.labels[deep]; l != "app/src/main/kotlin/cc/x" {
		t.Errorf("label %q", l)
	}
	if l := tree.labels[deep+"/data/Api.kt"]; l != "Api.kt" {
		t.Errorf("file label %q", l)
	}
	if !tree.has(deep+"/data/Api.kt") || tree.has(deep) {
		t.Error("has should list files alone")
	}
	tree.show(deep + "/ui/Screen.kt")
	if !tree.state.Open.Has("apps/android") || !tree.state.Open.Has(deep) || !tree.state.Open.Has(deep+"/ui") {
		t.Errorf("show opened %v, want every folder above the file", slices.Collect(tree.state.Open.All()))
	}
}

// The history's divider, at the end, follows the pointer: dragged to the
// left, the history widens.
func TestHistoryResize(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
	settle(tt)
	x := float32(1200) - a.historyWidth - 0.5 // the divider, a DIP wide
	tt.Press(x, 400)
	tt.Move(x-50, 400)
	tt.Move(x-100, 400)
	tt.Release(x-100, 400)
	tt.Frame()
	if a.historyWidth < 440 || a.historyWidth > 480 {
		t.Errorf("history width %v after dragging 100 to the left, want about 460", a.historyWidth)
	}
}

func TestGrouped(t *testing.T) {
	files := []string{
		"README.md", "docs/guide.png", "fileicons/LICENSE-file-icons.md",
		"git_test.go", "src/__tests__/app.tsx", "app.spec.ts", "FooTest.java", "test_util.py", "testdata/in.md",
		"Button.stories.tsx", "main.go", ".storybook/main.ts", "go.mod", "src/app.tsx", "Test.java", "latest.go", "fileicons/tables.go",
	}
	tree := newPathTree()
	tree.set(foldGrouped(files))
	tree.openAll()
	want := []string{
		".storybook/main.ts", "fileicons/tables.go", "src/app.tsx", "go.mod", "latest.go", "main.go", "Test.java",
		"src/__tests__/app.tsx", "testdata/in.md", "app.spec.ts", "Button.stories.tsx", "FooTest.java", "git_test.go", "test_util.py",
		"docs/guide.png", "fileicons/LICENSE-file-icons.md", "README.md",
	}
	if got := tree.files(); !slices.Equal(got, want) {
		t.Errorf("files\n %q\nwant\n %q", got, want)
	}
	var groups []string
	for _, g := range tree.dirs[""] {
		if !isGroup(g) {
			t.Errorf("%q at the root, want only groups", g)
		}
		groups = append(groups, tree.labels[g])
	}
	if !slices.Equal(groups, []string{"Code", "Tests", "Docs"}) {
		t.Errorf("groups %q", groups)
	}
	// fileicons is a directory in Code and in Docs both.
	if got := tree.labels[groupPrefix+"Docs/fileicons"]; got != "fileicons" {
		t.Errorf("Docs' fileicons labeled %q", got)
	}
	if got := itemPath(groupPrefix + "Docs/fileicons"); got != "fileicons" {
		t.Errorf("path of Docs' fileicons %q", got)
	}

	tree.state.Open.Clear()
	tree.show("src/__tests__/app.tsx")
	if !tree.state.Open.Has(groupPrefix+"Tests") || !tree.state.Open.Has(groupPrefix+"Tests/src/__tests__") {
		t.Errorf("open %v after showing a test", slices.Collect(tree.state.Open.All()))
	}

	// Files of one kind take no group.
	tree.set(foldGrouped([]string{"a.go", "b/c.go"}))
	if got := tree.dirs[""]; !slices.Equal(got, []string{"b", "a.go"}) {
		t.Errorf("root %q for code only", got)
	}
}

// As AppKit's split views do, the window's narrowing hides the history,
// then the sidebar, and its widening shows them again, but a column the
// user shows stays until the window resizes.
func TestColumnsCollapse(t *testing.T) {
	a := newApp("/tmp/repo")
	a.commits = []git.Commit{{Hash: "abc", Short: "abc", Subject: "Initial commit", Author: "Ada"}}
	a.loadedRow, a.commitRow = 0, 0
	tt := ui.NewTester(a.view, 1200, 800)
	tt.Frame()
	if !a.showTree || !a.showHistory {
		t.Fatal("columns hidden in a wide window")
	}
	tt.SetSize(900, 800)
	tt.Frame()
	if !a.showTree || a.showHistory {
		t.Fatalf("tree %v, history %v at 900", a.showTree, a.showHistory)
	}
	tt.SetSize(640, 800)
	tt.Frame()
	if a.showTree || a.showHistory || !tt.HasText("Ship") {
		t.Fatalf("tree %v, history %v at 640, texts %q", a.showTree, a.showHistory, tt.Texts())
	}
	if err := tt.Click("Show Sidebar"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.showTree {
		t.Fatal("the sidebar shown did not stay")
	}
	tt.SetSize(1300, 800)
	tt.Frame()
	if !a.showTree || !a.showHistory {
		t.Fatalf("tree %v, history %v after widening", a.showTree, a.showHistory)
	}
}

// The commit's message shows at the top of its diffs, with who made it
// and its hash, without asking; the work tree has none.
func TestCommitMessageShows(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.Git(t, repo, "add", "--all")
	testutil.Git(t, repo, "commit", "-q", "-m", "Add the docs", "-m", "Explain why the docs moved.")
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if tt.HasText("Explain why the docs moved.") {
		t.Fatal("a message shown for the work tree")
	}
	a.commitRow = 1
	settle(tt)
	cm, _ := a.current()
	if cm.Subject != "Add the docs" || !tt.HasText("Explain why the docs moved.") || !tt.HasText(cm.Short) || !tt.HasText("Ada") {
		t.Fatalf("commit %q, texts %q", cm.Subject, tt.Texts())
	}
}
