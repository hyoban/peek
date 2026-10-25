package app

import (
	"context"
	"peek/internal/git"
	"strconv"
	"time"

	"github.com/egoist/mygo/ui"
)

// fetchEvery is how often the remote the branch tracks is fetched while
// its project shows, for the toolbar to tell of new commits there.
const fetchEvery = 3 * time.Minute

type pullState struct {
	fetching bool
	running  bool
	err      string // why the last pull failed, "" for none
}

// pullButton tells of the commits the branch's upstream has and the
// branch has not, beside its name, as last fetched; a click brings them
// in by fast-forward.
func (a *app) pullButton(c *ui.Context) {
	if a.behind == 0 && a.pull.err == "" && !a.pull.running {
		return
	}
	t := c.Theme()
	label := "↓ " + strconv.Itoa(a.behind)
	tooltip := count(a.behind, "new commit") + " on " + a.upstream + "\nClick to pull (fast-forward only)"
	color := t.Accent
	switch {
	case a.pull.running:
		tooltip = "Pulling " + a.upstream + "…"
	case a.pull.err != "":
		tooltip, color = "Pull failed: "+a.pull.err+"\nClick to retry", system(t, systemRed)
		if a.behind == 0 {
			label = "↓"
		}
	}
	busy := a.push.running || a.review.running || a.switcher.running != ""
	b := ui.ButtonBase(c.Key("pull")).Shrink(0).Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Radius).
		TextColor(color).Label("Pull").Tooltip(tooltip).Disabled(busy || a.pull.running)
	switch {
	case b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
	b.OnClick(a.startPull)
	b.Children(func() {
		if a.pull.running {
			ui.Spinner(c).Size(12, 12).Label("Pulling")
			return
		}
		ui.Text(c, label).FontSize(smallSize).FontWeight(600).SingleLine()
	})
}

func (a *app) startPull() {
	if a.pull.running || a.push.running || a.review.running || a.switcher.running != "" {
		return
	}
	a.pull.running, a.pull.err = true, ""
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := git.PullUpstream(ctx, repo)
		return func() {
			a.pull.running = false
			if err != nil {
				a.pull.err = err.Error()
			}
			a.refresh()
		}
	})
}

// fetchRemote fetches the remote the branch tracks, unless git is busy
// with the branch meanwhile, and reads the history again where the
// upstream moved. Failures, as offline, pass quietly until the next.
func (a *app) fetchRemote() {
	if a.pull.fetching || a.pull.running || a.push.running || a.switcher.running != "" {
		return
	}
	a.pull.fetching = true
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		moved, _ := git.FetchUpstream(ctx, repo)
		return func() {
			a.pull.fetching = false
			if moved {
				a.refresh()
			}
		}
	})
}
