package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peek/internal/definition"
	"peek/internal/git"
	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type fakeDefinitions struct {
	lookup func(string, string, int) ([]definition.Location, error)
	closed bool
}

func (f *fakeDefinitions) Definition(_ context.Context, p, text string, offset int) ([]definition.Location, error) {
	return f.lookup(p, text, offset)
}
func (f *fakeDefinitions) Close() { f.closed = true }

func navigationApp(t *testing.T) (*app, *ui.Tester, string) {
	t.Helper()
	repo := testutil.Repo(t)
	text := "package main\n\nfunc target() {}\n\nfunc main() { target() }\n"
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	a, tt := testApp(t, repo, nil)
	a.setMode(modeFiles)
	tt.Frame()
	a.openInFiles(git.Worktree, "main.go")
	tt.Frame()
	t.Cleanup(a.close)
	return a, tt, text
}

func TestDefinitionNavigation(t *testing.T) {
	a, tt, text := navigationApp(t)
	// A dependency outside the tree remains open across frames and refreshes.
	external := filepath.Join(t.TempDir(), "library.go")
	body := "package library\r\n\r\n// 中文 😀\r\nfunc Target() {}\r\n"
	if err := os.WriteFile(external, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	offset := strings.Index(body, "Target")
	fake := &fakeDefinitions{lookup: func(path, source string, at int) ([]definition.Location, error) {
		if path != filepath.Join(a.repo, "main.go") || source != text || at != strings.LastIndex(text, "target") {
			t.Errorf("lookup %s at %d: %q", path, at, source)
		}
		return []definition.Location{{Path: external, Range: definition.Range{Start: definition.PositionAt(body, offset), End: definition.PositionAt(body, offset+6)}}}, nil
	}}
	a.navigation.client = fake
	a.code.Select(strings.LastIndex(text, "target"), strings.LastIndex(text, "target"))
	a.navigation.focus = true
	tt.Frame()
	tt.Key(0, ui.KeyF12)
	settle(tt)
	if a.openFile != external || a.code.SelectedText() != "Target" || !a.code.ReadOnly {
		t.Fatalf("target %q: %q", a.openFile, a.code.SelectedText())
	}
	if a.allTree.chosen() != "" {
		t.Fatalf("external file selected %s", a.allTree.chosen())
	}
	if err := tt.Click("Go Back"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.openFile != "main.go" || a.code.Cursor() != strings.LastIndex(text, "target") {
		t.Fatalf("back to %s:%d", a.openFile, a.code.Cursor())
	}
	if err := tt.Click("Go Forward"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.openFile != external {
		t.Fatalf("forward to %s", a.openFile)
	}
	a.reloadFiles()
	settle(tt)
	if a.openFile != external || a.code.Buffer.String() != strings.ReplaceAll(body, "\r\n", "\n") {
		t.Fatal("refresh lost external source")
	}
	a.goBack(-1)
	settle(tt)
	// Ordinary clicks keep selecting text. Cmd-click asks the provider.
	calls := 0
	fake.lookup = func(_ string, _ string, _ int) ([]definition.Location, error) { calls++; return nil, nil }
	a.navigation.client = fake
	var bounds ui.Rect
	// Handle geometry is read during construction, only for this test's input.
	probe := ui.NewTester(func(c *ui.Context) {
		a.configureNavigation()
		e := editor.View(c, a.code).Fill().Bind(&a.navigation.editor)
		_ = e
		bounds = a.navigation.editor.Bounds(c)
	}, 600, 300)
	probe.Frame()
	probe.ClickAt(bounds.X+110, bounds.Y+8)
	if calls != 0 {
		t.Fatal("ordinary click queried a definition")
	}
	probe.Advance(time.Second) // the next click is not a double-click selection
	probe.ClickAtWith(ui.Cmd, bounds.X+110, bounds.Y+8)
	if calls != 1 {
		t.Fatalf("Cmd-click made %d requests", calls)
	}
}

func TestDefinitionChoicesAndFailures(t *testing.T) {
	a, tt, text := navigationApp(t)
	loc := definition.Location{Path: filepath.Join(a.repo, "main.go"), Range: definition.Range{Start: definition.Position{Line: 2, Character: 5}, End: definition.Position{Line: 2, Character: 11}}}
	fake := &fakeDefinitions{lookup: func(string, string, int) ([]definition.Location, error) {
		return []definition.Location{loc, {Path: filepath.Join(a.repo, "missing.go")}}, nil
	}}
	a.navigation.client = fake
	a.goDefinition(strings.LastIndex(text, "target"))
	settle(tt)
	if !tt.HasText("Choose a definition") {
		t.Fatalf("no chooser: %q", tt.Texts())
	}
	if err := tt.Click("main.go:3:6"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.code.SelectedText() != "target" {
		t.Fatalf("selected %q", a.code.SelectedText())
	}
	back := len(a.navigation.back)
	a.openDefinition(definition.Location{Path: filepath.Join(a.repo, "missing.go")})
	settle(tt)
	if a.openFile != "main.go" || len(a.navigation.back) != back || !strings.Contains(a.navigation.note, "Could not open") {
		t.Fatal("failed destination changed navigation")
	}
	fake.lookup = func(string, string, int) ([]definition.Location, error) {
		return nil, errors.New("gopls is not installed")
	}
	a.goDefinition(0)
	settle(tt)
	if !strings.Contains(a.navigation.note, "gopls is not installed") {
		t.Fatalf("missing-server message %q", a.navigation.note)
	}
	fake.lookup = func(string, string, int) ([]definition.Location, error) { return nil, nil }
	a.goDefinition(0)
	settle(tt)
	if !tt.HasText("No definition found") {
		t.Fatalf("no result message %q", a.navigation.note)
	}
}

func TestDefinitionStaleResults(t *testing.T) {
	for _, action := range []string{"file", "commit", "buffer", "close"} {
		t.Run(action, func(t *testing.T) {
			a, tt, _ := navigationApp(t)
			fake := &fakeDefinitions{lookup: func(string, string, int) ([]definition.Location, error) {
				return []definition.Location{{Path: filepath.Join(a.repo, "main.go"), Range: definition.Range{Start: definition.Position{Line: 2}}}}, nil
			}}
			a.navigation.client = fake
			// Hold the worker's publication until the UI has changed.
			a.building = true
			a.goDefinition(0)
			a.building = false
			pending := a.published
			a.published = nil
			switch action {
			case "file":
				a.openInFiles(git.Worktree, "docs/new.md")
			case "commit":
				a.commitRow = 1
			case "buffer":
				resetText(a.code.Buffer, "package main\n")
			case "close":
				a.close()
			}
			for _, publish := range pending {
				publish()
			}
			tt.Frame()
			if len(a.navigation.back) != 0 || a.navigation.busy {
				t.Fatal("stale result navigated")
			}
			if action == "commit" && a.code != nil && a.code.OnDefinition != nil {
				t.Fatal("history has a definition handler")
			}
		})
	}
}

func TestFileLoadsRejectOldRevision(t *testing.T) {
	a, tt, _ := navigationApp(t)
	oldHash := a.commits[1].Hash
	a.building = true
	a.loadText(oldHash, "main.go")
	a.building = false
	old := a.published
	a.published = nil
	a.loadText(git.Worktree, "main.go")
	for _, publish := range old {
		publish()
	}
	tt.Frame()
	if !strings.Contains(a.code.Buffer.String(), "target") || a.codeHash != git.Worktree {
		t.Fatal("old revision replaced the working tree")
	}
}
