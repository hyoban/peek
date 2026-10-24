package main

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

type pushState struct {
	running bool
	failed  bool
	status  string
	cancel  context.CancelFunc
}

func (a *app) pushButton(c *ui.Context) {
	label, tooltip := "Push", "Commit all changes with Codex and push"
	switch {
	case a.push.running:
		label = "Pushing…"
		if phase, _, ok := strings.Cut(a.push.status, " "); ok {
			label = phase + "…"
		}
		tooltip = a.push.status + "\nClick to cancel"
	case a.push.failed:
		label, tooltip = "Push failed", a.push.status+"\nClick to retry"
	case a.push.status != "":
		label, tooltip = "Pushed", a.push.status+"\nClick to push again"
	}
	ui.Button(c.Key("push"), label).Shrink(0).Tooltip(tooltip).OnClick(func() {
		if a.push.running {
			a.push.cancel()
			return
		}
		a.startPush()
	})
}

func (a *app) startPush() {
	if a.push.running {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	a.push = pushState{running: true, status: "Preparing to push…", cancel: cancel}
	repo, win, inline := a.repo, a.win, a.inline
	progress := func(status string) {
		if !inline {
			win.Update(func() { a.push.status = status })
		}
	}
	a.background(func() func() {
		defer cancel()
		status, err := pushRepository(ctx, repo, codexCommitMessage, progress)
		return func() {
			a.push.running, a.push.cancel = false, nil
			a.push.status, a.push.failed = status, err != nil
			if err != nil {
				a.push.status = err.Error()
			}
			a.refresh()
		}
	})
}
