package main

import (
	"context"
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
	label := "Push"
	if a.push.running {
		label = "Pushing…"
	}
	ui.Button(c.Key("push"), label).Shrink(0).Disabled(a.push.running).
		Tooltip("Commit all changes with Codex and push").OnClick(a.startPush)
}

func (a *app) pushStatus(c *ui.Context) {
	if a.push.status == "" {
		return
	}
	t := c.Theme()
	ui.Row(c).Key("push-status").FillWidth().Shrink(0).AlignItems(ui.Center).
		Padding(t.Space(1), t.Space(3)).Gap(t.Space(2)).Background(t.Surface).Children(func() {
		label := a.push.status
		if a.push.failed {
			label = "Push failed: " + label
		}
		ui.Text(c, label).SingleLine().Ellipsis("…").Grow(1).MinWidth(0).Tooltip(label)
		if a.push.running {
			ui.Button(c.Key("cancel-push"), "Cancel").OnClick(func() { a.push.cancel() })
		} else {
			ui.Button(c.Key("dismiss-push"), "Dismiss").OnClick(func() { a.push.status = "" })
		}
	})
	ui.Divider(c)
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
