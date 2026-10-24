package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// The window shows one project at a time, each kept as the user left it.
func TestProjectsSwitch(t *testing.T) {
	one, two := testRepo(t), testRepo(t)
	p := &projects{apps: map[string]*app{}, inline: true}
	tt := ui.NewTester(p.view, 1200, 800)
	if !tt.HasText("No Repository") {
		t.Fatalf("empty window not shown, texts %q", tt.Texts())
	}
	p.open(one)
	settle(tt)
	first := p.cur
	first.setMode(modeFiles)
	first.showTree = false
	p.open(two)
	settle(tt)
	if p.cur == first || p.cur.mode != modeChanges {
		t.Fatal("second project not opened anew")
	}
	if p.cur.showTree {
		t.Error("columns not kept from the project before")
	}
	p.open(one)
	settle(tt)
	if p.cur != first || p.cur.mode != modeFiles {
		t.Error("first project not shown as left")
	}
	if len(p.recent) != 2 || p.recent[0] != one || p.recent[1] != two {
		t.Errorf("recent %q, want the latest first", p.recent)
	}
	if !tt.HasText("Switch Project") && !tt.HasText(p.label(one)) {
		t.Errorf("project title missing, texts %q", tt.Texts())
	}
}
