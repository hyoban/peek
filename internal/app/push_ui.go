package app

import (
	"context"
	"peek/internal/codex"
	"peek/internal/git"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type pushState struct {
	running bool
	failed  bool
	status  string
	cancel  context.CancelFunc
	until   time.Time // when the success label returns to Ship
}

func (a *app) pushButton(c *ui.Context) {
	if !a.push.running && !a.push.failed && a.push.status != "" {
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
	switch {
	case a.push.running:
		label = "Shipping…"
		if phase, _, ok := strings.Cut(a.push.status, " "); ok {
			label = phase + "…"
		}
		tooltip = a.push.status + "\nClick to cancel"
	case a.push.failed:
		label, tooltip = "Ship failed", a.push.status
		if a.canPush {
			tooltip += "\nClick to retry"
		}
	case a.push.status != "":
		label, tooltip = "Shipped", a.push.status
	}
	checkingOut := a.switcher.running != "" && !a.push.running
	pulling := a.pull.running && !a.push.running
	disabled := a.review.running || checkingOut || pulling || (!a.canPush && !a.push.running)
	switch {
	case checkingOut:
		tooltip = "Checking out " + a.switcher.running + "…"
	case pulling:
		tooltip = "Pulling " + a.upstream + "…"
	case disabled && a.push.status == "":
		tooltip = "Nothing to ship"
	}
	ui.Button(c.Key("push"), label).Shrink(0).Disabled(disabled).Tooltip(tooltip).OnClick(func() {
		if a.push.running {
			a.push.cancel()
			return
		}
		a.startPush()
	})
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
		return func() {
			a.push.running, a.push.cancel = false, nil
			a.push.status, a.push.failed = status, err != nil
			if err != nil {
				a.push.status = err.Error()
			} else {
				a.canPush = false
			}
			a.refresh()
		}
	})
}
