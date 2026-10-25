package app

import (
	"context"
	"errors"
	"peek/internal/git"
	"peek/internal/review"
	"strings"
	"time"
)

// answerState is Codex's work on a request of a conversation, answered
// in Markdown: conv's turn at index turn.
type answerState struct {
	codexTask
	conv *conversation
	turn int
}

// answerSource is what a question about the entry would be of: its
// changes as read last, and their scope; "" where it has none.
func (a *app) answerSource(cm git.Commit) (id, scope string) {
	switch {
	case cm.Hash == git.Worktree:
		if a.reviewSource.Scope == "Uncommitted changes" {
			return a.reviewSource.ID, a.reviewSource.Scope
		}
		return "", ""
	case git.IsRange(cm.Hash):
		return a.branchReview.ID, review.BranchScope
	}
	return review.CommitScope + "\x00" + cm.Hash, review.CommitScope + " " + cm.Short
}

// answerable is what a question about the entry shown would be of.
func (a *app) answerable() (cm git.Commit, id, scope string) {
	cm, ok := a.current()
	if ok {
		id, scope = a.answerSource(cm)
	}
	return cm, id, scope
}

// startAnswer starts a conversation of the request about the changes of
// the entry shown, titled title, or after the request where it is "".
func (a *app) startAnswer(title, request string) {
	_, id, scope := a.answerable()
	request = strings.TrimSpace(request)
	if a.codexRunning() || id == "" || request == "" || a.changing() {
		return
	}
	if title == "" {
		title = conversationTitle(request)
	}
	a.runAnswer(a.newConversation(title, scope, request))
}

// followUp asks Codex the request in the conversation, which it answers
// with the conversation's earlier requests in mind.
func (a *app) followUp(conv *conversation, request string) {
	if a.codexRunning() || a.changing() || strings.TrimSpace(request) == "" {
		return
	}
	conv.Turns = append(conv.Turns, &turn{Request: request})
	a.showConversation(conv)
	a.saveConversations()
	a.runAnswer(conv)
}

// runAnswer has Codex answer the last request of the conversation: in
// its session where it has one, else about the changes of the entry
// shown.
func (a *app) runAnswer(conv *conversation) {
	cm, _, _ := a.answerable()
	snapshot := a.reviewSource
	if git.IsRange(cm.Hash) {
		snapshot = a.branchReview
	}
	i := len(conv.Turns) - 1
	request, session := conv.Turns[i].Request, conv.Session
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.answer = answerState{codexTask: codexTask{running: true, cancel: cancel, phase: "Starting…", detail: "Starting Codex"},
		conv: conv, turn: i}
	progress := a.codexProgress(ctx, &a.answer.codexTask, func() bool { return a.answer.conv == conv && a.answer.turn == i })
	repo := a.repo
	a.background(func() func() {
		defer cancel()
		var text string
		var err error
		if session == "" && !git.IsRange(cm.Hash) && cm.Hash != git.Worktree {
			snapshot, err = review.CommitSnapshot(repo, cm.Hash)
		}
		if err == nil {
			text, session, err = review.Ask(ctx, repo, session, snapshot, request, progress)
		}
		return func() {
			e := &a.answer
			if e.conv != conv || e.turn != i || !e.running {
				return
			}
			e.finish()
			if session != "" {
				conv.Session = session
			}
			tn := conv.Turns[i]
			switch {
			case errors.Is(err, context.Canceled):
				tn.Err = "Stopped"
			case err != nil:
				tn.Err = err.Error()
			default:
				tn.Answer = text
			}
			a.saveConversations()
			if !errors.Is(err, context.Canceled) {
				a.lastCodex = codexAnswer
				a.notify(conv.Title, conv.Scope, func() { a.showConversation(conv) })
			}
		}
	})
}
