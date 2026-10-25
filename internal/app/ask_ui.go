package app

import (
	"peek/internal/review"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// askTemplate is a request to Codex the fields starting a conversation
// offer, to send as it is or edit: review ones have Codex review the
// changes, its findings deciding whether they pass, and the others answer
// in Markdown.
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

// askState is a field's starting a conversation: the template its
// request came from, -1 for none, and that it is to take the keys. The
// popover's, under the toolbar's button, as Xcode's, to ask at once, has
// its request, and is open or not; a new conversation's field writes in
// app.reply.
type askState struct {
	template int
	focus    bool
	open     bool
	text     string
	field    ui.Handle
	button   ui.Handle // the toolbar's button, under which the popover shows
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

// templateOf is the template of a field's request, nil for none.
func templateOf(s *askState) *askTemplate {
	if i := s.template; i >= 0 && i < len(askTemplates) {
		return &askTemplates[i]
	}
	return nil
}

// askScope is what a request starting a conversation would be of: the
// changes a review is of, as the history's selection has them, or the
// entry shown.
func (a *app) askScope(s *askState) (id, scope string) {
	if tp := templateOf(s); tp != nil && tp.review {
		target, err := a.reviewTarget()
		if err != "" {
			return "", err
		}
		return target.ID, target.Scope
	}
	_, id, scope = a.answerable()
	return id, scope
}

// canStart reports whether the request can start a conversation now.
func (a *app) canStart(s *askState, text string) bool {
	id, _ := a.askScope(s)
	return strings.TrimSpace(text) != "" && id != "" && !a.codexRunning() && !a.changing()
}

// start starts a conversation of the request, as a review where its
// template is one, else a question about the changes.
func (a *app) start(s *askState, text string) {
	text = strings.TrimSpace(text)
	switch tp := templateOf(s); {
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

// templatesButton is the button of the shortcuts to a request, its
// templates: a capsule of a bolt and its name, as a menu's button, the
// request of the one chosen going in the field, to send or edit.
func templatesButton(c *ui.Context, s *askState, text *string) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("shortcuts")).Shrink(0).Height(26).Padding(0, t.Space(2.5)).Radius(13).AlignItems(ui.Center).
		Gap(t.Space(1)).Label("Shortcuts").Tooltip("Shortcuts: requests to start from").TextColor(t.TextMuted).
		Background(t.Text.Alpha(0.06))
	b.Children(func() {
		ui.Icon(c, shortcutsIcon).Size(13, 13)
		ui.Text(c, "Shortcuts").FontSize(smallSize).SingleLine()
		ui.Icon(c, chevronIcon).Size(9, 9)
	})
	b.Menu(func(m *ui.Menu) {
		for i, tp := range askTemplates {
			if m.Item(tp.name).Checked(s.template == i).Chosen() {
				s.template, *text, s.focus = i, tp.request, true
			}
		}
	})
}

// openAsk shows the popover, its field taking the keys.
func (a *app) openAsk() {
	if a.codexRunning() {
		return
	}
	a.ask.open, a.ask.focus = true, true
	if a.ask.text == "" {
		a.ask.template = -1
	}
}

// sendAsk starts the conversation the popover asks, and closes it, the
// request cleared for the next.
func (a *app) sendAsk() {
	if !a.canStart(&a.ask, a.ask.text) {
		return
	}
	s, text := a.ask, a.ask.text
	a.ask = askState{template: -1, field: a.ask.field, button: a.ask.button}
	a.start(&s, text)
}

// newDraft shows a new conversation, as Xcode's New Conversation does:
// empty, in the list of conversations, its field taking the keys. A new
// one not started yet shows again rather than another.
func (a *app) newDraft() {
	conv := a.draft()
	if conv == nil {
		conv = &conversation{ID: newConversationID(), Title: "New Conversation", Created: time.Now()}
		a.conversations = append([]*conversation{conv}, a.conversations...)
		a.reply, a.compose.template = "", -1
	}
	a.showConversation(conv)
	a.compose.focus = true
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

// canSend reports whether the field's request can go to Codex now, in
// the conversation.
func (a *app) canSend(conv *conversation) bool {
	if len(conv.Turns) == 0 {
		return a.canStart(&a.compose, a.reply)
	}
	return strings.TrimSpace(a.reply) != "" && !a.codexRunning() && !a.changing()
}

// send sends the field's request to Codex: one starting the conversation,
// or following up in it. The field empties for the next.
func (a *app) send(conv *conversation) {
	if !a.canSend(conv) {
		return
	}
	s, text := a.compose, a.reply
	a.reply, a.compose.template = "", -1
	if len(conv.Turns) > 0 {
		a.followUp(conv, strings.TrimSpace(text))
		return
	}
	a.start(&s, text)
}

// codexTool is the toolbar's button of Codex: the request under way,
// which it stops, or the button opening the popover, titled with the
// verdict of the review where it came last.
func (a *app) codexTool(c *ui.Context) toolButton {
	if a.review.running {
		return a.review.activity(c, "Reviewing…", "Codex is reviewing the changes", "Stop Review")
	}
	if a.answer.running {
		return a.answer.activity(c, "Thinking…", "Codex is answering", "Stop Codex")
	}
	b := toolButton{tooltip: "New conversation with Codex", click: func() {
		if a.ask.open {
			a.ask.open = false
			return
		}
		a.openAsk()
	}}
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
	ui.Button(c.Key("codex"), label).Shrink(0).Label("Ask Codex").Tooltip(b.tooltip).Bind(&a.ask.button).OnClick(b.click)
}

// askWidth is the width of the popover's content.
const askWidth = 400

// askPopover shows the popover under the toolbar's button: in an
// NSPopover, where there are windows, as the branch switcher.
func (a *app) askPopover(c *ui.Context) {
	if a.codexRunning() {
		a.ask.open = false
	}
	var anchor mygo.Rectangle
	switch {
	case a.nativeBar:
		r, ok := a.win.ToolbarItemBounds("codex")
		if !ok && a.askPopoverWin.win == nil {
			a.ask.open = false // nowhere to show it from
			return
		}
		anchor = r
	case a.native():
		e := c.Resolve(a.ask.button)
		if !e.Valid() {
			return
		}
		b := e.Bounds()
		anchor = mygo.Rectangle{X: int(b.X), Y: int(b.Y), Width: int(b.W), Height: int(b.H)}
	default:
		e := c.Resolve(a.ask.button)
		if !e.Valid() {
			return
		}
		ui.Popover(c.Key("ask"), e, &a.ask.open, func() { a.askPanel(c) })
		return
	}
	a.popover(&a.askPopoverWin, &a.ask.open, anchor, askWidth+2*popoverPadding, func(c *ui.Context) { a.askPanel(c) })
}

// askPanel is the popover's content, as Xcode's: the request, which
// Return sends and Shift-Return breaks, and under it the templates, what
// it is of, and Send, which starts the conversation and shows it.
func (a *app) askPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.ask
	ui.Column(c).Width(askWidth).Gap(t.Space(2)).Padding(t.Space(1)).Children(func() {
		field := ui.TextAreaBase(c.Key("ask-request"), &s.text).Bind(&s.field).FillWidth().MinHeight(64).MaxHeight(200).
			FontSize(fontSize + 2).Placeholder("New conversation with Codex").Label("Request")
		field.OnTextKey(func(k ui.TextKey) bool {
			if k.Key != ui.KeyEnter || k.Mods != 0 {
				return false // Shift-Return breaks the line
			}
			a.sendAsk()
			return true
		})
		if field.Changed() && strings.TrimSpace(s.text) == "" {
			s.template = -1
		}
		if s.focus {
			s.focus = false
			s.field.Focus(c)
		}
		ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
			templatesButton(c, s, &s.text)
			ui.Text(c, scopeDetail(s, a)).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			sendButton(c, "ask-send", "Send", a.canStart(s, s.text), a.sendAsk)
		})
	})
}

// scopeDetail is what a request starting a conversation is of, after its
// template.
func scopeDetail(s *askState, a *app) string {
	_, scope := a.askScope(s)
	if tp := templateOf(s); tp != nil {
		return tp.name + " · " + scope
	}
	return scope
}
