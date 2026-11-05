package app

import (
	"strings"
	"testing"
	"time"

	"peek/internal/testutil"
)

func TestShotOpensCommitFileAndMode(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.Git(t, repo, "add", ".")
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Second")
	first := strings.TrimSpace(testutil.Git(t, repo, "rev-parse", "HEAD~1"))
	t.Setenv("PEEK_SHOT_COMMIT", first[:7])
	t.Setenv("PEEK_SHOT_FILE", "main.go")
	t.Setenv("PEEK_SHOT_MODE", "files")
	t.Setenv("PEEK_SHOT_TERMINAL", "echo peek-$((40+2))")
	a, tt := testApp(t, repo, func(a *app) { a.shot = shotFromEnv() })
	defer a.closeTerminal()
	for range 20 {
		if a.shot == nil {
			break
		}
		settle(tt)
	}
	if a.shot != nil {
		t.Fatalf("the shot did not finish: %+v", *a.shot)
	}
	if cm, ok := a.current(); !ok || cm.Hash != first {
		t.Errorf("the commit shown is %+v, want %s", cm, first)
	}
	if a.mode != modeFiles || a.openFile != "main.go" {
		t.Errorf("mode %d, file %q, want the files mode with main.go", a.mode, a.openFile)
	}
	// The terminal shows, running the command.
	term := a.terminal.term
	if !a.terminal.shown || term == nil {
		t.Fatalf("the terminal is not shown: %s", a.terminal.err)
	}
	for deadline := time.Now().Add(10 * time.Second); !strings.Contains(term.Text(), "peek-42"); {
		if time.Now().After(deadline) {
			t.Fatalf("the command did not run, the terminal shows %q", term.Text())
		}
		time.Sleep(20 * time.Millisecond)
		tt.Frame()
	}
	// A shot of Changes after puts back the mode and hides the terminal,
	// as the project's saved view would show them.
	a.shot = &shot{commit: first[:7], row: -1}
	for range 20 {
		if a.shot == nil {
			break
		}
		settle(tt)
	}
	if a.mode != modeChanges || a.terminal.shown {
		t.Errorf("mode %d, terminal shown %v, want Changes without the terminal", a.mode, a.terminal.shown)
	}
}

func TestNoShotWithoutEnv(t *testing.T) {
	t.Setenv("PEEK_SHOT_COMMIT", "")
	if s := shotFromEnv(); s != nil {
		t.Errorf("a shot without PEEK_SHOT_COMMIT: %+v", *s)
	}
}
