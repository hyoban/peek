package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
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
	hover  func(text string, offset, verbosity int) definition.Hover
	closed bool
}

func (f *fakeDefinitions) Hover(_ context.Context, _, text string, offset, verbosity int) (definition.Hover, error) {
	if f.hover == nil {
		return definition.Hover{}, nil
	}
	return f.hover(text, offset, verbosity), nil
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

func TestDefinitionPointerFeedback(t *testing.T) {
	a, _, _ := navigationApp(t)
	a.code = codeOf(codeBuffer("main.go", "target()"), defaultSettings)
	a.navigation.client = &fakeDefinitions{lookup: func(string, string, int) ([]definition.Location, error) {
		t.Fatal("hover should not query the language server")
		return nil, nil
	}}
	probe := ui.NewTester(func(c *ui.Context) {
		a.configureNavigation()
		editor.View(c, a.code).Fill()
	}, 600, 300)
	probe.Move(80, 8)
	probe.HoldModifiers(ui.Cmd)
	if probe.Cursor() != ui.CursorPointer {
		t.Fatal("working-tree identifier did not offer definition lookup")
	}
	probe.HoldModifiers(0)
	if probe.Cursor() != ui.CursorText {
		t.Fatal("releasing Cmd kept the pointing hand")
	}
	probe.HoldModifiers(ui.Cmd)
	a.commits[a.loadedRow].Hash = "historical"
	probe.Frame()
	if probe.Cursor() == ui.CursorPointer {
		t.Fatal("historical file offered definition lookup")
	}
}

func TestKotlinDefinitionNavigation(t *testing.T) {
	for _, name := range []string{"Main.kt", "script.kts"} {
		t.Run(name, func(t *testing.T) {
			a, tt, _ := navigationApp(t)
			text := "fun target() {}\nfun main() { target() }\n"
			if err := os.WriteFile(filepath.Join(a.repo, name), []byte(text), 0600); err != nil {
				t.Fatal(err)
			}
			a.reloadFiles()
			settle(tt)
			a.openInFiles(git.Worktree, name)
			settle(tt)
			calls := 0
			a.navigation.client = &fakeDefinitions{lookup: func(path, source string, at int) ([]definition.Location, error) {
				calls++
				if path != filepath.Join(a.repo, name) || source != text || at != strings.LastIndex(text, "target") {
					t.Fatalf("lookup %s:%d: %q", path, at, source)
				}
				return []definition.Location{{Path: path, Range: definition.Range{Start: definition.Position{Character: 4}, End: definition.Position{Character: 10}}}}, nil
			}}
			a.code.Select(strings.LastIndex(text, "target"), strings.LastIndex(text, "target"))
			a.navigation.focus = true
			tt.Frame()
			tt.Key(0, ui.KeyF12)
			settle(tt)
			if calls != 1 || a.code.SelectedText() != "target" || a.code.Cursor() > 10 {
				t.Fatalf("definition: %d calls, cursor %d, selection %q", calls, a.code.Cursor(), a.code.SelectedText())
			}
			a.commits[a.loadedRow].Hash = "historical"
			a.configureNavigation()
			if a.code.OnDefinition != nil || a.canDefine() {
				t.Fatal("Kotlin definition enabled in history")
			}
		})
	}
}

func TestDefinitionStatusKeepsEditorPosition(t *testing.T) {
	a, tt, _ := navigationApp(t)
	before, ok := tt.Find("Code editor")
	if !ok {
		t.Fatal("no editor")
	}
	for _, note := range []string{"Finding definition…", "Opening definition…", "No definition found", "Choose a definition", ""} {
		a.navigation.note = note
		a.navigation.busy = strings.HasSuffix(note, "…")
		a.navigation.targets = nil
		if note == "Choose a definition" {
			a.navigation.targets = []definition.Location{{Path: filepath.Join(a.repo, "main.go")}}
		}
		settle(tt)
		after, ok := tt.Find("Code editor")
		if !ok || after != before {
			t.Fatalf("%q moved editor from %+v to %+v", note, before, after)
		}
	}
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

func TestHoverShowsType(t *testing.T) {
	a, tt, text := navigationApp(t)
	at := strings.LastIndex(text, "target")
	var asked []int
	a.navigation.client = &fakeDefinitions{hover: func(_ string, offset, _ int) definition.Hover {
		asked = append(asked, offset)
		return definition.Hover{Markdown: "```go\nfunc target()\n```\n\nTarget does nothing."}
	}}
	tt.Frame()
	if a.code.OnHover == nil {
		t.Fatal("the working tree's Go file offers no hovers")
	}
	a.code.OnHover(editor.HoverRequest{Offset: at + 2, Version: a.code.Buffer.Version()})
	tt.Frame()
	if len(asked) != 1 || asked[0] != at+2 {
		t.Fatalf("asked %v", asked)
	}
	if !tt.HasText("func target()") || !tt.HasText("Target does nothing.") {
		t.Errorf("the hover does not show, texts %q", tt.Texts())
	}
	if start, end := wordAt(text, at+2); text[start:end] != "target" {
		t.Errorf("word %q", text[start:end])
	}
}

func TestHoverLinks(t *testing.T) {
	a, tt, _ := navigationApp(t)
	backend := filepath.Join(a.repo, "backend.py")
	if err := os.WriteFile(backend, []byte("a\nb\nc\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var opened []string
	a.openURL = func(u string) { opened = append(opened, u) }
	a.hoverLink("https://example.com/docs")
	a.hoverLink("ftp://example.com/x")
	if len(opened) != 1 || opened[0] != "https://example.com/docs" {
		t.Fatalf("opened %q", opened)
	}
	// A content mapper links the backend implementation of an operation.
	a.hoverLink("file://" + backend + "#L3")
	tt.Frame()
	if filepath.Base(a.openFile) != "backend.py" || a.code.Cursor() != len("a\nb\n") {
		t.Fatalf("opened %q at %d, note %q", a.openFile, a.code.Cursor(), a.navigation.note)
	}
}

func TestHoverExpandsTypes(t *testing.T) {
	a, tt, text := navigationApp(t)
	var levels []int
	a.navigation.client = &fakeDefinitions{hover: func(_ string, _, verbosity int) definition.Hover {
		levels = append(levels, verbosity)
		if verbosity == 0 {
			return definition.Hover{Markdown: "```go\nvar x Outer\n```", Expandable: true}
		}
		return definition.Hover{Markdown: "```go\nvar x struct{ inner Inner }\n```"}
	}}
	tt.Frame()
	a.code.OnHover(editor.HoverRequest{Offset: strings.LastIndex(text, "target"), Version: a.code.Buffer.Version()})
	tt.Frame()
	if !tt.HasText("var x Outer") {
		t.Fatalf("texts %q", tt.Texts())
	}
	if err := tt.Click("Expand Type"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("var x struct{ inner Inner }") || !reflect.DeepEqual(levels, []int{0, 1}) {
		t.Fatalf("levels %v, texts %q", levels, tt.Texts())
	}
	if err := tt.Click("Collapse Type"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !tt.HasText("var x Outer") || !reflect.DeepEqual(levels, []int{0, 1, 0}) {
		t.Fatalf("levels %v, texts %q", levels, tt.Texts())
	}
}
