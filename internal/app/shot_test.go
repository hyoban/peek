package app

import (
	"strings"
	"testing"

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
	a, tt := testApp(t, repo, func(a *app) { a.shot = shotFromEnv() })
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
}

func TestNoShotWithoutEnv(t *testing.T) {
	t.Setenv("PEEK_SHOT_COMMIT", "")
	if s := shotFromEnv(); s != nil {
		t.Errorf("a shot without PEEK_SHOT_COMMIT: %+v", *s)
	}
}
