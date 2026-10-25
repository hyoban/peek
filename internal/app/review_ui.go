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

func (a *app) reviewButton(c *ui.Context) { drawTool(c, "review", a.reviewTool(c)) }

// reviewTool is the Review button as the review's state has it.
func (a *app) reviewTool(c *ui.Context) toolButton {
	services := c.Services()
	if a.review.running {
		text, tooltip := a.review.activityText(c, "Reviewing…", "Codex is reviewing the changes")
		return toolButton{label: text, tooltip: tooltip, busy: true, stop: "Stop Review", cancel: a.review.stop}
	}
	target, targetErr := a.reviewTarget()
	// A review of other changes than those selected stays for them, and
	// the selected ones are offered instead.
	other := a.review.sourceID != "" && (a.review.scope == review.BranchScope) != (target.Scope == review.BranchScope)
	label, tooltip := "Review", "Review the changes with Codex"
	switch {
	case other && targetErr != "":
		tooltip = targetErr
	case other && target.ID == "":
		tooltip = "Nothing to review"
	case other:
		tooltip = "Review " + target.Scope + " with Codex"
	case a.review.stale:
		label, tooltip = "Review Again", "The changes have changed since the last review"
	case a.review.err != "":
		label, tooltip = "Review Error", a.review.err
	case a.review.result != nil:
		label, tooltip = "Passed", a.review.result.Summary
		if n := len(a.review.result.Findings); n > 0 && a.review.result.Verdict != "failed" {
			tooltip += "\n" + count(n, "minor finding")
		}
		if a.review.result.Verdict == "failed" {
			label = "Not Passed"
		}
	case targetErr != "":
		tooltip = targetErr
	case target.ID == "":
		tooltip = "Nothing to review"
	default:
		tooltip = "Review " + target.Scope + " with Codex"
	}
	available := target.ID != "" || targetErr != ""
	completed := !other && !a.review.stale && (a.review.result != nil || a.review.err != "")
	disabled := a.changing() || !available && !completed
	if a.review.stale && !other && !available {
		label, tooltip = "Review", "Nothing to review"
	}
	return toolButton{label: label, tooltip: tooltip, disabled: disabled, click: func() {
		if completed {
			a.review.open = true
			return
		}
		a.startReview(services)
	}}
}

func (a *app) startReview(services ui.Services) {
	if a.review.running || a.changing() {
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
		sourceID: snapshot.ID, scope: snapshot.Scope, head: snapshot.Head, base: snapshot.Base}
	progress := a.codexProgress(ctx, &a.review.codexTask, func() bool { return !a.review.stale && a.review.sourceID == snapshot.ID })
	a.background(func() func() {
		defer cancel()
		run := func(ctx context.Context, repo string, snapshot review.Snapshot) (review.Result, error) {
			progress("Starting…", "Starting Codex")
			result, err := review.Codex(ctx, repo, snapshot, progress)
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
	services := c.Services()
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
	}, r.text(), "Review Again", a.changing() || r.running || a.reviewTargetID() == "", func() { a.startReview(services) })
}
