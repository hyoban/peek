package app

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"peek/internal/review"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// conversation is one with Codex about the repository's changes, as
// Xcode's coding assistant keeps them: the requests and what Codex
// answered, Codex keeping the conversation itself, by session, for the
// requests that follow up.
type conversation struct {
	ID      string    `json:"id"`
	Title   string    `json:"title"`
	Created time.Time `json:"created"`
	Scope   string    `json:"scope"`
	Session string    `json:"session,omitempty"`
	Turns   []*turn   `json:"turns"`
}

// turn is a request of a conversation and Codex's answer to it: in
// Markdown, a review, or the error that came instead, none while Codex
// works on it.
type turn struct {
	Request string         `json:"request"`
	Answer  string         `json:"answer,omitempty"`
	Review  *review.Result `json:"review,omitempty"`
	Err     string         `json:"error,omitempty"`
}

func (t *turn) done() bool { return t.Answer != "" || t.Review != nil || t.Err != "" }

// The sidebar's navigators, as Xcode's: the files, and the conversations
// with Codex.
const (
	navFiles = iota
	navConversations
)

// newConversation starts a conversation of the request about the scope,
// titled title, and shows it: the new one shown where it is not started
// yet, else another, first in the list.
func (a *app) newConversation(title, scope, request string) *conversation {
	conv := a.shownConversation()
	if conv == nil || len(conv.Turns) > 0 {
		conv = &conversation{ID: newConversationID()}
		a.conversations = append([]*conversation{conv}, a.conversations...)
	}
	conv.Title, conv.Created, conv.Scope, conv.Turns = title, time.Now(), scope, []*turn{{Request: request}}
	a.showConversation(conv)
	a.saveConversations()
	return conv
}

// newConversationID is the ID of a conversation made now.
func newConversationID() string { return fmt.Sprintf("%d", time.Now().UnixNano()) }

// conversationTitle is the title of a conversation of the request: its
// first line, cut short.
func conversationTitle(request string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(request), "\n")
	if r := []rune(line); len(r) > 60 {
		line = string(r[:60]) + "…"
	}
	return line
}

// showConversation shows the conversation in place of the diffs, chosen
// in the sidebar's list of conversations.
func (a *app) showConversation(conv *conversation) {
	for i, c := range a.conversations {
		if c == conv {
			a.convRow = i
		}
	}
	a.navigator = navConversations
}

// shownConversation is the conversation shown in place of the diffs, nil
// for none.
func (a *app) shownConversation() *conversation {
	if a.navigator != navConversations || a.convRow < 0 || a.convRow >= len(a.conversations) {
		return nil
	}
	return a.conversations[a.convRow]
}

// running reports whether Codex works on the conversation.
func (a *app) running(conv *conversation) bool {
	return a.answer.running && a.answer.conv == conv || a.review.running && a.review.conv == conv
}

// deleteConversation forgets the conversation, stopping Codex's work on
// it.
func (a *app) deleteConversation(conv *conversation) {
	if a.answer.conv == conv {
		a.answer.stop()
	}
	if a.review.conv == conv {
		a.review.stop()
	}
	for i, c := range a.conversations {
		if c == conv {
			a.conversations = append(a.conversations[:i], a.conversations[i+1:]...)
			if a.convRow >= len(a.conversations) {
				a.convRow = len(a.conversations) - 1
			}
			break
		}
	}
	a.saveConversations()
}

// conversationsFile is the file keeping the repository's conversations,
// in the app's folder, by repository: "" in tests, which keep none.
func conversationsFile(dir, repo string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(dir, "conversations", fmt.Sprintf("%x.json", sha1.Sum([]byte(repo))))
}

// loadConversations reads the repository's conversations, a request Codex
// worked on as the app quit stopped.
func (a *app) loadConversations() {
	if a.convFile == "" {
		return
	}
	data, err := os.ReadFile(a.convFile)
	if err != nil {
		return
	}
	var convs []*conversation
	if json.Unmarshal(data, &convs) != nil {
		return
	}
	for _, conv := range convs {
		for _, t := range conv.Turns {
			if !t.done() {
				t.Err = "Stopped"
			}
		}
	}
	a.conversations = convs
}

// saveConversations keeps the repository's conversations for the next
// launches.
func (a *app) saveConversations() {
	if a.convFile == "" {
		return
	}
	var started []*conversation // not those not started yet
	for _, conv := range a.conversations {
		if len(conv.Turns) > 0 {
			started = append(started, conv)
		}
	}
	data, err := json.MarshalIndent(started, "", "\t")
	if err != nil {
		return
	}
	os.MkdirAll(filepath.Dir(a.convFile), 0o755)
	os.WriteFile(a.convFile, data, 0o600)
}

// chooseNavigator shows the navigator in the sidebar, or hides the
// sidebar where it shows it already, as Xcode's navigators do.
func (a *app) chooseNavigator(n int) {
	if n == a.navigator && a.showTree {
		a.toggle(&a.showTree, &a.collapsedTree)
		return
	}
	a.showNavigator(n)
	if !a.showTree {
		a.toggle(&a.showTree, &a.collapsedTree)
	}
}

// showNavigator shows the navigator in the sidebar: the conversations
// with the newest chosen where none is, to show in place of the diffs.
func (a *app) showNavigator(n int) {
	a.navigator = n
	if n == navConversations && a.convRow < 0 && len(a.conversations) > 0 {
		a.convRow = 0
	}
}

// navigatorBar is the row of the sidebar's navigators where AppKit's
// toolbar does not show them (chooseNavigator): the files and the
// conversations with Codex, as Xcode's navigator bar, a capsule of
// symbols, the one shown raised.
func (a *app) navigatorBar(c *ui.Context) {
	if a.nativeBar {
		return
	}
	t := c.Theme()
	ui.Row(c).Shrink(0).FillWidth().Padding(t.Space(1), t.Space(2)).Children(func() {
		ui.Row(c).FillWidth().Justify(ui.SpaceAround).Padding(2).Radius(999).Background(t.Text.Alpha(0.06)).Children(func() {
			for _, nav := range []struct {
				n     int
				label string
				icon  *ui.SVG
			}{{navFiles, "File Navigator", folderIcon}, {navConversations, "Conversation Navigator", commentIcon}} {
				on := a.navigator == nav.n
				b := ui.ButtonBase(c.Key("nav-" + nav.label)).Grow(1).Height(24).Radius(999).AlignItems(ui.Center).Justify(ui.Center).
					Label(nav.label).Tooltip(nav.label).Selected(on).TextColor(t.TextMuted)
				if on {
					b.Background(t.Text.Alpha(0.1)).TextColor(t.Text)
				}
				b.Children(func() { ui.Icon(c, nav.icon).Size(15, 15) })
				b.OnClick(func() { a.showNavigator(nav.n) })
			}
		})
	})
}

// capsuleButton is a button as Xcode's navigators head their lists with,
// New Conversation's: a capsule of the control's fill.
func capsuleButton(c *ui.Context, key, label string) ui.Element {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Shrink(0).Height(26).Padding(0, t.Space(3)).Radius(999).AlignItems(ui.Center).
		Background(t.Text.Alpha(0.08)).Label(label)
	b.Children(func() { ui.Text(c, label).FontSize(fontSize).SingleLine() })
	return b
}

// conversationsPane is the sidebar's list of conversations, newest first,
// under New Conversation, which shows a new one, empty, to write in.
func (a *app) conversationsPane(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Shrink(0).FillWidth().Padding(t.Space(1), t.Space(3)).Children(func() {
		capsuleButton(c, "new-conversation", "New Conversation").OnClick(a.newDraft)
	})
	if len(a.conversations) == 0 {
		emptyNote(c, "No Conversations")
		return
	}
	a.convList.Selected = &a.convRow
	// No keys: the list's choice following its conversation would leave
	// a new one, at the top where the old one was, unchosen.
	a.convList.Label = func(i int) string { return a.conversations[i].Title }
	a.withFocus(c, a.convList.FocusWithin(c), func() {
		list := ui.List(c.Key("conversations"), &a.convList, len(a.conversations)).Gap(1).Label("Conversations")
		sideList(c, list).Rows(func(row ui.ListRow) { a.conversationItem(row, a.conversations[row.Index]) })
	})
}

// conversationItem is the row of a conversation, as a source list's: its
// title, and when and what of under it, or Codex working on it.
func (a *app) conversationItem(row ui.ListRow, conv *conversation) {
	c := row.Context
	t := c.Theme()
	in := rowInk(t, row.Selected(), row.ListFocused())
	item := ui.Row(c).FillWidth().Gap(t.Space(2)).Padding(t.Space(1.5), t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Column(c).Grow(1).MinWidth(0).Gap(1).Children(func() {
			ui.Text(c, conv.Title).TextColor(in.text).SingleLine().Ellipsis("…").FillWidth()
			detail := shortDate(conv.Created, time.Now())
			if conv.Scope != "" {
				detail += " · " + conv.Scope
			}
			ui.Text(c, detail).FontSize(smallSize).TextColor(in.muted).
				SingleLine().Ellipsis("…").FillWidth()
		})
		if a.running(conv) {
			ui.Spinner(c).Size(12, 12).Shrink(0)
		}
	})
	services := c.Services()
	item.ContextMenu(func(m *ui.Menu) {
		if m.Item("Copy Conversation").Chosen() {
			services.WriteClipboard(conv.text())
		}
		m.Separator()
		if m.Item("Delete Conversation").Chosen() {
			a.deleteConversation(conv)
		}
	})
}

// text is the conversation as copied: its requests and answers.
func (conv *conversation) text() string {
	var parts []string
	for _, t := range conv.Turns {
		parts = append(parts, "> "+strings.ReplaceAll(t.Request, "\n", "\n> "), t.text(conv.Scope))
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

// text is what Codex answered, as copied.
func (t *turn) text(scope string) string {
	switch {
	case t.Review != nil:
		return reviewHeading(t.Review) + "\n" + scope + "\n\n" + t.Review.Text()
	case t.Err != "":
		return t.Err
	}
	return t.Answer
}

// reviewHeading is a review's verdict, as its turn and the notification
// say it.
func reviewHeading(r *review.Result) string {
	if r.Verdict == "failed" {
		return "Review not passed"
	}
	return "Review passed"
}

// transcriptWidth is the widest the transcript reads, centered in the
// room, as Xcode's conversations.
const transcriptWidth = 720

// conversationPane is the conversation in place of the diffs, as Xcode
// shows one in its editor: the requests and Codex's answers in a column
// to read, and under them, floating, the field to follow up with.
func (a *app) conversationPane(c *ui.Context, conv *conversation) {
	t := c.Theme()
	jumpBar(c, conv.Title, "", mutedDetail(c, conv.Scope), func() {
		ui.Scroll(c.Key("transcript:" + conv.ID)).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Row(c).FillWidth().Justify(ui.Center).Padding(t.Space(6), t.Space(6), t.Space(4), t.Space(6)).Children(func() {
				ui.Column(c).Grow(1).MaxWidth(transcriptWidth).Gap(t.Space(6)).Children(func() {
					for i, tn := range conv.Turns {
						ui.Column(c).Key(i).FillWidth().Gap(t.Space(3)).Children(func() { a.turnView(c, conv, tn) })
					}
				})
			})
		})
		a.composer(c, conv)
	})
}

// turnView is a request, in a bubble at the end as the user's, and
// Codex's answer under it, plain on the page, or its stage as it works.
func (a *app) turnView(c *ui.Context, conv *conversation, tn *turn) {
	t := c.Theme()
	services := c.Services()
	ui.Row(c).FillWidth().Justify(ui.End).PaddingStart(t.Space(16)).Children(func() {
		ui.Box(c).Shrink(1).MinWidth(0).Padding(t.Space(2), t.Space(3.5)).Radius(18).Background(t.Text.Alpha(0.08)).Children(func() {
			ui.Text(c, tn.Request).Selectable()
		})
	})
	switch {
	case !tn.done():
		phase := a.answer.phase
		if a.review.conv == conv {
			phase = a.review.phase
		}
		if phase == "" {
			phase = "Thinking…"
		}
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Spinner(c).Size(12, 12)
			ui.Text(c, phase).TextColor(t.TextMuted)
		})
		return
	case tn.Err != "":
		ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Start).Children(func() {
			ui.Icon(c, warningIcon).Size(16, 16).Shrink(0).TextColor(system(c, systemOrange))
			ui.Text(c, tn.Err).TextColor(t.TextMuted).Selectable().Grow(1).MinWidth(0)
		})
	case tn.Review != nil:
		reviewView(c, tn.Review)
	default:
		markdown.View(c, tn.Answer, &markdown.Options{Theme: &markdown.Theme{CodeFont: monoFamily}, WrapCode: true})
	}
	// Under the answer, its actions, as symbols, muted until pointed at.
	ui.Row(c).Gap(t.Space(1)).Children(func() {
		b := ui.ButtonBase(c.Key("copy")).Size(26, 26).Radius(t.Radius).AlignItems(ui.Center).Justify(ui.Center).
			Label("Copy").Tooltip("Copy").TextColor(t.TextMuted)
		flat(b, t, false)
		b.Children(func() { ui.Icon(c, copyIcon).Size(15, 15) })
		b.OnClick(func() {
			services.WriteClipboard(tn.text(conv.Scope))
			c.Toast("Copied")
		})
	})
}

// reviewView is a review in its conversation: the verdict beside its
// symbol, the summary, and the findings, each a card.
func reviewView(c *ui.Context, r *review.Result) {
	t := c.Theme()
	glyph, tint := checkCircleIcon, system(c, systemGreen)
	if r.Verdict == "failed" {
		glyph, tint = warningIcon, system(c, systemOrange)
	}
	ui.Row(c).Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
		ui.Icon(c, glyph).Size(18, 18).TextColor(tint)
		ui.Text(c, reviewHeading(r)).FontWeight(600).FontSize(fontSize + 1)
	})
	ui.Text(c, r.Summary).Selectable()
	for i, f := range r.Findings {
		ui.Column(c).Key(i).FillWidth().Gap(t.Space(1)).Padding(t.Space(3)).Radius(10).Background(t.Text.Alpha(0.04)).
			Border(1, t.Text.Alpha(0.06)).Children(func() {
			ui.Text(c, findingTitle(f)).FontWeight(600).Selectable()
			ui.Text(c, fmt.Sprintf("%s:%d", f.Path, f.Line)).Font(monoFamily).FontSize(smallSize).TextColor(t.TextMuted).Selectable()
			ui.Text(c, f.Body).Selectable()
		})
	}
}

// composer is the field under a conversation, as Xcode's: a rounded box
// floating over the bottom of the transcript, the request over a row of
// the templates, where the conversation is new, what it is of, and the
// round button sending it, or stopping Codex where it works. Return
// sends and Shift-Return breaks the line.
func (a *app) composer(c *ui.Context, conv *conversation) {
	t := c.Theme()
	draft := len(conv.Turns) == 0
	placeholder, scope := "Reply to Codex", conv.Scope
	if draft {
		placeholder = "New conversation with Codex"
		_, scope = a.askScope()
		if tp := a.askTemplateOf(); tp != nil {
			scope = tp.name + " · " + scope
		}
	}
	ui.Row(c).Shrink(0).FillWidth().Justify(ui.Center).Padding(0, t.Space(6), t.Space(4), t.Space(6)).Children(func() {
		box := ui.Column(c).Grow(1).MaxWidth(transcriptWidth).Gap(t.Space(2)).Padding(t.Space(3), t.Space(3), t.Space(2.5), t.Space(4)).
			Radius(18).Background(t.Surface).Border(1, t.Border).Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.12))
		box.Children(func() {
			field := ui.TextAreaBase(c.Key("message"), &a.reply).Bind(&a.replyField).FillWidth().MinHeight(22).MaxHeight(200).
				Placeholder(placeholder).Label("Message")
			field.OnTextKey(func(k ui.TextKey) bool {
				if k.Key != ui.KeyEnter || k.Mods != 0 {
					return false
				}
				a.send(conv)
				return true
			})
			if field.Changed() && strings.TrimSpace(a.reply) == "" {
				a.ask.template = -1
			}
			if a.ask.focus {
				a.ask.focus = false
				a.replyField.Focus(c)
			}
			ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
				if draft {
					templates := ui.ButtonBase(c.Key("templates")).Shrink(0).Size(28, 28).Radius(14).AlignItems(ui.Center).
						Justify(ui.Center).Label("Templates").Tooltip("Templates").TextColor(t.TextMuted)
					flat(templates, t, false)
					templates.Children(func() { ui.Icon(c, templatesIcon).Size(18, 18) })
					templates.Menu(func(m *ui.Menu) {
						for i, tp := range askTemplates {
							if m.Item(tp.name).Checked(a.ask.template == i).Chosen() {
								a.chooseTemplate(i)
							}
						}
					})
				}
				ui.Text(c, scope).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Grow(1).MinWidth(0)
				if a.running(conv) {
					stopButton(c, func() {
						a.answer.stop()
						a.review.stop()
					})
					return
				}
				sendButton(c, "send", "Send", a.canSend(conv), func() { a.send(conv) })
			})
		})
	})
}

// stopButton is the round button stopping Codex, in place of Send while
// it works.
func stopButton(c *ui.Context, stop func()) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key("reply-stop")).Shrink(0).Size(28, 28).Radius(14).Label("Stop").Tooltip("Stop").
		AlignItems(ui.Center).Justify(ui.Center).Background(t.Text).TextColor(t.Background).OnClick(stop)
	b.Children(func() { ui.Icon(c, stopIcon).Size(12, 12) })
}

// sendButton is the round button sending a request, the accent's while
// it can.
func sendButton(c *ui.Context, key, label string, can bool, send func()) {
	t := c.Theme()
	b := ui.ButtonBase(c.Key(key)).Shrink(0).Size(28, 28).Radius(14).Label(label).Tooltip(label).
		Disabled(!can).AlignItems(ui.Center).Justify(ui.Center).OnClick(send)
	if can {
		b.Background(t.Accent).TextColor(t.AccentText)
	} else {
		b.Background(t.Text.Alpha(0.08)).TextColor(t.TextMuted)
	}
	b.Children(func() { ui.Icon(c, sendIcon).Size(14, 14) })
}
