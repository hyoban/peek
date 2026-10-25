package app

import (
	"peek/internal/review"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// askTemplate is a request to Codex the conversation popover offers, to
// send as it is or edit: review ones have Codex review the changes, its
// findings deciding whether they pass, and the others answer in Markdown.
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

// askState is the conversation popover's, as Xcode's coding assistant
// starts a conversation: the request being written, and the template it
// came from, -1 for none.
type askState struct {
	open     bool
	text     string
	template int
	field    ui.Handle // the request's, which takes the keys as it opens
	focus    bool
	button   ui.Handle // the toolbar's button, under which it shows
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

// askScope is what the request would be of: the changes a review is of,
// as the history's selection has them, or the entry shown.
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

// openAsk shows the conversation popover, its field taking the keys.
func (a *app) openAsk() {
	if a.codexRunning() {
		return
	}
	a.ask.open, a.ask.focus = true, true
	if a.ask.text == "" {
		a.ask.template = -1
	}
}

// chooseTemplate puts the template's request in the field, to send or
// edit.
func (a *app) chooseTemplate(i int) {
	a.ask.template, a.ask.text, a.ask.focus = i, askTemplates[i].request, true
}

// canAsk reports whether the request can go to Codex now.
func (a *app) canAsk() bool {
	id, _ := a.askScope()
	return strings.TrimSpace(a.ask.text) != "" && id != "" && !a.codexRunning() && !a.changing()
}

// sendAsk sends the request to Codex, as a review where its template is
// one, and closes the popover, the request cleared for the next.
func (a *app) sendAsk() {
	if !a.canAsk() {
		return
	}
	text := strings.TrimSpace(a.ask.text)
	tp := a.askTemplateOf()
	switch {
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
	a.ask = askState{template: -1, field: a.ask.field, button: a.ask.button}
}

// codexTool is the toolbar's button of Codex: the request under way,
// which it stops, or the button opening the conversation popover, titled
// with the verdict of the review where it came last.
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

// askWidth is the width of the conversation popover's content.
const askWidth = 400

// askPopover shows the conversation popover under the toolbar's button:
// in an NSPopover, where there are windows, as the branch switcher.
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

// askPanel is the conversation popover's content, as Xcode's: the
// request, which Return sends and Shift-Return breaks, and under it the
// templates, what it is of, and Send, which starts the conversation.
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
			templates := ui.ButtonBase(c.Key("ask-templates")).Shrink(0).Padding(t.Space(1)).Radius(t.Radius).
				Label("Templates").Tooltip("Templates")
			flat(templates, t, false)
			templates.Children(func() { ui.Icon(c, templatesIcon).Size(18, 18) })
			templates.Menu(func(m *ui.Menu) {
				for i, tp := range askTemplates {
					if m.Item(tp.name).Checked(s.template == i).Chosen() {
						a.chooseTemplate(i)
					}
				}
			})
			_, scope := a.askScope()
			detail := scope
			if tp := a.askTemplateOf(); tp != nil {
				detail = tp.name + " · " + scope
			}
			ui.Text(c, detail).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
			sendButton(c, "ask-send", "Send", a.canAsk(), a.sendAsk)
		})
	})
}
