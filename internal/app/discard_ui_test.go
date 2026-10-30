package app

import (
	"os"
	"path/filepath"
	"testing"

	"peek/internal/testutil"
)

// A file's Discard Changes… asks first, then brings it back to HEAD's,
// leaving the other changes as they are; Cancel leaves it changed.
func TestDiscardFile(t *testing.T) {
	repo := testutil.Repo(t)
	a, tt := testApp(t, repo, nil)
	settle(tt)
	n := len(a.diffs)
	discard := func(button string) {
		t.Helper()
		if err := tt.RightClick("main.go"); err != nil {
			t.Fatal(err)
		}
		if err := tt.ChooseMenuItem("Discard Changes…"); err != nil {
			t.Fatal(err)
		}
		settle(tt)
		if !tt.HasText("Discard changes to “main.go”?") {
			t.Fatalf("no alert asking, texts %q", tt.Texts())
		}
		if err := tt.Click(button); err != nil {
			t.Fatal(err)
		}
		settle(tt)
	}
	discard("Cancel")
	if b, _ := os.ReadFile(filepath.Join(repo, "main.go")); string(b) == "package main\n" {
		t.Error("main.go discarded on Cancel")
	}
	discard("Discard Changes")
	if b, _ := os.ReadFile(filepath.Join(repo, "main.go")); string(b) != "package main\n" {
		t.Errorf("main.go %q, want HEAD's", b)
	}
	if len(a.diffs) != n-1 {
		t.Errorf("%d diffs after discarding one of %d", len(a.diffs), n)
	}
}
