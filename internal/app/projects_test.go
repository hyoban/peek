package app

import (
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// The window shows one project at a time, each kept as the user left it.
func TestProjectsSwitch(t *testing.T) {
	one, two := testutil.Repo(t), testutil.Repo(t)
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

// Repositories added to the window show together: the sidebar lists the
// files of each under its name, a file chosen in another's shows that
// one, and the group is kept for the next launch.
func TestProjectsGroup(t *testing.T) {
	one, two := testutil.Repo(t), testutil.Repo(t)
	if err := os.WriteFile(filepath.Join(one, "first.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(two, "second.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	p := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	tt := ui.NewTester(p.view, 1400, 900)
	p.open(one)
	settle(tt)
	p.cur.setMode(modeFiles)
	p.add(two)
	settle(tt)
	if p.cur == nil || p.cur.repo != two {
		t.Fatal("added repository not shown")
	}
	if p.cur.mode != modeFiles {
		t.Error("mode not kept in the group")
	}
	for _, file := range []string{"first.go", "second.go"} {
		if !tt.HasText(file) {
			t.Errorf("Files lacks %q, texts %q", file, tt.Texts())
		}
	}
	p.cur.setMode(modeChanges)
	settle(tt)
	for _, text := range []string{p.label(one), p.label(two), "first.go", "second.go"} {
		if !tt.HasText(text) {
			t.Errorf("sidebar lacks %q, texts %q", text, tt.Texts())
		}
	}
	if err := tt.Click("first.go"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if p.cur.repo != one || p.cur.file != "first.go" {
		t.Errorf("chose first.go, shows %s's %q", p.cur.repo, p.cur.file)
	}
	if !tt.HasText("second.go") {
		t.Error("the other repository left the sidebar")
	}

	// Kept for the next launch.
	time.Sleep(100 * time.Millisecond) // writeList writes in the background
	again := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	if g := again.readGroups(); len(g) != 1 || !slices.Equal(g[0], []string{one, two}) {
		t.Errorf("groups kept %q", g)
	}

	// A repository taken out shows alone, the other in its place.
	p.remove(one)
	settle(tt)
	if p.cur.repo != two || p.cur.together() != nil || len(p.groups) != 0 {
		t.Errorf("removed: shows %s, groups %q", p.cur.repo, p.groups)
	}
	if tt.HasText("first.go") {
		t.Error("removed repository still in the sidebar")
	}
}
