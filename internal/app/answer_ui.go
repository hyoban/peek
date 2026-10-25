package app

import (
	"context"
	"errors"
	"peek/internal/git"
	"peek/internal/review"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// answerState is Codex's answer to what the user asked of the entry of
// the history it was of: the uncommitted changes, a commit, or the
// branch's changes.
type answerState struct {
	codexTask
	open bool
	hash string // the entry's
	// sourceID is what the entry's changes were as asked about, which
	// tells an answer about the uncommitted changes out of date.
	sourceID, scope string
	// title names the answer, its template's or Answer, and request is
	// what was asked.
	title, request string
	text, err      string
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

// answerFresh reports whether the answer is of the entry shown as it
// is now.
func (a *app) answerFresh() bool {
	cm, id, _ := a.answerable()
	e := &a.answer
	return id != "" && e.hash == cm.Hash && e.sourceID == id && (e.text != "" || e.err != "")
}

// startAnswer has Codex answer the request about the changes of the
// entry shown, the answer titled title.
func (a *app) startAnswer(title, request string) {
	cm, id, scope := a.answerable()
	request = strings.TrimSpace(request)
	if a.codexRunning() || id == "" || request == "" || a.changing() {
		return
	}
	snapshot := a.reviewSource
	if git.IsRange(cm.Hash) {
		snapshot = a.branchReview
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.answer = answerState{codexTask: codexTask{running: true, cancel: cancel, phase: "Starting…", detail: "Starting Codex"},
		hash: cm.Hash, sourceID: id, scope: scope, title: title, request: request}
	progress := a.codexProgress(ctx, &a.answer.codexTask, func() bool { return a.answer.sourceID == id })
	repo := a.repo
	a.background(func() func() {
		defer cancel()
		var text string
		var err error
		if !git.IsRange(cm.Hash) && cm.Hash != git.Worktree {
			snapshot, err = review.CommitSnapshot(repo, cm.Hash)
		}
		if err == nil {
			text, err = review.Ask(ctx, repo, snapshot, request, progress)
		}
		return func() {
			e := &a.answer
			if e.sourceID != id || !e.running {
				return
			}
			e.finish()
			switch {
			case errors.Is(err, context.Canceled):
				*e = answerState{}
				return
			case err != nil:
				e.err = err.Error()
			default:
				e.text = text
			}
			a.lastCodex = codexAnswer
			a.notify(e.heading(), e.scope, func() { e.open = true })
		}
	})
}

// heading is the answer's, as its sheet and notification name it.
func (e *answerState) heading() string {
	if e.err != "" {
		return e.title + " error"
	}
	return e.title
}

// answerDetails is the answer in a sheet, as its Markdown reads, to copy
// or ask for again.
func (a *app) answerDetails(c *ui.Context) {
	e := &a.answer
	a.resultSheet(c, &a.answerSheet, &e.open, e.heading(), e.scope, func(c *ui.Context) {
		if e.err != "" {
			ui.Text(c, e.err).Selectable()
			return
		}
		markdown.View(c, e.text, &markdown.Options{Theme: &markdown.Theme{CodeFont: monoFamily}, WrapCode: true})
	}, strings.TrimSpace(e.text), "Ask Again", a.changing() || a.codexRunning(), func() {
		e.open = false
		a.startAnswer(e.title, e.request)
	})
}
