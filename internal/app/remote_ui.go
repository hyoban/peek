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
}

// pullButton tells of the commits the branch's upstream has and the
// branch has not, beside its name, as last fetched; a click brings them
// in by fast-forward.
func (a *app) pullButton(c *ui.Context) {
	if a.behind == 0 && !a.pull.running {
		return
	}
	t := c.Theme()
	tooltip := "Pull " + count(a.behind, "new commit") + " from " + a.upstream + ", fast-forward only"
	if a.pull.running {
		tooltip = "Pulling " + a.upstream + "…"
	}
	busy := a.push.running || a.review.running || a.switcher.running != ""
	b := ui.ButtonBase(c.Key("pull")).Shrink(0).Padding(t.Space(0.5), t.Space(1.5)).Radius(t.Radius).
		TextColor(t.Accent).Label("Pull").Tooltip(tooltip).Disabled(busy || a.pull.running)
	flat(b, t, false)
	b.OnClick(a.startPull)
	b.Children(func() {
		if a.pull.running {
			ui.Spinner(c).Size(12, 12).Label("Pulling")
			return
		}
		ui.Row(c).Gap(t.Space(0.5)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, pullArrow).Size(11, 11).Shrink(0)
			ui.Text(c, strconv.Itoa(a.behind)).FontSize(smallSize).FontWeight(600).SingleLine()
		})
	})
}

func (a *app) startPull() {
	if a.pull.running || a.push.running || a.review.running || a.switcher.running != "" {
		return
	}
	a.pull.running = true
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		err := git.PullUpstream(ctx, repo)
		return func() {
			a.pull.running = false
			if err != nil {
				a.showAlert("Couldn’t Pull "+a.upstream, err.Error())
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
