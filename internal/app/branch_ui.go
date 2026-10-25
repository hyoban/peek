package app

import (
	"context"
	"fmt"
	"peek/internal/git"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// switcherState is the branch switcher's: a popover under the branch's
// name listing the branches and the open pull requests to check out, and
// a field to filter them, or to name a pull request by number or page.
type switcherState struct {
	open     bool
	query    string
	gen      int // counts the openings, to drop stale lists
	branches git.Branches
	listed   bool // the branches are read
	prs      []git.PullRequest
	prRepo   string // the page of the repository they are of, "" until known
	prsRead  bool
	prErr    string
	running  string // what is checked out meanwhile, "" for nothing
	err      string
	field    ui.Handle // the query's, which takes the keys as it opens
	button   ui.Handle // the branch's name, under which the switcher shows
}

// switchTarget is a row of the switcher: a branch, local or remote, or a
// pull request of the repository on GitHub whose page prRepo is; or a
// branch of another work tree, which opens there.
type switchTarget struct {
	branch   string
	remote   bool
	pr       int
	prRepo   string
	worktree string
}

func (s switchTarget) String() string {
	if s.pr > 0 {
		return "#" + strconv.Itoa(s.pr)
	}
	return s.branch
}

// branchLabel is the branch checked out, muted beside the project's name,
// which gives way to it when the toolbar is narrow; a click on it lists
// the branches and pull requests to switch to.
func (a *app) branchLabel(c *ui.Context) {
	if a.branch == "" {
		return
	}
	t := c.Theme()
	b := ui.ButtonBase(c.Key("branch")).Bind(&a.switcher.button).Shrink(2).MinWidth(0).Padding(t.Space(0.5), t.Space(1.5)).
		Radius(t.Radius).TextColor(t.TextMuted).Label("Switch Branch").Tooltip(a.branch).
		Disabled(a.push.running || a.review.running || a.pull.running)
	flat(b, t, a.switcher.open)
	b.OnClick(func() {
		if a.switcher.open {
			a.switcher.open = false
			return
		}
		a.openSwitcher()
		a.switcher.field.Focus(c)
	})
	b.Children(func() {
		ui.Row(c).MinWidth(0).Gap(t.Space(1)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, branchIcon).Size(12, 12).Shrink(0)
			ui.Text(c, a.branch).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
		})
	})
}

// branchSwitcher shows the switcher under the branch's name, out of the
// toolbar, whose keys move among its controls alone.
func (a *app) branchSwitcher(c *ui.Context) {
	if anchor := c.Resolve(a.switcher.button); anchor.Valid() {
		ui.Popover(c.Key("branches"), anchor, &a.switcher.open, func() { a.switcherPanel(c) })
	}
}

// openSwitcher shows the switcher and reads again what it lists: the
// branches at once, the pull requests as GitHub answers.
func (a *app) openSwitcher() {
	s := &a.switcher
	s.open, s.query, s.err = true, "", ""
	s.gen++
	gen, repo := s.gen, a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		b, err := git.ListBranches(ctx, repo)
		return func() {
			if s.gen != gen {
				return
			}
			s.branches, s.listed = b, true
			if err != nil {
				s.err = err.Error()
			}
		}
	})
	if a.github == "" {
		s.prs, s.prsRead, s.prErr = nil, true, ""
		return
	}
	s.prsRead = s.prs != nil // the last ones show until the new come
	github := a.github
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		// One repository for the list, the links and the checkouts, as a
		// fork's gh may take its upstream's.
		page := git.PullRepository(ctx, repo, github)
		prs, err := git.ListPullRequests(ctx, repo, page)
		return func() {
			if s.gen != gen {
				return
			}
			s.prsRead, s.prErr, s.prRepo = true, "", page
			if err != nil {
				s.prErr = err.Error()
				return
			}
			s.prs = prs
		}
	})
}

// matches are the rows the query keeps: the branches and pull requests
// it is part of, and the pull request it names first, where it names one.
func (a *app) matches() (local, remote []string, prs []git.PullRequest, named int) {
	s := &a.switcher
	q := strings.ToLower(strings.TrimSpace(s.query))
	has := func(text string) bool { return strings.Contains(strings.ToLower(text), q) }
	for _, b := range s.branches.Local {
		if has(b) {
			local = append(local, b)
		}
	}
	for _, b := range s.branches.Remote {
		if has(b) {
			remote = append(remote, b)
		}
	}
	if n, ok := git.ParsePullRequest(s.query, s.prRepo); ok && s.prRepo != "" {
		named = n
	}
	for _, pr := range s.prs {
		if pr.Number == named || (named == 0 && (has(pr.Title) || has(pr.Branch) || has(pr.Author.Login))) {
			prs = append(prs, pr)
		}
	}
	return local, remote, prs, named
}

// submitSwitch checks out what Enter in the field picks: the pull request
// the query names, else the first row.
func (a *app) submitSwitch() {
	local, remote, prs, named := a.matches()
	switch {
	case named > 0:
		a.switchTo(a.pullTarget(named))
	case len(local) > 0:
		a.switchTo(a.localTarget(local[0]))
	case len(remote) > 0:
		a.switchTo(switchTarget{branch: remote[0], remote: true})
	case len(prs) > 0:
		a.switchTo(a.pullTarget(prs[0].Number))
	}
}

// localTarget is the local branch's row: the work tree it is checked out
// in, where another has it.
func (a *app) localTarget(branch string) switchTarget {
	if branch == a.branch {
		return switchTarget{branch: branch}
	}
	return switchTarget{branch: branch, worktree: a.switcher.branches.Worktrees[branch]}
}

// pullTarget is the row of a pull request of the repository listed.
func (a *app) pullTarget(number int) switchTarget {
	return switchTarget{pr: number, prRepo: a.switcher.prRepo}
}

// switchTo checks out the branch or the pull request, closing the
// switcher once it is, or showing in it why not. A branch of another
// work tree opens that work tree as a project instead.
func (a *app) switchTo(target switchTarget) {
	s := &a.switcher
	if target.worktree != "" {
		if s.running == "" && a.projects != nil {
			s.open = false
			a.projects.open(target.worktree)
		}
		return
	}
	if s.running != "" || a.push.running || a.review.running || a.pull.running || (target.pr == 0 && target.branch == a.branch) {
		if target.branch == a.branch {
			s.open = false
		}
		return
	}
	s.running, s.err = target.String(), ""
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		var err error
		if target.pr > 0 {
			err = git.CheckoutPullRequest(ctx, repo, target.prRepo, target.pr)
		} else {
			err = git.SwitchBranch(ctx, repo, target.branch, target.remote)
		}
		// The branch as the checkout left it, for the next choice to see
		// before the history is read again.
		branch := git.CurrentBranch(repo)
		return func() {
			s.running = ""
			if branch != "" {
				a.branch = branch
			}
			if err != nil {
				s.err, s.open = err.Error(), true // the reason shows where it was asked
			} else {
				s.open = false
			}
			a.refresh()
		}
	})
}

// switcherPanel is the switcher's content: the field, then the local
// branches, the remote ones and the open pull requests, and a note of
// what is checked out or why it was not.
func (a *app) switcherPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.switcher
	ui.Column(c).Width(380).Gap(t.Space(1)).Children(func() {
		ui.TextInput(c.Key("branch-query"), &s.query).Bind(&s.field).Label("Branch or pull request").
			Placeholder("Branch, or pull request number or link").OnSubmit(a.submitSwitch)
		local, remote, prs, named := a.matches()
		ui.Scroll(c.Key("branch-list")).FillWidth().MaxHeight(380).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				if !s.listed {
					ui.Row(c).FillWidth().Justify(ui.Center).Padding(t.Space(2)).Children(func() { ui.Spinner(c).Label("Loading") })
				}
				if len(local) > 0 {
					switcherHeading(c, "Branches")
				}
				for _, b := range local {
					a.switcherRow(c, a.localTarget(b), branchIcon, b, "")
				}
				if len(remote) > 0 {
					switcherHeading(c, "Remote Branches")
				}
				for _, b := range remote {
					a.switcherRow(c, switchTarget{branch: b, remote: true}, branchIcon, b, "")
				}
				if a.github == "" {
					return
				}
				switcherHeading(c, "Pull Requests")
				listedNamed := false
				for _, pr := range prs {
					listedNamed = listedNamed || pr.Number == named
					detail := pr.Branch
					if pr.Draft {
						detail = "Draft · " + detail
					}
					a.switcherRow(c, a.pullTarget(pr.Number), pullIcon, fmt.Sprintf("#%d %s", pr.Number, pr.Title), detail)
				}
				switch {
				case named > 0 && !listedNamed:
					a.switcherRow(c, a.pullTarget(named), pullIcon, fmt.Sprintf("Check out pull request #%d", named), "")
				case !s.prsRead:
					ui.Row(c).FillWidth().Justify(ui.Center).Padding(t.Space(2)).Children(func() { ui.Spinner(c).Label("Loading pull requests") })
				case s.prErr != "":
					switcherNote(c, s.prErr)
				case len(prs) == 0:
					switcherNote(c, "No open pull requests")
				}
			})
		})
		switch {
		case s.running != "":
			ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Center).Padding(t.Space(1), t.Space(2)).Children(func() {
				ui.Spinner(c).Size(12, 12).Label("Checking out")
				ui.Text(c, "Checking out "+s.running+"…").FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
			})
		case s.err != "":
			ui.Text(c, s.err).FontSize(smallSize).TextColor(system(t, systemRed)).Selectable().
				Padding(t.Space(1), t.Space(2)).FillWidth()
		}
	})
}

func switcherHeading(c *ui.Context, title string) {
	t := c.Theme()
	ui.Text(c, title).FontSize(smallSize).FontWeight(600).TextColor(t.TextMuted).SingleLine().
		Padding(t.Space(1.5), t.Space(2), t.Space(0.5), t.Space(2))
}

func switcherNote(c *ui.Context, note string) {
	t := c.Theme()
	ui.Text(c, note).FontSize(smallSize).TextColor(t.TextMuted).Padding(t.Space(1), t.Space(2)).FillWidth()
}

// switcherRow is a row to check out, the branch checked out marked, and
// those of other work trees.
func (a *app) switcherRow(c *ui.Context, target switchTarget, svg *ui.SVG, title, detail string) {
	t := c.Theme()
	key := "branch:" + target.branch
	if target.pr > 0 {
		key = "pr:" + strconv.Itoa(target.pr)
	}
	current := target.pr == 0 && !target.remote && target.branch == a.branch
	tooltip := title
	if target.worktree != "" {
		detail, tooltip = "Worktree", title+"\nOpen the worktree at "+target.worktree
	}
	b := ui.ButtonBase(c.Key(key)).FillWidth().Padding(t.Space(1), t.Space(2)).Radius(t.Radius).
		Label(title).Tooltip(tooltip).Disabled(a.switcher.running != "" || a.pull.running)
	flat(b, t, false)
	b.OnClick(func() { a.switchTo(target) })
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, svg).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
			weight := 400
			if current {
				weight = 600
			}
			ui.Text(c, title).FontWeight(weight).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
			if detail != "" {
				ui.Text(c, detail).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0).MaxWidth(150)
			}
			if current {
				ui.Icon(c, checkIcon).Size(12, 12).Shrink(0).TextColor(t.Accent)
			}
		})
	})
}
