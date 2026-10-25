package app

import (
	"context"
	"fmt"
	"peek/internal/git"
	"slices"
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
	focus    bool      // the field is to take the keys, as the menu bar opened the switcher
	// moved is the row the arrows or the pointer moved to, for the query
	// movedFor; reveal scrolls it into view.
	moved    int
	movedFor string
	reveal   bool
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
	b := ui.ButtonBase(c.Key("branch")).Bind(&a.switcher.button).Shrink(1).MinWidth(0).Padding(t.Space(0.5), t.Space(1.5)).
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
	if a.switcher.focus {
		a.switcher.focus = false
		a.switcher.field.Focus(c)
	}
	if anchor := c.Resolve(a.switcher.button); anchor.Valid() {
		ui.Popover(c.Key("branches"), anchor, &a.switcher.open, func() { a.switcherPanel(c) })
	}
}

// openSwitcher shows the switcher and reads again what it lists: the
// branches at once, the pull requests as GitHub answers.
func (a *app) openSwitcher() {
	s := &a.switcher
	s.open, s.query, s.err = true, "", ""
	s.moved, s.movedFor = 0, "\x00" // none moved to yet
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

// switchRow is a row of the switcher, under its heading.
type switchRow struct {
	target        switchTarget
	heading       string // the section it starts, "" for none
	icon          *ui.SVG
	title, detail string
}

// rows lists what the query keeps, in the switcher's order: the local
// branches, the remote ones, the open pull requests, and the pull request
// the query names where none listed is it. preferred is the row Enter
// picks before the arrows move: the pull request named, else the first.
func (a *app) rows() (rows []switchRow, preferred int) {
	local, remote, prs, named := a.matches()
	heading := func(title string) func() string {
		first := true
		return func() string {
			if first {
				first = false
				return title
			}
			return ""
		}
	}
	h := heading("Branches")
	for _, b := range local {
		rows = append(rows, switchRow{target: a.localTarget(b), heading: h(), icon: branchIcon, title: b})
	}
	h = heading("Remote Branches")
	for _, b := range remote {
		rows = append(rows, switchRow{target: switchTarget{branch: b, remote: true}, heading: h(), icon: branchIcon, title: b})
	}
	if a.github == "" {
		return rows, 0
	}
	h = heading("Pull Requests")
	listedNamed := false
	for _, pr := range prs {
		if pr.Number == named {
			listedNamed, preferred = true, len(rows)
		}
		detail := pr.Branch
		if pr.Draft {
			detail = "Draft · " + detail
		}
		rows = append(rows, switchRow{target: a.pullTarget(pr.Number), heading: h(), icon: pullIcon,
			title: fmt.Sprintf("#%d %s", pr.Number, pr.Title), detail: detail})
	}
	if named > 0 && !listedNamed {
		preferred = len(rows)
		rows = append(rows, switchRow{target: a.pullTarget(named), heading: h(), icon: pullIcon,
			title: fmt.Sprintf("Check out pull request #%d", named)})
	}
	return rows, preferred
}

// submitSwitch checks out the row highlighted, as Enter in the field does.
func (a *app) submitSwitch() {
	rows, preferred := a.rows()
	i := a.highlighted(len(rows), preferred)
	if i >= 0 {
		a.switchTo(rows[i].target)
	}
}

// highlighted is the row the arrows have moved to for the query, else the
// preferred one; -1 for no rows.
func (a *app) highlighted(n, preferred int) int {
	s := &a.switcher
	if s.movedFor != s.query || s.moved >= n {
		return min(preferred, n-1)
	}
	return s.moved
}

// moveHighlight moves the highlight by delta rows, round the ends, as the
// arrows in the field ask.
func (a *app) moveHighlight(delta int) {
	s := &a.switcher
	rows, preferred := a.rows()
	n := len(rows)
	if n == 0 {
		return
	}
	s.moved, s.movedFor = (a.highlighted(n, preferred)+delta+n)%n, s.query
	s.reveal = true
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
// what is checked out or why it was not. As a menu, the row under the
// pointer or that the arrows move to is highlighted, which Enter checks
// out.
func (a *app) switcherPanel(c *ui.Context) {
	t := c.Theme()
	s := &a.switcher
	ui.Column(c).Width(380).Gap(t.Space(1)).Children(func() {
		ui.TextInput(c.Key("branch-query"), &s.query).Bind(&s.field).Label("Branch or pull request").
			Placeholder("Branch, or pull request number or link").OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Mods == 0 && k.Key == ui.KeyDown:
				a.moveHighlight(1)
			case k.Mods == 0 && k.Key == ui.KeyUp:
				a.moveHighlight(-1)
			case k.Mods == 0 && k.Key == ui.KeyEnter:
				a.submitSwitch()
			default:
				return false
			}
			return true
		})
		rows, preferred := a.rows()
		lit := a.highlighted(len(rows), preferred)
		ui.Scroll(c.Key("branch-list")).FillWidth().MaxHeight(380).Children(func() {
			ui.Column(c).FillWidth().Gap(1).Children(func() {
				if !s.listed {
					ui.Row(c).FillWidth().Justify(ui.Center).Padding(t.Space(2)).Children(func() { ui.Spinner(c).Label("Loading") })
				}
				for i, row := range rows {
					if row.heading != "" {
						switcherHeading(c, row.heading)
					}
					if a.switcherRow(c, row, i == lit) {
						s.moved, s.movedFor = i, s.query
					}
				}
				if a.github == "" {
					return
				}
				if !slices.ContainsFunc(rows, func(r switchRow) bool { return r.target.pr > 0 }) {
					switcherHeading(c, "Pull Requests")
				}
				_, _, prs, named := a.matches()
				switch {
				case named > 0:
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
			ui.Text(c, s.err).FontSize(smallSize).TextColor(system(c, systemRed)).Selectable().
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
// those of other work trees; lit, in the accent as a menu's highlighted
// item. It reports whether the pointer is on it, which highlights it.
func (a *app) switcherRow(c *ui.Context, row switchRow, lit bool) (hovered bool) {
	t := c.Theme()
	target, title, detail := row.target, row.title, row.detail
	key := "branch:" + target.branch
	if target.pr > 0 {
		key = "pr:" + strconv.Itoa(target.pr)
	}
	current := target.pr == 0 && !target.remote && target.branch == a.branch
	tooltip := title
	if target.worktree != "" {
		detail, tooltip = "Worktree", title+"\nOpens the worktree at "+target.worktree
	}
	text, muted := t.Text, t.TextMuted
	b := ui.ButtonBase(c.Key(key)).FillWidth().Padding(t.Space(1), t.Space(2)).Radius(t.Radius).
		Label(title).Tooltip(tooltip).Disabled(a.switcher.running != "" || a.pull.running)
	if lit {
		text, muted = t.AccentText, t.AccentText.Alpha(0.72)
		b.Background(t.Accent)
		if a.switcher.reveal {
			a.switcher.reveal = false
			b.ScrollIntoView()
		}
	}
	b.OnClick(func() { a.switchTo(target) })
	b.Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, row.icon).Size(14, 14).Shrink(0).TextColor(muted)
			weight := 400
			if current {
				weight = 600
			}
			ui.Text(c, title).FontWeight(weight).TextColor(text).SingleLine().Ellipsis("…").Grow(1).Shrink(1).MinWidth(0)
			if detail != "" {
				ui.Text(c, detail).FontSize(smallSize).TextColor(muted).SingleLine().Ellipsis("…").Shrink(2).MinWidth(0).MaxWidth(150)
			}
			if current {
				check := t.Accent
				if lit {
					check = t.AccentText
				}
				ui.Icon(c, checkIcon).Size(12, 12).Shrink(0).TextColor(check)
			}
		})
	})
	return b.Hovered() && !lit
}
