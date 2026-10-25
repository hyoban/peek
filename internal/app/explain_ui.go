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

// explainState is Codex's explanation of the entry of the history it was
// of: the uncommitted changes, a commit, or the branch's changes.
type explainState struct {
	codexTask
	open bool
	hash string // the entry's
	// sourceID is what the entry's changes were as explained, which
	// tells an explanation of the uncommitted changes out of date.
	sourceID, scope string
	text, err       string
}

// explainSource is what an explanation of the entry would be of: its
// changes as read last, and their scope; "" where it has none.
func (a *app) explainSource(cm git.Commit) (id, scope string) {
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

// explainable is what an explanation of the entry shown would be of.
func (a *app) explainable() (cm git.Commit, id, scope string) {
	cm, ok := a.current()
	if ok {
		id, scope = a.explainSource(cm)
	}
	return cm, id, scope
}

func (a *app) explainButton(c *ui.Context) {
	e := &a.explain
	services := c.Services()
	if e.running {
		e.activity(c, "explain", "Explaining…", "Codex is explaining the changes", "Stop Explaining")
		return
	}
	cm, id, scope := a.explainable()
	done := id != "" && e.hash == cm.Hash && (e.text != "" || e.err != "")
	open := done && e.sourceID == id
	label, tooltip := "Explain", "Explain "+scope+" with Codex"
	switch {
	case done && !open:
		label, tooltip = "Explain Again", "The changes have changed since they were explained"
	case open && e.err != "":
		label, tooltip = "Explain Error", e.err
	case open:
		label, tooltip = "Explanation", "Read Codex's explanation of "+e.scope
	case id == "":
		tooltip = "Nothing to explain"
	}
	ui.Button(c.Key("explain"), label).Shrink(0).Disabled(a.changing() || id == "").Tooltip(tooltip).OnClick(func() {
		if open {
			e.open = true
			return
		}
		a.startExplain(services)
	})
}

// startExplain has Codex explain the changes of the entry shown.
func (a *app) startExplain(services ui.Services) {
	cm, id, scope := a.explainable()
	if a.explain.running || id == "" || a.changing() {
		return
	}
	snapshot := a.reviewSource
	if git.IsRange(cm.Hash) {
		snapshot = a.branchReview
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.explain = explainState{codexTask: codexTask{running: true, cancel: cancel, phase: "Starting…", detail: "Starting Codex"},
		hash: cm.Hash, sourceID: id, scope: scope}
	progress := a.codexProgress(ctx, &a.explain.codexTask, func() bool { return a.explain.sourceID == id })
	repo := a.repo
	a.background(func() func() {
		defer cancel()
		var text string
		var err error
		if !git.IsRange(cm.Hash) && cm.Hash != git.Worktree {
			snapshot, err = review.CommitSnapshot(repo, cm.Hash)
		}
		if err == nil {
			text, err = review.Explain(ctx, repo, snapshot, progress)
		}
		return func() {
			e := &a.explain
			if e.sourceID != id || !e.running {
				return
			}
			e.finish()
			switch {
			case errors.Is(err, context.Canceled):
				*e = explainState{}
				return
			case err != nil:
				e.err = err.Error()
			default:
				e.text = text
			}
			title := "Explanation ready"
			if err != nil {
				title = "Explanation error"
			}
			a.notify(title, e.scope, func() { e.open = true })
		}
	})
}

// explanationDetails is the explanation in a sheet, as its Markdown
// reads, to copy or ask for again.
func (a *app) explanationDetails(c *ui.Context) {
	e := &a.explain
	services := c.Services()
	resultSheet(c, &e.open, "Explanation", e.scope, func() {
		if e.err != "" {
			ui.Text(c, e.err).Selectable()
			return
		}
		markdown.View(c, e.text, &markdown.Options{Theme: &markdown.Theme{CodeFont: monoFamily}, WrapCode: true})
	}, strings.TrimSpace(e.text), "Explain Again", a.changing() || e.running, func() {
		e.open = false
		a.startExplain(services)
	})
}
