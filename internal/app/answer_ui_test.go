package app

import (
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// askCodex sends the template's request to Codex in a new conversation,
// as the user does.
func askCodex(t *testing.T, tt *ui.Tester, template string) {
	t.Helper()
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Shortcuts"); err != nil {
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
	t.Setenv("PEEK_TEST_CODEX_THREAD", "thread-1")
	args := filepath.Join(t.TempDir(), "args")
	t.Setenv("PEEK_TEST_CODEX_ARGS", args)
	a, tt := testApp(t, repo, nil)
	askCodex(t, tt, "Explain")
	conv := a.shownConversation()
	if a.answer.running || a.reply != "" || conv == nil || conv.Title != "Explain" || conv.Scope != "Uncommitted changes" || conv.Session != "thread-1" {
		t.Fatalf("conversation %+v, texts %q", conv, tt.Texts())
	}
	if got, _ := os.ReadFile(prompt); !strings.HasPrefix(string(got), askTemplates[0].request+"\n") {
		t.Fatalf("prompt %q, want the template's request", got)
	}
	// It shows in place of the diffs, listed in the sidebar.
	if !tt.HasText("adds") || !tt.HasText(askTemplates[0].request) || !tt.HasText("Conversations") {
		t.Fatalf("the conversation does not show, texts %q", tt.Texts())
	}
	// A reply follows up in Codex's conversation.
	os.WriteFile(os.Getenv("PEEK_TEST_REVIEW_RESPONSE"), []byte(`{"answer":"Only docs."}`), 0o600)
	if err := tt.Click("Message"); err != nil {
		t.Fatal(err)
	}
	tt.Type("Anything else?")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	if len(conv.Turns) != 2 || conv.Turns[1].Answer != "Only docs." || !tt.HasText("Only docs.") || a.reply != "" {
		t.Fatalf("follow-up %+v, texts %q", conv.Turns, tt.Texts())
	}
	runs, _ := os.ReadFile(args)
	if lines := strings.Split(strings.TrimSpace(string(runs)), "\n"); len(lines) != 2 || !strings.Contains(lines[1], "resume") || !strings.Contains(lines[1], "thread-1") {
		t.Fatalf("codex ran with %q", lines)
	}
	if got, _ := os.ReadFile(prompt); !strings.HasPrefix(string(got), "Anything else?\n") || strings.Contains(string(got), "Scope:") {
		t.Fatalf("follow-up prompt %q", got)
	}
	// The files show again in their navigator.
	if err := tt.Click("File Navigator"); err != nil {
		t.Fatal(err)
	}
	if a.shownConversation() != nil || tt.HasText("Only docs.") {
		t.Fatalf("the conversation still shows, texts %q", tt.Texts())
	}
}

// A request of the user's own goes as it is, Return sending it, titled
// after it.
func TestAskOwnRequest(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, `{"answer":"Two files."}`)
	prompt := filepath.Join(t.TempDir(), "prompt")
	t.Setenv("PEEK_TEST_REVIEW_PROMPT", prompt)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Ask Codex"); err != nil {
		t.Fatal(err)
	}
	if _, ok := tt.Find("Request"); !ok || !tt.Focused("Request") {
		t.Fatalf("no request to write, texts %q", tt.Texts())
	}
	tt.Type("Which files change?")
	tt.Key(ui.Shift, ui.KeyEnter) // breaks the line
	tt.Type("Briefly.")
	tt.Key(0, ui.KeyEnter)
	tt.Frame()
	conv := a.shownConversation()
	if conv == nil || conv.Title != "Which files change?" || conv.Turns[0].Answer != "Two files." || a.ask.open || a.ask.text != "" {
		t.Fatalf("conversation %+v, popover %+v", conv, a.ask)
	}
	if got, _ := os.ReadFile(prompt); !strings.HasPrefix(string(got), "Which files change?\nBriefly.\n") {
		t.Fatalf("prompt %q, want the user's request", got)
	}
}

// Conversations are kept for the next launch, a request under way as the
// app quit stopped.
func TestConversationsKept(t *testing.T) {
	file := filepath.Join(t.TempDir(), "conversations.json")
	a := newApp("/tmp/repo")
	a.convFile = file
	conv := a.newConversation("Explain", "Uncommitted changes", "Explain it.")
	conv.Session = "thread-1"
	conv.Turns = append(conv.Turns, &turn{Request: "More?"})
	conv.Turns[0].Answer = "It does."
	a.saveConversations()
	b := newApp("/tmp/repo")
	b.convFile = file
	b.loadConversations()
	if len(b.conversations) != 1 || b.conversations[0].Session != "thread-1" || b.conversations[0].Turns[0].Answer != "It does." ||
		b.conversations[0].Turns[1].Err != "Stopped" {
		t.Fatalf("kept %+v", b.conversations)
	}
	b.deleteConversation(b.conversations[0])
	c := newApp("/tmp/repo")
	c.convFile = file
	c.loadConversations()
	if len(c.conversations) != 0 {
		t.Fatalf("deleted conversation kept: %+v", c.conversations)
	}
}

// New Conversation shows an empty one, the same until it starts, which is
// not kept.
func TestNewConversation(t *testing.T) {
	file := filepath.Join(t.TempDir(), "conversations.json")
	a := newApp("/tmp/repo")
	a.convFile = file
	a.newDraft()
	draft := a.shownConversation()
	a.newDraft()
	if draft == nil || a.shownConversation() != draft || len(a.conversations) != 1 || !a.compose.focus {
		t.Fatalf("conversations %+v", a.conversations)
	}
	a.saveConversations()
	b := newApp("/tmp/repo")
	b.convFile = file
	b.loadConversations()
	if len(b.conversations) != 0 {
		t.Fatalf("an empty conversation was kept: %+v", b.conversations)
	}
	// Started, it is the one New Conversation showed.
	if conv := a.newConversation("Explain", "Uncommitted changes", "Explain it."); conv != draft || len(a.conversations) != 1 {
		t.Fatalf("started %+v, conversations %+v", conv, a.conversations)
	}
}

// The sidebar's New Conversation shows a new one to write in, its
// templates in its own field.
func TestStartInConversation(t *testing.T) {
	repo := testutil.Repo(t)
	testutil.FakeReviewCodex(t, `{"answer":"A summary."}`)
	a, tt := testApp(t, repo, nil)
	if err := tt.Click("Conversation Navigator"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("New Conversation"); err != nil {
		t.Fatal(err)
	}
	draft := a.shownConversation()
	if draft == nil || len(draft.Turns) != 0 || a.ask.open || !tt.Focused("Message") {
		t.Fatalf("no new conversation to write in: %+v, texts %q", draft, tt.Texts())
	}
	if err := tt.Click("Shortcuts"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Summarize"); err != nil {
		t.Fatal(err)
	}
	if err := tt.Click("Send Message"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.shownConversation() != draft || draft.Title != "Summarize" || draft.Turns[0].Answer != "A summary." || a.reply != "" {
		t.Fatalf("conversation %+v", draft)
	}
}
