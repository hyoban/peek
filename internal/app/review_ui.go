package app

import (
	"context"
	"errors"
	"fmt"
	"peek/internal/review"
	"time"

	"github.com/egoist/mygo/ui"
)

type reviewState struct {
	running, stale, open bool
	cancel               context.CancelFunc
	sourceID, scope, err string
	// head and base are the commits the review was of: base "" for the
	// uncommitted changes, else the commits from it to head.
	head, base    string
	phase, detail string
	started       time.Time
	result        *review.Result
}

func (a *app) updateReviewSource(snapshot review.Snapshot, err string) {
	a.reviewSource, a.reviewSourceErr = snapshot, err
	if a.review.sourceID != "" && (snapshot.ID != a.review.sourceID || err != "") {
		a.review.stale = true
		if a.review.cancel != nil {
			a.review.cancel()
		}
	}
}

func (a *app) reviewButton(c *ui.Context) {
	services := c.Services()
	if a.review.running {
		text, tooltip := "Reviewing…", "Codex is reviewing the changes"
		if a.review.phase != "" {
			if a.review.started.IsZero() {
				a.review.started = c.Now()
			}
			elapsed := c.Now().Sub(a.review.started).Truncate(time.Second)
			text, tooltip = fmt.Sprintf("%s %s", a.review.phase, elapsed), a.review.detail
			c.After(time.Second)
		}
		activity(c, "review", text, tooltip, "Stop Review", func() { a.review.cancel() })
		return
	}
	label, tooltip := "Review", "Review the changes with Codex"
	switch {
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
	case a.reviewSourceErr != "":
		tooltip = a.reviewSourceErr
	case a.reviewSource.ID == "":
		tooltip = "Nothing to review"
	default:
		tooltip = "Review " + a.reviewSource.Scope + " with Codex"
	}
	available := a.reviewSource.ID != "" || a.reviewSourceErr != ""
	completed := !a.review.stale && (a.review.result != nil || a.review.err != "")
	disabled := a.push.running || a.switcher.running != "" || a.pull.running || !available && !completed
	if a.review.stale && !available {
		label, tooltip = "Review", "Nothing to review"
	}
	ui.Button(c.Key("review"), label).Shrink(0).Disabled(disabled).Tooltip(tooltip).OnClick(func() {
		if completed {
			a.review.open = true
			return
		}
		a.startReview(services)
	})
}

func (a *app) startReview(services ui.Services) {
	if a.review.running || a.push.running || a.switcher.running != "" || a.pull.running {
		return
	}
	if a.reviewSourceErr != "" {
		a.review = reviewState{err: a.reviewSourceErr, open: true}
		return
	}
	if a.reviewSource.ID == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	snapshot, repo := a.reviewSource, a.repo
	a.review = reviewState{running: true, cancel: cancel, sourceID: snapshot.ID, scope: snapshot.Scope, head: snapshot.Head, base: snapshot.Base,
		phase: "Checking…", detail: "Checking the review snapshot"}
	win, inline := a.win, a.inline
	progress := func(phase, detail string) {
		if !inline {
			win.Update(func() {
				if ctx.Err() == nil && a.review.running && !a.review.stale && a.review.sourceID == snapshot.ID {
					a.review.phase, a.review.detail = phase, detail
				}
			})
		}
	}
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
			a.review.running, a.review.cancel = false, nil
			if errors.Is(err, review.ErrChanged) || a.reviewSource.ID != snapshot.ID {
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
	t := c.Theme()
	services := c.Services()
	sheet(c, &a.review.open, a.review.title(), 620, 480, func() {
		ui.Text(c, a.review.title()).FontWeight(600).FontSize(fontSize + 3).Shrink(0)
		ui.Text(c, a.review.scope).TextColor(t.TextMuted).Shrink(0)
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(3)).Children(func() {
				if a.review.stale {
					ui.Text(c, "This review is out of date. Review the current changes again.")
				}
				if a.review.err != "" {
					ui.Text(c, a.review.err).Selectable()
				}
				if result := a.review.result; result != nil {
					ui.Text(c, result.Summary).Selectable()
					for i, f := range result.Findings {
						ui.Column(c).Key(i).FillWidth().Gap(t.Space(1)).Children(func() {
							ui.Text(c, fmt.Sprintf("[P%d] %s", f.Priority, f.Title)).FontWeight(600).Selectable()
							ui.Text(c, fmt.Sprintf("%s:%d", f.Path, f.Line)).TextColor(t.TextMuted).Selectable()
							ui.Text(c, f.Body).Selectable()
						})
					}
				}
			})
		})
	}, func() {
		// Copy apart at the start, as a sheet's secondary action, and the
		// default button last.
		ui.Button(c.Key("copy-review"), "Copy").OnClick(func() { services.WriteClipboard(a.review.text()) })
		ui.Spacer(c)
		ui.Button(c.Key("review-again"), "Review Again").Disabled(a.push.running || a.review.running || a.pull.running || a.reviewSource.ID == "").
			OnClick(func() { a.startReview(services) })
	})
}
