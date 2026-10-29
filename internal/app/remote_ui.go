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

// fetchTick is how often a project asks whether its remote is due to be
// fetched (fetchIfDue).
const fetchTick = 30 * time.Second

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

// fetchIfDue fetches the remote where the interval the user chose has
// passed since it last did, as the user and the network allow, and reads
// the checks of the branch's pull request again while they run.
func (a *app) fetchIfDue() {
	if a.closed || !a.projects.fetchAllowed() {
		return
	}
	if a.pullReq != nil && a.pullReq.State == "OPEN" {
		if s, ok := a.pullReq.ChecksState(); ok && s == git.CheckPending {
			a.readPullRequest(true)
		}
	}
	if time.Since(a.fetchedAt) >= a.projects.fetchEvery() {
		a.fetchRemote()
	}
}

// fetchRemote fetches the remote the branch tracks, unless git is busy
// with the branch meanwhile, and reads the history again where the
// upstream moved. Failures, as offline, pass quietly until the next.
func (a *app) fetchRemote() {
	if a.pull.fetching || a.pull.running || a.push.running || a.switcher.running != "" {
		return
	}
	a.pull.fetching = true
	a.fetchedAt = time.Now()
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		moved, _ := git.FetchUpstream(ctx, repo)
		return func() {
			a.pull.fetching = false
			a.readPullRequest(true) // a pull request opened since
			if moved {
				a.refresh()
			}
		}
	})
}

// githubPage is the page the GitHub button opens: the branch's pull
// request, else the branch's page, else the repository's.
func (a *app) githubPage() string {
	if a.pullReq != nil && a.pullFor == a.branchURL {
		return a.pullReq.URL
	}
	return cmp.Or(a.branchURL, a.github)
}

// readPullRequest asks gh for the branch's pull request where the branch
// moved to another page since, or always with again. Only a branch of
// its own on GitHub, not the default one, has one. What gh cannot tell,
// as offline, leaves the pull request as last read.
func (a *app) readPullRequest(again bool) {
	page := a.branchURL
	if page == a.pullFor && !again {
		return
	}
	if !strings.Contains(page, "/tree/") {
		a.pullReq, a.pullFor = nil, page
		return
	}
	if page != a.pullFor {
		a.pullReq, a.pullFor = nil, page
	}
	if a.pullReading {
		return
	}
	a.pullReading = true
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pr, err := git.BranchPullRequest(ctx, repo)
		return func() {
			a.pullReading = false
			switch {
			case a.branchURL != page:
				a.readPullRequest(true) // the branch's now, which waited
			case err == nil:
				a.pullReq = pr
			}
		}
	})
}
