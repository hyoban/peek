package app

import (
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// askCodex sends the template's request to Codex through the
// conversation popover, as the user does.
func askCodex(t *testing.T, tt *ui.Tester, template string) {
	t.Helper()
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Templates"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem(template); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Send"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
}

func TestAnswerUI(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, `{"answer":"It **adds** the docs."}`)
	prompt := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", prompt)
	a, tt := testApp(t, repo, nil)
	askCodex(t, tt, "Explain")
	if a.answer.running || a.answer.scope != "Uncommitted changes" || a.answer.title != "Explain" || a.ask.open {
		t.Fatalf("answer %+v, texts %q", a.answer, tt.Texts())
	}
	if got, _ := os.ReadFile(prompt); !strings.HasPrefix(string(got), askTemplates[0].request+"\n") {
		t.Fatalf("prompt %q, want the template's request", got)
	}
	// The popover shows the answer again.
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Explain") || !tt.HasText("Uncommitted changes") {
		t.Fatalf("the popover does not offer the answer, texts %q", tt.Texts())
	}
	if err := tt.Click("Show"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("adds") || a.ask.open {
		t.Fatalf("the sheet does not show the answer, texts %q", tt.Texts())
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
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if !tt.HasText("Out of date") {
		t.Fatalf("a stale answer shows, texts %q", tt.Texts())
	}
}

// A request of the user's own goes as it is, Return sending it.
func TestAskOwnRequest(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, `{"answer":"Two files."}`)
	prompt := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", prompt)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Request"); !ok {
		t.Fatalf("no request to write, texts %q", tt.Texts())
	}
	tt.Type("Which files change?")
	tt.Key(ui.Shift, ui.KeyEnter) // breaks the line
	tt.Type("Briefly.")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if a.answer.title != "Answer" || a.answer.text != "Two files." || a.ask.open || a.ask.text != "" {
		t.Fatalf("answer %+v, ask %+v", a.answer, a.ask)
	}
	if got, _ := os.ReadFile(prompt); !strings.HasPrefix(string(got), "Which files change?\nBriefly.\n") {
		t.Fatalf("prompt %q, want the user's request", got)
	}
}
