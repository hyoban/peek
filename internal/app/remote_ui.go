package app

import (
	"cmp"
	"context"
	"peek/internal/git"
	"strconv"
	"strings"
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
			a.readPullURL(true) // a pull request opened since
			if moved {
				a.refresh()
			}
		}
	})
}

// githubPage is the page the GitHub button opens: the branch's pull
// request, else the branch's page, else the repository's.
func (a *app) githubPage() string {
	if a.pullURLFor == a.branchURL {
		return cmp.Or(a.pullURL, a.branchURL, a.github)
	}
	return cmp.Or(a.branchURL, a.github)
}

// readPullURL asks gh for the branch's pull request where the branch
// moved to another page since, or always with again. Only a branch of
// its own on GitHub, not the default one, has one.
func (a *app) readPullURL(again bool) {
	page := a.branchURL
	if page == a.pullURLFor && !again {
		return
	}
	if !strings.Contains(page, "/tree/") {
		a.pullURL, a.pullURLFor = "", page
		return
	}
	if page != a.pullURLFor {
		a.pullURL, a.pullURLFor = "", page
	}
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		url := git.BranchPullRequestURL(ctx, repo)
		return func() {
			if a.branchURL == page {
				a.pullURL, a.pullURLFor = url, page
			}
		}
	})
}
