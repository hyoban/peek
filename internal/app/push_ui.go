package app

import (
	"context"
	"peek/internal/codex"
	"peek/internal/git"
	"time"

	"github.com/egoist/mygo/ui"
)

type pushState struct {
	running bool
	status  string
	cancel  context.CancelFunc
	until   time.Time // when the success label returns to Ship
}

func (a *app) pushButton(c *ui.Context) { drawTool(c, "push", a.pushTool(c)) }

// pushTool is the Ship button as the push's state has it: Shipping… for
// as long as it runs, its stage in the tooltip, for the button to keep
// its width as the stages, most of them a moment's, pass.
func (a *app) pushTool(c *ui.Context) toolButton {
	if a.push.running {
		return toolButton{label: "Shipping…", tooltip: a.push.status, busy: true, stop: "Stop Shipping", cancel: func() { a.push.cancel() }}
	}
	if a.push.status != "" {
		if a.push.until.IsZero() {
			a.push.until = c.Now().Add(3 * time.Second)
		}
		if c.Now().Before(a.push.until) {
			c.After(a.push.until.Sub(c.Now()))
		} else {
			a.push.status = ""
			a.push.until = time.Time{}
		}
	}
	label, tooltip := "Ship", "Commit all changes with Codex and push"
	if a.push.status != "" {
		label, tooltip = "Shipped", a.push.status
	}
	checkingOut := a.switcher.running != ""
	disabled := a.review.running || checkingOut || a.pull.running || !a.canPush
	switch {
	case checkingOut:
		tooltip = "Checking out " + a.switcher.running + "…"
	case a.pull.running:
		tooltip = "Pulling " + a.upstream + "…"
	case disabled && a.push.status == "":
		tooltip = "Nothing to ship"
	}
	return toolButton{label: label, tooltip: tooltip, disabled: disabled, click: a.startPush}
}

func (a *app) startPush() {
	if a.push.running || a.review.running || a.switcher.running != "" || a.pull.running || !a.canPush {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.push = pushState{running: true, status: "Preparing to ship…", cancel: cancel}
	repo, win, inline := a.repo, a.win, a.inline
	progress := func(status string) {
		if !inline {
			win.Update(func() { a.push.status = status })
		}
	}
	a.background(func() func() {
		defer cancel()
		status, err := git.Push(ctx, repo, codex.CommitMessage, progress)
		stopped := ctx.Err() == context.Canceled // before the deferred cancel
		return func() {
			a.push.running, a.push.cancel = false, nil
			switch {
			case stopped:
				a.push.status = "" // stopped, as asked
			case err != nil:
				a.push.status = ""
				a.showAlert("Couldn’t Ship", err.Error())
				a.notify("Couldn’t Ship", err.Error(), nil)
			default:
				a.push.status, a.canPush = status, false
				a.notify("Shipped", status, nil)
			}
			a.refresh()
		}
	})
}
