package app

import (
	"context"
	"errors"
	"peek/internal/git"
	"peek/internal/review"
	"time"
)

type reviewState struct {
	codexTask
	stale                bool
	sourceID, scope, err string
	// head and base are the commits the review was of: base "" for the
	// uncommitted changes, else the commits from it to head.
	head, base string
	result     *review.Result
	conv       *conversation // the conversation it is of
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
	shown := request
	if shown == "" {
		shown = reviewTemplate().request
	}
	snapshot, targetErr := a.reviewTarget()
	if targetErr != "" {
		a.review = reviewState{err: targetErr}
		conv := a.newConversation("Review", "Uncommitted changes", shown)
		conv.Turns[0].Err = targetErr
		a.saveConversations()
		return
	}
	if snapshot.ID == "" {
		return
	}
	conv := a.newConversation("Review", snapshot.Scope, shown)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	repo := a.repo
	a.review = reviewState{codexTask: codexTask{running: true, cancel: cancel, phase: "Checking…", detail: "Checking the review snapshot"},
		sourceID: snapshot.ID, scope: snapshot.Scope, head: snapshot.Head, base: snapshot.Base, conv: conv}
	progress := a.codexProgress(ctx, &a.review.codexTask, func() bool { return !a.review.stale && a.review.sourceID == snapshot.ID })
	a.background(func() func() {
		defer cancel()
		var session string
		run := func(ctx context.Context, repo string, snapshot review.Snapshot) (review.Result, error) {
			progress("Starting…", "Starting Codex")
			result, id, err := review.CodexSession(ctx, repo, snapshot, request, progress)
			session = id
			progress("Checking…", "Validating the review result and checking for changes")
			return result, err
		}
		result, err := review.Run(ctx, repo, snapshot, run)
		return func() {
			a.review.finish()
			tn := conv.Turns[0]
			conv.Session = session
			switch {
			case errors.Is(err, context.Canceled):
				tn.Err = "Stopped"
			case errors.Is(err, review.ErrChanged) || a.reviewSourceOf(snapshot.Scope).ID != snapshot.ID:
				a.review.stale = true
				tn.Err = "The changes changed during the review. Review them again."
			case err != nil:
				a.review.err = err.Error()
				tn.Err = a.review.err
			case !a.review.stale:
				a.review.result = &result
				tn.Review = &result
			}
			a.saveConversations()
			if !a.review.stale && (a.review.result != nil || a.review.err != "") {
				a.lastCodex = codexReview
				body := a.review.err
				if a.review.result != nil {
					body = a.review.result.Summary
				}
				a.notify(a.review.title(), body, func() { a.showConversation(conv) })
			}
			a.refresh()
		}
	})
}

// reviewTemplate is the template that reviews.
func reviewTemplate() askTemplate {
	for _, tp := range askTemplates {
		if tp.review {
			return tp
		}
	}
	return askTemplate{}
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
