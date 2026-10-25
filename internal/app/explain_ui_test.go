package app

import (
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"testing"
)

func TestExplainUI(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, `{"explanation":"It **adds** the docs."}`)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Explain"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.explain.running || a.explain.scope != "Uncommitted changes" || !tt.HasText("Explanation") {
		t.Fatalf("explanation %+v, texts %q", a.explain, tt.Texts())
	}
	if err := tt.Click("Explanation"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("adds") {
		t.Fatalf("the sheet does not show the explanation, texts %q", tt.Texts())
	}
	if err := tt.Click("Done"); err != nil {
		t.Fatal(err)
	}
	// The changes changed, it is out of date.
	if err := os.WriteFile(filepath.Join(repo, "docs/new.md"), []byte("new content\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.refresh()
	tt.Frame()
	if !tt.HasText("Explain Again") {
		t.Fatalf("a stale explanation shows, texts %q", tt.Texts())
	}
}
