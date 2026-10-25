package app

import (
	"context"
	"errors"
	"fmt"
	"peek/internal/git"
	"peek/internal/review"
	"time"

	"github.com/egoist/mygo/ui"
)

type reviewState struct {
	codexTask
	stale, open          bool
	sourceID, scope, err string
	// head and base are the commits the review was of: base "" for the
	// uncommitted changes, else the commits from it to head.
	head, base string
	request    string // what the user asked more of it, "" for none
	result     *review.Result
}

func (a *app) updateReviewSource(snapshot review.Snapshot, err string) {
	a.reviewSource, a.reviewSourceErr = snapshot, err
	if a.review.scope != review.BranchScope && a.review.sourceID != "" && (snapshot.ID != a.review.sourceID || err != "") {
		a.review.outdated()
	}
}

func (a *app) updateBranchReview(snapshot review.Snapshot) {
	a.branchReview = snapshot
	if a.review.scope == review.BranchScope && a.review.sourceID != "" && snapshot.ID != a.review.sourceID {
		a.review.outdated()
	}
}

// outdated marks the review out of date, stopping it where it runs.
func (r *reviewState) outdated() {
	r.stale = true
	r.stop()
}

// reviewTarget is what a review would be of, as the history's selection
// has it: the branch's changes where they show, else the uncommitted
// changes or the commits not pushed yet.
func (a *app) reviewTarget() (review.Snapshot, string) {
	if cm, ok := a.current(); ok && git.IsRange(cm.Hash) && a.branchReview.ID != "" {
		return a.branchReview, ""
	}
	return a.reviewSource, a.reviewSourceErr
}

func (a *app) reviewTargetID() string {
	target, _ := a.reviewTarget()
	return target.ID
}

// reviewSourceOf is the source of a review of the scope as read last.
func (a *app) reviewSourceOf(scope string) review.Snapshot {
	if scope == review.BranchScope {
		return a.branchReview
	}
	return a.reviewSource
}

// startReview has Codex review the changes, as request asks where the
// user asked more: "" for none.
func (a *app) startReview(request string) {
	if a.codexRunning() || a.changing() {
		return
	}
	snapshot, targetErr := a.reviewTarget()
	if targetErr != "" {
		a.review = reviewState{err: targetErr, open: true}
		return
	}
	if snapshot.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	repo := a.repo
	a.review = reviewState{codexTask: codexTask{running: true, cancel: cancel, phase: "Checking…", detail: "Checking the review snapshot"},
		sourceID: snapshot.ID, scope: snapshot.Scope, head: snapshot.Head, base: snapshot.Base, request: request}
	progress := a.codexProgress(ctx, &a.review.codexTask, func() bool { return !a.review.stale && a.review.sourceID == snapshot.ID })
	a.background(func() func() {
		defer cancel()
		run := func(ctx context.Context, repo string, snapshot review.Snapshot) (review.Result, error) {
			progress("Starting…", "Starting Codex")
			result, err := review.Codex(ctx, repo, snapshot, request, progress)
			progress("Checking…", "Validating the review result and checking for changes")
			return result, err
		}
		result, err := review.Run(ctx, repo, snapshot, run)
		return func() {
			a.review.finish()
			if errors.Is(err, review.ErrChanged) || a.reviewSourceOf(snapshot.Scope).ID != snapshot.ID {
				a.review.stale = true
			} else if err != nil {
				a.review.err = err.Error()
			} else if !a.review.stale {
				a.review.result = &result
			}
			if !a.review.stale && (a.review.result != nil || a.review.err != "") {
				a.lastCodex = codexReview
				body := a.review.err
				if a.review.result != nil {
					body = a.review.result.Summary
				}
				a.notify(a.review.title(), body, func() { a.review.open = true })
			}
			a.refresh()
		}
	})
}

func (r *reviewState) title() string {
	switch {
	case r.result == nil:
		return "Review error"
	case r.result.Verdict == "failed":
		return "Review not passed"
	}
	return "Review passed"
}

// text is the review as copied to the clipboard.
func (r *reviewState) text() string {
	body := r.err
	if r.result != nil {
		body = r.result.Text()
	}
	return r.title() + "\n" + r.scope + "\n\n" + body
}

// reviewDetails is the review in a sheet, its findings to read and copy.
func (a *app) reviewDetails(c *ui.Context) {
	r := &a.review
	a.resultSheet(c, &a.reviewSheet, &r.open, r.title(), r.scope, func(c *ui.Context) {
		t := c.Theme()
		if r.stale {
			ui.Text(c, "This review is out of date. Review the current changes again.")
		}
		if r.err != "" {
			ui.Text(c, r.err).Selectable()
		}
		if result := r.result; result != nil {
			ui.Text(c, result.Summary).Selectable()
			for i, f := range result.Findings {
				ui.Column(c).Key(i).FillWidth().Gap(t.Space(1)).Children(func() {
					ui.Text(c, findingTitle(f)).FontWeight(600).Selectable()
					ui.Text(c, fmt.Sprintf("%s:%d", f.Path, f.Line)).TextColor(t.TextMuted).Selectable()
					ui.Text(c, f.Body).Selectable()
				})
			}
		}
	}, r.text(), "Review Again", a.changing() || a.codexRunning() || a.reviewTargetID() == "", func() { a.startReview(r.request) })
}
