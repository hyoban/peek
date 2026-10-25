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
	phase, detail        string
	started              time.Time
	result               *review.Result
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
	label, tooltip := "Review", "Review changes with Codex"
	elapsedText := ""
	switch {
	case a.review.running:
		label, tooltip = "Reviewing…", "Codex is reviewing changes. Click to cancel."
		if a.review.phase != "" {
			if a.review.started.IsZero() {
				a.review.started = c.Now()
			}
			elapsed := c.Now().Sub(a.review.started).Truncate(time.Second)
			elapsedText = elapsed.String()
			label = fmt.Sprintf("%s %s", a.review.phase, elapsed)
			tooltip = a.review.detail + "\nElapsed: " + elapsed.String() + "\nClick to cancel"
			c.After(time.Second)
		}
	case a.review.stale:
		label, tooltip = "Review again", "Changes have changed since the last review. Click to review again."
	case a.review.err != "":
		label, tooltip = "Review error", a.review.err+"\nClick for details"
	case a.review.result != nil:
		label = "Passed"
		tooltip = a.review.result.Summary
		if n := len(a.review.result.Findings); n > 0 && a.review.result.Verdict != "failed" {
			tooltip += "\n" + count(n, "minor finding")
		}
		if a.review.result.Verdict == "failed" {
			label = "Not passed"
			tooltip += "\nReview copied to clipboard"
		}
		tooltip += "\nClick for details"
	case a.reviewSourceErr != "":
		tooltip = a.reviewSourceErr
	case a.reviewSource.ID == "":
		tooltip = "Nothing to review"
	default:
		tooltip = "Review " + a.reviewSource.Scope + " with Codex"
	}
	available := a.reviewSource.ID != "" || a.reviewSourceErr != ""
	completed := !a.review.stale && (a.review.result != nil || a.review.err != "")
	disabled := a.push.running || (!a.review.running && (a.switcher.running != "" || a.pull.running || !available && !completed))
	if a.review.stale && !available && !a.review.running {
		label, tooltip = "Review", "Nothing to review"
	}
	ui.Button(c.Key("review"), "").Label(label).Shrink(1).MinWidth(0).Disabled(disabled).Tooltip(tooltip).
		Children(func() {
			if elapsedText != "" {
				ui.Row(c).Gap(4).Shrink(1).MinWidth(0).Children(func() {
					ui.Text(c, a.review.phase).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					ui.Text(c, elapsedText).SingleLine().Shrink(0)
				})
			} else {
				ui.Text(c, label).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			}
		}).OnClick(func() {
		switch {
		case a.review.running:
			a.review.cancel()
		case a.review.stale:
			a.startReview(services)
		case a.review.result != nil || a.review.err != "":
			a.review.open = true
		default:
			a.startReview(services)
		}
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
	a.review = reviewState{running: true, cancel: cancel, sourceID: snapshot.ID, scope: snapshot.Scope,
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
				if result.Verdict == "failed" {
					services.WriteClipboard("Review not passed\n" + snapshot.Scope + "\n\n" + result.Text())
				}
			}
			a.refresh()
		}
	})
}

func (a *app) reviewDetails(c *ui.Context) {
	t := c.Theme()
	services := c.Services()
	ui.Modal(c.Key("review-details"), &a.review.open, func() {
		title := "Review error"
		if a.review.result != nil {
			title = "Review passed"
			if a.review.result.Verdict == "failed" {
				title = "Review not passed"
			}
		}
		ui.Text(c, title).FontWeight(600).FontSize(fontSize + 3).Shrink(0)
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
		ui.Row(c).FillWidth().Shrink(0).Justify(ui.End).Gap(t.Space(2)).Children(func() {
			ui.Button(c.Key("review-again"), "Review again").Disabled(a.push.running || a.review.running || a.pull.running || a.reviewSource.ID == "").
				OnClick(func() { a.startReview(services) })
			ui.Button(c.Key("close-review"), "Close").OnClick(func() { a.review.open = false })
		})
	}).Width(620).Height(480)
}
