package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

func TestFoldFiles(t *testing.T) {
	files := []string{"apps/web/src/main.go", "apps/web/src/main_test.go", "apps/web/go.mod", "apps/web/go.sum", "go.sum", "other/main_test.go", "package.json", ".npmrc/config"}
	tree := newPathTree()
	tree.set(foldFiles(files, true))
	if tree.parents["apps/web/go.sum"] != "apps/web/go.mod" || tree.parents["apps/web/src/main_test.go"] != "apps/web/src/main.go" {
		t.Fatalf("nested parents: %v", tree.parents)
	}
	if tree.parents["go.sum"] != "" || tree.parents["other/main_test.go"] != "other" || tree.parents[".npmrc"] != "" {
		t.Fatalf("nesting crossed directories or nested a folder: %v", tree.parents)
	}
	if tree.labels["apps/web/src/main_test.go"] != "main_test.go" || tree.labels["apps/web"] != "apps/web" {
		t.Fatalf("labels: %v", tree.labels)
	}
	for _, p := range files {
		if !tree.has(p) || tree.isDir(p) {
			t.Errorf("file %q lost its identity", p)
		}
	}
	got := tree.files()
	slices.Sort(got)
	slices.Sort(files)
	if !slices.Equal(got, files) {
		t.Fatalf("enumeration lost or duplicated files: %q, want %q", got, files)
	}
	tree.show("apps/web/src/main_test.go")
	if !tree.state.Open.Has("apps/web/src/main.go") || tree.row < 0 {
		t.Fatal("revealing a nested file did not open its parent")
	}

	changes := newPathTree()
	changes.set(foldGrouped([]string{"a.ts", "a.js", "a.js.map"}))
	if changes.nested != nil || changes.parents["a.js"] == "a.ts" {
		t.Fatal("Changes must not nest files")
	}
}

func TestNestedFilterAndReplace(t *testing.T) {
	tree := newPathTree()
	files := []string{"src/main.go", "src/main_test.go", "go.mod", "go.sum"}
	tree.set(foldFiles(files, true))
	tt := ui.NewTester(func(c *ui.Context) {
		tree.applyFilter()
		tree.view(c, "Files", func(p string) ui.SourceListItem { return ui.SourceListItem{Title: tree.labels[p]} })
		tree.sync()
	}, 400, 400)
	tree.show("go.mod")
	tt.Frame()
	tree.filter = "main_test"
	tt.Frame()
	if got := tree.rows(); !slices.Equal(got, []string{"src", "src/main.go", "src/main_test.go"}) {
		t.Fatalf("filtered rows %q", got)
	}
	if kept, all := tree.filterCount(); kept != 1 || all != 4 {
		t.Fatalf("filter count %d/%d, want 1/4", kept, all)
	}
	tree.filter = "go.mod"
	tt.Frame()
	if got := tree.rows(); !slices.Equal(got, []string{"go.mod"}) {
		t.Fatalf("parent did not match independently: %q", got)
	}
	tree.filter = ""
	tt.Frame()
	if tree.state.Open.Has("src/main.go") || tree.state.Open.Has("src") {
		t.Fatal("filter did not restore closed nesting")
	}
	tree.show("src/main_test.go")
	tt.Frame()
	tree.replace(foldFiles([]string{"src/main_test.go", "go.mod", "go.sum"}, true))
	tt.Frame()
	if tree.chosen() != "src/main_test.go" || tree.parents["src/main_test.go"] != "src" {
		t.Fatal("removing the parent lost the selected child")
	}
	tree.replace(foldFiles(files, true))
	tt.Frame()
	if tree.chosen() != "src/main_test.go" || !tree.state.Open.Has("src/main.go") {
		t.Fatal("restoring the parent hid the selected child")
	}
	tt.Frame()
}

func TestNestedFilesOpenAndRefresh(t *testing.T) {
	repo := testutil.Repo(t)
	for name, text := range map[string]string{"go.mod": "module example\n", "go.sum": "sum contents\n"} {
		if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	a, tt := testApp(t, repo, func(a *app) { a.mode = modeFiles })
	tree := &a.allTree
	if tree.parents["go.sum"] != "go.mod" || tree.state.Open.Has("go.mod") {
		t.Fatal("Files did not start with go.sum nested and collapsed")
	}
	if err := tt.Click("go.mod"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.openFile != "go.mod" || a.code == nil || a.code.Buffer.String() != "module example\n" {
		t.Fatal("selecting the parent did not open its contents")
	}
	tree.show("go.sum")
	tt.Frame()
	if a.openFile != "go.sum" || a.code == nil || a.code.Buffer.String() != "sum contents\n" {
		t.Fatal("revealing the child did not open its contents")
	}
	if err := os.Remove(filepath.Join(repo, "go.mod")); err != nil {
		t.Fatal(err)
	}
	a.reloadFiles()
	tt.Frame()
	if a.openFile != "go.sum" || tree.chosen() != "go.sum" || tree.parents["go.sum"] != "" {
		t.Fatal("refresh lost the child when its parent disappeared")
	}
	if err := os.WriteFile(filepath.Join(repo, "go.mod"), []byte("module example\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.reloadFiles()
	tt.Frame()
	if a.openFile != "go.sum" || tree.chosen() != "go.sum" || !tree.state.Open.Has("go.mod") {
		t.Fatal("refresh did not reveal the child under its restored parent")
	}
	tt.Frame()
}
