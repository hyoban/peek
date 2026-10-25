package app

import (
	"peek/internal/review"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// askTemplate is a request to Codex a new conversation's field offers,
// to send as it is or edit: review ones have Codex review the changes,
// its findings deciding whether they pass, and the others answer in
// Markdown.
type askTemplate struct {
	name, request string
	review        bool
}

var askTemplates = []askTemplate{
	{name: "Explain", request: "Explain what these changes do and why, and how their parts fit together."},
	{name: "Review", request: "Review these changes for defects: correctness, regressions and security.", review: true},
	{name: "Summarize", request: "Summarize these changes in a few bullet points, for a pull request's description."},
	{name: "Suggest Tests", request: "Suggest the tests these changes need, and where in the repository they belong."},
}

// askState is the field's under a conversation: the template the
// request came from, -1 for none, and that it is to take the keys.
type askState struct {
	template int
	focus    bool
}

// codexResult tells which of Codex's results came last.
type codexResult int

const (
	codexNone codexResult = iota
	codexAnswer
	codexReview
)

// codexRunning reports whether Codex works on a request, of which there
// is one at a time.
func (a *app) codexRunning() bool { return a.answer.running || a.review.running }

// askTemplateOf is the template the request is of, nil for none.
func (a *app) askTemplateOf() *askTemplate {
	if i := a.ask.template; i >= 0 && i < len(askTemplates) {
		return &askTemplates[i]
	}
	return nil
}

// askScope is what a new conversation's request would be of: the
// changes a review is of, as the history's selection has them, or the
// entry shown.
func (a *app) askScope() (id, scope string) {
	if tp := a.askTemplateOf(); tp != nil && tp.review {
		target, err := a.reviewTarget()
		if err != "" {
			return "", err
		}
		return target.ID, target.Scope
	}
	_, id, scope = a.answerable()
	return id, scope
}

// newDraft shows a new conversation, as Xcode's New Conversation does:
// empty, in the list of conversations, its field taking the keys. A new
// one not started yet shows again rather than another.
func (a *app) newDraft() {
	conv := a.draft()
	if conv == nil {
		conv = &conversation{ID: newConversationID(), Title: "New Conversation", Created: time.Now()}
		a.conversations = append([]*conversation{conv}, a.conversations...)
		a.reply, a.ask.template = "", -1
	}
	a.showConversation(conv)
	a.ask.focus = true
}

// draft is the new conversation not started yet, nil for none.
func (a *app) draft() *conversation {
	for _, conv := range a.conversations {
		if len(conv.Turns) == 0 {
			return conv
		}
	}
	return nil
}

// chooseTemplate puts the template's request in the field, to send or
// edit.
func (a *app) chooseTemplate(i int) {
	a.ask.template, a.reply, a.ask.focus = i, askTemplates[i].request, true
}

// canSend reports whether the field's request can go to Codex now, in
// the conversation.
func (a *app) canSend(conv *conversation) bool {
	if strings.TrimSpace(a.reply) == "" || a.codexRunning() || a.changing() {
		return false
	}
	if len(conv.Turns) == 0 {
		id, _ := a.askScope()
		return id != ""
	}
	return true
}

// send sends the field's request to Codex: one starting the conversation
// as a review where its template is one, else a question about the
// changes, and one following up in it otherwise. The field empties for
// the next.
func (a *app) send(conv *conversation) {
	if !a.canSend(conv) {
		return
	}
	text := strings.TrimSpace(a.reply)
	tp := a.askTemplateOf()
	a.reply, a.ask.template = "", -1
	switch {
	case len(conv.Turns) > 0:
		a.followUp(conv, text)
	case tp != nil && tp.review:
		request := text
		if request == tp.request {
			request = "" // the review as it is
		}
		a.startReview(request)
	case tp != nil:
		a.startAnswer(tp.name, text)
	default:
		a.startAnswer("", text)
	}
}

// codexTool is the toolbar's button of Codex: the request under way,
// which it stops, or New Conversation, titled with the verdict of the
// review where it came last.
func (a *app) codexTool(c *ui.Context) toolButton {
	if a.review.running {
		return a.review.activity(c, "Reviewing…", "Codex is reviewing the changes", "Stop Review")
	}
	if a.answer.running {
		return a.answer.activity(c, "Thinking…", "Codex is answering", "Stop Codex")
	}
	b := toolButton{tooltip: "New conversation with Codex", click: a.newDraft}
	// A review of other changes than those selected waits for them, as
	// the branch's while the work tree shows.
	target, _ := a.reviewTarget()
	other := (a.review.scope == review.BranchScope) != (target.Scope == review.BranchScope)
	if a.lastCodex == codexReview && !a.review.stale && !other && (a.review.result != nil || a.review.err != "") {
		b.label = map[string]string{"Review passed": "Passed", "Review not passed": "Not Passed", "Review error": "Review Error"}[a.review.title()]
		b.tooltip = a.review.title() + " — " + a.review.scope + "\n" + b.tooltip
	}
	return b
}

// codexButton is the toolbar's button of Codex in the view, where
// AppKit's toolbar does not show.
func (a *app) codexButton(c *ui.Context) {
	b := a.codexTool(c)
	if b.busy {
		drawTool(c, "codex", b)
		return
	}
	label := b.label
	if label == "" {
		label = "Ask Codex"
	}
	ui.Button(c.Key("codex"), label).Shrink(0).Label("Ask Codex").Tooltip(b.tooltip).OnClick(b.click)
}
