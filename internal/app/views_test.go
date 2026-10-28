package app

import (
	"testing"

	"peek/internal/definition"
	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// A repository opens again as its window last closed: in its mode, at
// its file, the caret where it was, with the window's columns.
func TestViewsRestore(t *testing.T) {
	repo, dir := testutil.Repo(t), t.TempDir()
	p := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	tt := ui.NewTester(p.view, 1200, 800)
	p.open(repo)
	settle(tt)
	a := p.cur
	a.setMode(modeFiles)
	settle(tt)
	a.allTree.show("main.go")
	settle(tt)
	if a.code == nil || a.codePath != "main.go" {
		t.Fatalf("main.go not open, %q", a.codePath)
	}
	a.code.Select(16, 16) // the third line
	a.showTree, a.historyWidth = false, 300
	p.keepViews()

	again := &projects{apps: map[string]*app{}, inline: true, dir: dir, views: readViews(dir)}
	tt = ui.NewTester(again.view, 1200, 800)
	again.open(repo)
	settle(tt)
	b := again.cur
	if b.mode != modeFiles || b.codePath != "main.go" || b.code == nil {
		t.Fatalf("mode %d, file %q, want main.go in the files mode", b.mode, b.codePath)
	}
	if at := definition.PositionAt(b.code.Buffer.String(), b.code.Selection().Head); at.Line != 2 {
		t.Errorf("caret at %+v, want the third line", at)
	}
	if b.showTree || b.historyWidth != 300 {
		t.Errorf("columns not kept: tree %v, history %v", b.showTree, b.historyWidth)
	}
	if b.restoring != nil {
		t.Error("still restoring once shown")
	}
}

// The changes mode shows the file at the top of the diffs again.
func TestViewsRestoreChanges(t *testing.T) {
	repo, dir := testutil.Repo(t), t.TempDir()
	p := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	tt := ui.NewTester(p.view, 1200, 800)
	p.open(repo)
	settle(tt)
	a := p.cur
	first := a.file
	var other string
	for path := range a.changes {
		if path != first {
			other = path
		}
	}
	a.changeTree.show(other)
	settle(tt)
	if a.file != other {
		t.Fatalf("file %q, want %q", a.file, other)
	}
	p.keepViews()

	again := &projects{apps: map[string]*app{}, inline: true, dir: dir, views: readViews(dir)}
	tt = ui.NewTester(again.view, 1200, 800)
	again.open(repo)
	settle(tt)
	if b := again.cur; b.mode != modeChanges || b.file != other {
		t.Errorf("mode %d, file %q, want %q in the changes mode", b.mode, b.file, other)
	}
}
