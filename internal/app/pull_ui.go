package app

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"

	"peek/internal/git"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// shownPull is the branch's pull request where the branch's changes show,
// nil where it has none or they do not show.
func (a *app) shownPull() *git.BranchPull {
	cm, ok := a.current()
	if !ok || !git.IsRange(cm.Hash) || a.pullFor != a.branchURL {
		return nil
	}
	return a.pullReq
}

// htmlComment is a comment of a description's HTML, as templates leave,
// which GitHub does not show.
var htmlComment = regexp.MustCompile(`(?s)<!--.*?-->`)

// pullDescription is a pull request's description as GitHub shows it.
func pullDescription(body string) string {
	return strings.TrimSpace(htmlComment.ReplaceAllString(strings.ReplaceAll(body, "\r\n", "\n"), ""))
}

// pullHeader is the first row of the branch's changes where the branch
// has a pull request, as commitMessage is a commit's: its title and where
// it stands, whether it merges, its checks, and its description.
func (a *app) pullHeader(c *ui.Context, pr *git.BranchPull) {
	t := c.Theme()
	services := c.Services()
	ui.Column(c).FillWidth().Gap(t.Space(3)).Padding(t.Space(3), t.Space(4), t.Space(4), t.Space(4)).Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			ui.Icon(c, pullIcon).Size(16, 16).Shrink(0).TextColor(pullTint(c, pr))
			ui.RichText(c).FontWeight(600).FontSize(fontSize + 1).Grow(1).MinWidth(0).Children(func() {
				ui.Text(c, pr.Title+" ")
				ui.Link(c, "#"+strconv.Itoa(pr.Number), pr.URL)
			})
			pullCapsule(c, pullStateLabel(pr), pullTint(c, pr))
		})
		ui.Row(c).FillWidth().Gap(t.Space(1.5)).AlignItems(ui.Center).Children(func() {
			by := "By " + pr.Author
			if pr.Base != "" {
				by += " into " + pr.Base
			}
			ui.Text(c, by).FontSize(smallSize).TextColor(t.TextMuted).SingleLine()
			if r := reviewLabel(pr.Review); r != "" && pr.State == "OPEN" {
				ui.Text(c, "·").FontSize(smallSize).TextColor(t.TextMuted)
				ui.Text(c, r).FontSize(smallSize).TextColor(reviewTint(c, pr.Review)).SingleLine()
			}
		})
		// Whether it merges first, as what the reader wants most, over the
		// checks it may tell of, a long list of them below.
		if pr.State == "OPEN" {
			mergeBox(c, mergeStatusOf(pr))
		}
		if len(pr.Checks) > 0 {
			a.pullChecks(c, pr)
		}
		if body := pullDescription(pr.Body); body != "" {
			ui.Column(c).FillWidth().Children(func() {
				if a.pullMarkdown == nil {
					a.pullMarkdown = &markdown.Options{Theme: &markdown.Theme{CodeFont: monoFamily}, WrapCode: true,
						OnLink: func(u string) { services.OpenURL(u) }}
				}
				markdown.View(c, body, a.pullMarkdown)
			})
		} else {
			ui.Text(c, "No description provided.").TextColor(t.TextMuted).Italic()
		}
	})
}

// pullChecks are the checks of the pull request: how they stand, over
// the list of them, which a click on the summary shows or hides, shown at
// first where one failed. A check's row opens its details.
func (a *app) pullChecks(c *ui.Context, pr *git.BranchPull) {
	t := c.Theme()
	services := c.Services()
	state, _ := pr.ChecksState()
	open, ok := a.checksOpen[pr.URL]
	if !ok {
		open = state == git.CheckFailed
	}
	ui.Column(c).FillWidth().Radius(8).Border(1, t.Text.Alpha(0.08)).Background(t.Text.Alpha(0.03)).Clip().Children(func() {
		summary := ui.ButtonBase(c.Key("checks")).FillWidth().Padding(t.Space(1.5), t.Space(2.5)).
			Label(showOrHide(open, "Checks")).KeepFocus()
		flat(summary, t, false)
		summary.OnClick(func() {
			if a.checksOpen == nil {
				a.checksOpen = map[string]bool{}
			}
			a.checksOpen[pr.URL] = !open
		})
		summary.Children(func() {
			ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
				checkMark(c, state, 14)
				ui.Text(c, checksSummary(pr)).FontWeight(600).Grow(1).MinWidth(0).SingleLine().Ellipsis("…")
				chevron := ui.Icon(c, chevronIcon).Size(10, 10).Shrink(0).TextColor(t.TextMuted)
				if !open {
					chevron.Rotate(-90)
				}
			})
		})
		if !open {
			return
		}
		for i, ch := range pr.Checks {
			ui.Divider(c)
			row := ui.ButtonBase(c.Key("check:"+strconv.Itoa(i))).FillWidth().Padding(t.Space(1.25), t.Space(2.5)).
				Label(ch.Name).Tooltip(checkTooltip(ch)).Disabled(ch.URL == "").KeepFocus()
			flat(row, t, false)
			url := ch.URL
			row.OnClick(func() { services.OpenURL(url) })
			row.Children(func() {
				ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
					checkMark(c, ch.State, 12)
					ui.Text(c, ch.Name).FontSize(smallSize).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
					if ch.Required {
						ui.Text(c, "Required").FontSize(smallSize-1).TextColor(t.TextMuted).SingleLine().Shrink(0).
							Padding(0, t.Space(1)).Radius(t.Space(1)).Background(t.TextMuted.Alpha(0.12))
					}
					if ch.Workflow != "" {
						ui.Text(c, ch.Workflow).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").
							Grow(1).MinWidth(0)
					} else {
						ui.Spacer(c)
					}
					ui.Text(c, checkStateLabel(ch.State)).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Shrink(0)
				})
			})
		}
	})
}

// checkMark is the mark of where a check stands, as GitHub's: a tick, a
// cross, a spinner while it runs, or a dash where it was skipped.
func checkMark(c *ui.Context, s git.CheckState, size float32) {
	switch s {
	case git.CheckPending:
		ui.Spinner(c).Size(size, size).Shrink(0).Label("Running")
	default:
		glyph, label := checkStateGlyph(s)
		ui.Icon(c, glyph).Size(size, size).Shrink(0).TextColor(checkTint(c, s)).Label(label)
	}
}

func checkStateGlyph(s git.CheckState) (*ui.SVG, string) {
	switch s {
	case git.CheckFailed:
		return crossCircleIcon, "Failed"
	case git.CheckSkipped:
		return skipCircleIcon, "Skipped"
	}
	return checkCircleIcon, "Passed"
}

func checkTint(c *ui.Context, s git.CheckState) ui.Color {
	switch s {
	case git.CheckFailed:
		return system(c, systemRed)
	case git.CheckPending:
		return system(c, systemOrange)
	case git.CheckPassed:
		return system(c, systemGreen)
	}
	return c.Theme().TextMuted
}

func checkStateLabel(s git.CheckState) string {
	return [...]string{"Failed", "Running", "Passed", "Skipped"}[s]
}

func checkTooltip(ch git.Check) string {
	name := ch.Name
	if ch.Workflow != "" {
		name = ch.Workflow + " / " + name
	}
	if ch.URL == "" {
		return name
	}
	return name + "\nOpen the check’s details"
}

// checksSummary tells how the checks stand, as GitHub's merge box: those
// failing, running and passing, or all of them where they agree.
func checksSummary(pr *git.BranchPull) string {
	n := pr.Counts()
	all := len(pr.Checks)
	for s, done := range map[git.CheckState]string{git.CheckFailed: "failed", git.CheckPending: "running", git.CheckPassed: "passed", git.CheckSkipped: "skipped"} {
		if n[s] == all {
			if all == 1 {
				return "1 check " + done
			}
			return "All " + strconv.Itoa(all) + " checks " + done
		}
	}
	var parts []string
	for _, p := range []struct {
		s    git.CheckState
		word string
	}{{git.CheckFailed, "failed"}, {git.CheckPending, "running"}, {git.CheckPassed, "passed"}, {git.CheckSkipped, "skipped"}} {
		if n[p.s] > 0 {
			parts = append(parts, strconv.Itoa(n[p.s])+" "+p.word)
		}
	}
	return count(all, "check") + ": " + strings.Join(parts, ", ")
}

// checksBadge is the mark of the checks of the branch's pull request on
// the row of its changes in the history: a tick, a cross or a dot.
func checksBadge(c *ui.Context, pr *git.BranchPull) (ui.SourceListBadge, bool) {
	s, ok := pr.ChecksState()
	if !ok || pr.State != "OPEN" {
		return ui.SourceListBadge{}, false
	}
	text := map[git.CheckState]string{git.CheckFailed: "✕", git.CheckPending: "●", git.CheckPassed: "✓"}[s]
	return ui.SourceListBadge{Text: text, Color: checkTint(c, s), Bold: true}, true
}

func pullStateLabel(pr *git.BranchPull) string {
	switch {
	case pr.State == "MERGED":
		return "Merged"
	case pr.Draft:
		return "Draft"
	}
	return "Open"
}

// pullTint is the color of the pull request's state, as GitHub's: green
// open, gray a draft, purple merged.
func pullTint(c *ui.Context, pr *git.BranchPull) ui.Color {
	switch {
	case pr.State == "MERGED":
		return system(c, systemPurple)
	case pr.Draft:
		return c.Theme().TextMuted
	}
	return system(c, systemGreen)
}

func reviewLabel(decision string) string {
	switch decision {
	case "APPROVED":
		return "Approved"
	case "CHANGES_REQUESTED":
		return "Changes requested"
	case "REVIEW_REQUIRED":
		return "Review required"
	}
	return ""
}

func reviewTint(c *ui.Context, decision string) ui.Color {
	switch decision {
	case "APPROVED":
		return system(c, systemGreen)
	case "CHANGES_REQUESTED":
		return system(c, systemRed)
	}
	return c.Theme().TextMuted
}

// pullCapsule is a label in a capsule of its color, as GitHub's state.
func pullCapsule(c *ui.Context, label string, tint ui.Color) {
	t := c.Theme()
	ui.Text(c, label).FontSize(smallSize).FontWeight(600).TextColor(tint).SingleLine().Shrink(0).
		Padding(t.Space(0.25), t.Space(2)).Radius(999).Background(tint.Alpha(0.14))
}

// mergeLevel is how a pull request's merging stands, as the color of
// GitHub's merge box.
type mergeLevel int

const (
	mergeReady    mergeLevel = iota // green
	mergeWarning                    // orange: it merges, though not all is well
	mergeBlocked                    // red
	mergeChecking                   // muted, while GitHub works it out
)

// mergeStatus is where merging a pull request stands: a headline, and
// what holds it back.
type mergeStatus struct {
	level    mergeLevel
	headline string
	reasons  []string
}

// mergeStatusOf tells whether the pull request can be merged, and where
// not, why: a draft, conflicts, the reviews, the checks and the
// conversations the base's rules want, or the base having moved on.
func mergeStatusOf(pr *git.BranchPull) mergeStatus {
	base := cmp.Or(pr.Base, "the base branch")
	if pr.Draft {
		return mergeStatus{mergeBlocked, "This pull request is still a draft",
			[]string{"Mark it ready for review on GitHub to merge it"}}
	}
	var reasons []string
	conflicts := pr.Mergeable == "CONFLICTING" || pr.MergeState == "DIRTY"
	if conflicts {
		reasons = append(reasons, "This branch has conflicts with "+base+" that must be resolved")
	}
	if pr.MergeState == "BEHIND" {
		reasons = append(reasons, "This branch is out of date with "+base+": update it to merge")
	}
	switch pr.Review {
	case "CHANGES_REQUESTED":
		reasons = append(reasons, "A reviewer requested changes")
	case "REVIEW_REQUIRED":
		reasons = append(reasons, "An approving review is required")
	}
	if pr.MergeState == "BLOCKED" {
		// The checks the rules require, where GitHub told which: else
		// any, which may be.
		n, which := pr.RequiredCounts(), "required "
		if pr.Unresolved < 0 {
			n, which = pr.Counts(), ""
		}
		if n[git.CheckFailed] > 0 {
			reasons = append(reasons, plural(n[git.CheckFailed], which+"check")+" failed")
		}
		if n[git.CheckPending] > 0 {
			reasons = append(reasons, plural(n[git.CheckPending], which+"check")+" "+isAre(n[git.CheckPending], "still running"))
		}
		if pr.Unresolved > 0 {
			reasons = append(reasons, plural(pr.Unresolved, "conversation")+" "+isAre(pr.Unresolved, "not resolved"))
		}
	}
	switch {
	case conflicts:
		return mergeStatus{mergeBlocked, "Can’t merge: conflicts", reasons}
	case pr.MergeState == "BLOCKED" || pr.MergeState == "BEHIND":
		if len(reasons) == 0 {
			reasons = []string{"The rules of " + base + " aren’t met yet"}
		}
		return mergeStatus{mergeBlocked, "Merging is blocked", reasons}
	case pr.MergeState == "UNSTABLE":
		return mergeStatus{mergeWarning, "Can merge, though checks that aren’t required failed", reasons}
	case pr.MergeState == "CLEAN" || pr.MergeState == "HAS_HOOKS":
		return mergeStatus{mergeReady, "Ready to merge into " + base, reasons}
	}
	return mergeStatus{mergeChecking, "Checking whether it can merge…", reasons}
}

// plural is a count of things, as "1 check" or "2 checks".
func plural(n int, thing string) string {
	if n == 1 {
		return "1 " + thing
	}
	return strconv.Itoa(n) + " " + thing + "s"
}

func isAre(n int, rest string) string {
	if n == 1 {
		return "is " + rest
	}
	return "are " + rest
}

// mergeBox is the merge status as GitHub's merge box shows it: its mark
// and headline, over what holds it back.
func mergeBox(c *ui.Context, m mergeStatus) {
	t := c.Theme()
	tint := map[mergeLevel]ui.Color{mergeReady: system(c, systemGreen), mergeWarning: system(c, systemOrange),
		mergeBlocked: system(c, systemRed), mergeChecking: t.TextMuted}[m.level]
	ui.Column(c).FillWidth().Gap(t.Space(1)).Padding(t.Space(1.5), t.Space(2.5)).Radius(8).
		Border(1, tint.Alpha(0.35)).Background(tint.Alpha(0.06)).Role(ui.RoleGroup).Label("Merge Status").Children(func() {
		ui.Row(c).FillWidth().Gap(t.Space(2)).AlignItems(ui.Center).Children(func() {
			switch m.level {
			case mergeChecking:
				ui.Spinner(c).Size(14, 14).Shrink(0).Label("Checking")
			case mergeReady:
				ui.Icon(c, checkCircleIcon).Size(14, 14).Shrink(0).TextColor(tint)
			case mergeWarning:
				ui.Icon(c, warningIcon).Size(14, 14).Shrink(0).TextColor(tint)
			default:
				ui.Icon(c, crossCircleIcon).Size(14, 14).Shrink(0).TextColor(tint)
			}
			ui.Text(c, m.headline).FontWeight(600).Grow(1).MinWidth(0).SingleLine().Ellipsis("…").Tooltip(m.headline)
		})
		for i, r := range m.reasons {
			ui.Row(c).Key(i).FillWidth().Gap(t.Space(2)).AlignItems(ui.Start).Padding(0, 0, 0, 14+t.Space(2)).Children(func() {
				ui.Text(c, "•").FontSize(smallSize).TextColor(t.TextMuted).Shrink(0)
				ui.Text(c, r).FontSize(smallSize).TextColor(t.Text).Grow(1).MinWidth(0).Selectable()
			})
		}
	})
}
