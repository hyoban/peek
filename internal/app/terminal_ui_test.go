package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

func TestParsePlace(t *testing.T) {
	for link, want := range map[string]terminalPlace{
		"main.go":                 {path: "main.go"},
		"src/app.go:12":           {path: "src/app.go", line: 12},
		"src/app.go:12:3":         {path: "src/app.go", line: 12, col: 3},
		"src/app.go:12:3:":        {path: "src/app.go", line: 12, col: 3},
		"/abs/a b.txt:7":          {path: "/abs/a b.txt", line: 7},
		"internal/app/app.go:abc": {path: "internal/app/app.go:abc"},
	} {
		if got := parsePlace(link); got != want {
			t.Errorf("parsePlace(%q) = %+v, want %+v", link, got, want)
		}
	}
}

// The terminal runs the user's shell in the repository, under the text,
// once it shows.
func TestTerminalShell(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	defer a.closeTerminal()
	settle(tt)
	if a.terminal.term != nil {
		t.Fatal("terminal started before it shows")
	}
	// The toolbar's button shows it, as ⌘J does.
	if err := tt.Click("Show Terminal"); err != nil {
		t.Fatal(err)
	}
	settle(tt) // as it slides in
	term := a.terminal.term
	if term == nil {
		t.Fatalf("terminal not started: %s", a.terminal.err)
	}
	tt.Type("pwd; echo peek-$((40+2))")
	tt.Key(0, ui.KeyEnter)
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(term.Text(), "peek-42"); {
		if time.Now().After(deadline) {
			t.Fatalf("no output, terminal shows %q", term.Text())
		}
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	real, _ := filepath.EvalSymlinks(repo)
	if text := term.Text(); !strings.Contains(text, repo) && !strings.Contains(text, real) {
		t.Errorf("shell not in the repository %s, terminal shows %q", repo, text)
	}
	if err := tt.Click("Hide Terminal"); err != nil {
		t.Fatal(err)
	}
	settle(tt)
	if a.terminal.term != term {
		t.Error("hiding the terminal ended its shell")
	}
}

// A file the terminal printed opens in the work tree's files, at its line.
func TestTerminalLinkOpensFile(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	if !a.openTerminalLink(nil, filepath.Join(repo, "main.go")+":3:6") {
		t.Fatal("link to main.go not opened")
	}
	settle(tt)
	if a.mode != modeFiles || a.openFile != "main.go" || a.code == nil {
		t.Fatalf("mode %d, open file %q, code %v; want main.go in the files", a.mode, a.openFile, a.code != nil)
	}
	if pos := a.code.CursorPos(); pos.Line != 2 || pos.Col != 5 {
		t.Errorf("caret at %+v, want line 2, column 5", pos)
	}
	if a.openTerminalLink(nil, filepath.Join(repo, "missing.go")) {
		t.Error("a missing file opened")
	}
}
