package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"peek/internal/git"

	"github.com/egoist/mygo/ui"
)

// worktreeBusy reports whether a worktree can't be removed now: one is
// being removed, or something runs that checks out or commits.
func (a *app) worktreeBusy() bool {
	return a.removingWorktrees || a.push.running || a.review.running || a.pull.running || a.switcher.running != ""
}

// confirmRemoveWorktree asks before removing the worktree of the branch,
// as the branch switcher's Remove Worktree… does.
func (a *app) confirmRemoveWorktree(dir, branch string) {
	if a.worktreeBusy() {
		return
	}
	message := fmt.Sprintf("The folder at %s is deleted. The branch “%s” is kept.", homePath(dir), branch)
	a.confirm(fmt.Sprintf("Remove the worktree of “%s”?", branch), message, "Remove Worktree", func() {
		a.removeWorktrees([]git.DoneWorktree{{Dir: dir, Branch: branch}}, false, true)
	})
}

// cleanUpWorktrees lists the worktrees whose branches are merged or
// deleted from their remote, forgetting those whose folders are gone,
// then asks before removing them, as Repository ▸ Clean Up Worktrees…
// does.
func (a *app) cleanUpWorktrees() {
	if a.worktreeBusy() {
		return
	}
	a.removingWorktrees = true
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		pruneErr := git.PruneWorktrees(ctx, repo)
		done, kept, err := git.CleanableWorktrees(ctx, repo)
		return func() {
			a.removingWorktrees = false
			switch {
			case err != nil || pruneErr != nil:
				a.showAlert("Couldn’t Clean Up Worktrees", errors.Join(pruneErr, err).Error())
			case len(done) == 0:
				a.showAlert("No Worktrees to Clean Up",
					"No other worktree has a branch that’s merged or deleted from its remote."+keptNote(kept))
			default:
				a.confirmCleanUp(done, kept)
			}
		}
	})
}

// keptNote names the worktrees kept, each with why, under the message.
func keptNote(kept []git.KeptWorktree) string {
	if len(kept) == 0 {
		return ""
	}
	lines := make([]string, len(kept))
	for i, w := range kept {
		lines[i] = worktreeLine(w.Branch, w.Dir, w.Reason)
	}
	return "\n\nKept:\n" + strings.Join(lines, "\n")
}

// worktreeLine names a worktree by its branch, else its folder, and where
// it is, and why it was kept where there is a reason.
func worktreeLine(branch, dir, reason string) string {
	line := homePath(dir)
	if branch != "" {
		line = branch + " — " + line
	}
	if reason != "" {
		line += ": " + reason
	}
	return line
}

// confirmCleanUp asks before removing the worktrees listed, naming each,
// and those kept, each with why.
func (a *app) confirmCleanUp(done []git.DoneWorktree, kept []git.KeptWorktree) {
	lines := make([]string, len(done))
	for i, w := range done {
		why := "merged"
		if w.Gone {
			why = "deleted from its remote"
		}
		lines[i] = fmt.Sprintf("%s (%s)", worktreeLine(w.Branch, w.Dir, ""), why)
	}
	title := fmt.Sprintf("Remove %d worktrees?", len(done))
	if len(done) == 1 {
		title = fmt.Sprintf("Remove the worktree of “%s”?", done[0].Branch)
	}
	message := strings.Join(lines, "\n") + "\n\nThe folders are deleted; the branches are kept. Worktrees with uncommitted changes aren’t removed." + keptNote(kept)
	a.confirm(title, message, "Remove Worktrees", func() { a.removeWorktrees(done, false, false) })
}

// removeWorktrees removes the worktrees off the UI thread, closing the
// projects open on them. Those with changes are removed only with force:
// one asked for alone is asked of again, more are kept and named.
func (a *app) removeWorktrees(list []git.DoneWorktree, force, alone bool) {
	if a.worktreeBusy() {
		return
	}
	a.removingWorktrees = true
	repo := a.repo
	// The projects open on them, found while their folders are there to
	// resolve, as /var is /private/var.
	open := map[string][]string{}
	if a.projects != nil {
		for _, w := range list {
			for dir, other := range a.projects.apps {
				if other != a && git.SamePath(dir, w.Dir) {
					open[w.Dir] = append(open[w.Dir], dir)
				}
			}
		}
	}
	stop := a.startWorktreeProgress(len(list))
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		var removed []string
		var changed []git.DoneWorktree
		var failed []git.KeptWorktree // each with git's reason
		for i, w := range list {
			if stop.Load() {
				break // the rest left as they are
			}
			a.onUI(func() { a.worktreeProgress.step(i, w.Branch) })
			err := git.RemoveWorktree(ctx, repo, w.Dir, force)
			if err == nil {
				removed = append(removed, w.Dir)
				continue
			}
			if errors.Is(err, git.ErrWorktreeChanged) {
				changed = append(changed, w)
			}
			failed = append(failed, git.KeptWorktree{Dir: w.Dir, Branch: w.Branch, Reason: err.Error()})
		}
		return func() {
			a.removingWorktrees = false
			a.worktreeProgress = worktreeProgress{}
			for _, dir := range removed {
				for _, project := range open[dir] {
					a.forgetProject(project)
				}
			}
			a.switcher.branches.Worktrees = remaining(a.switcher.branches.Worktrees, removed)
			switch {
			case len(failed) == 0:
			case alone && len(changed) > 0:
				w := changed[0]
				a.confirm(fmt.Sprintf("The worktree of “%s” has changes.", w.Branch),
					capitalize(failed[0].Reason)+". Removing the worktree deletes its changes, and you can’t undo this.",
					"Remove Anyway", func() { a.removeWorktrees(changed, true, true) })
			case alone:
				a.showAlert(fmt.Sprintf("Couldn’t remove the worktree of “%s”.", failed[0].Branch), capitalize(failed[0].Reason)+".")
			default:
				lines := make([]string, len(failed))
				for i, w := range failed {
					lines[i] = worktreeLine(w.Branch, w.Dir, w.Reason)
				}
				message := strings.Join(lines, "\n")
				if len(changed) > 0 {
					message += "\n\nRemove one with changes from the branch switcher to discard them."
				}
				title := fmt.Sprintf("%d worktrees weren’t removed.", len(failed))
				if len(failed) == 1 {
					title = fmt.Sprintf("The worktree of “%s” wasn’t removed.", failed[0].Branch)
				}
				a.showAlert(title, message)
			}
		}
	})
}

// worktreeProgress is the sheet that shows how far removing worktrees
// has got, as Finder's copy window: which one is being removed, of how
// many, and Stop, which leaves the rest. It shows once removing has taken
// a moment, rather than flash for a quick one.
type worktreeProgress struct {
	running     bool
	shown       bool // the sheet is up: it was dismissed where this goes false
	done, total int
	current     string
	since       time.Time
	stop        *atomic.Bool
}

// worktreeProgressDelay is how long removing goes on before the sheet shows.
const worktreeProgressDelay = 400 * time.Millisecond

// startWorktreeProgress starts the progress of removing total worktrees,
// and returns what Stop sets.
func (a *app) startWorktreeProgress(total int) *atomic.Bool {
	stop := &atomic.Bool{}
	a.worktreeProgress = worktreeProgress{running: true, total: total, since: time.Now(), stop: stop}
	if !a.inline {
		time.AfterFunc(worktreeProgressDelay, func() { a.win.Update(func() {}) }) // to show the sheet
	}
	return stop
}

func (p *worktreeProgress) step(done int, current string) {
	if p.running {
		p.done, p.current = done, current
	}
}

// onUI runs fn on the UI thread, as work in the background reports how
// far it has got; at once without a window, as background runs work.
func (a *app) onUI(fn func()) {
	if a.inline {
		fn()
		return
	}
	a.win.Update(fn)
}

// worktreeProgressSheet shows the progress of removing worktrees over the
// window: a bar that fills as each is removed, or that moves where there
// is one, and Stop. Escape stops too.
func (a *app) worktreeProgressSheet(c *ui.Context) {
	p := &a.worktreeProgress
	if !p.running || time.Since(p.since) < worktreeProgressDelay && !a.inline {
		return
	}
	if !p.shown {
		p.shown = true
	}
	stopping := p.stop.Load()
	ui.Modal(c.Key("worktree-progress"), &p.shown, func() {
		t := c.Theme()
		ui.Column(c).Width(360).Gap(t.Space(2)).Children(func() {
			title := "Removing Worktrees"
			if p.total == 1 {
				title = "Removing Worktree"
			}
			ui.Text(c, title).FontWeight(600)
			detail := fmt.Sprintf("Removing “%s” — %d of %d", p.current, p.done+1, p.total)
			value := float64(p.done) / float64(p.total)
			if p.total == 1 {
				detail, value = fmt.Sprintf("Removing “%s”", p.current), -1
			}
			if stopping {
				detail = "Stopping after “" + p.current + "”…"
			}
			ui.Text(c, detail).FontSize(smallSize).TextColor(t.TextMuted).SingleLine().Ellipsis("…").FillWidth()
			ui.Progress(c, value).FillWidth().Label("Progress")
			if p.total > 1 { // one has nothing to stop before
				ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
					if ui.Button(c.Key("worktree-stop"), "Stop").Disabled(stopping).Clicked() {
						p.stop.Store(true)
					}
				})
			}
		})
	})
	if !p.shown { // dismissed, as Escape does
		p.shown = true
		if p.total > 1 {
			p.stop.Store(true)
		}
	}
}

// remaining is the worktrees by branch less those removed.
func remaining(worktrees map[string]string, removed []string) map[string]string {
	kept := map[string]string{}
	for branch, dir := range worktrees {
		gone := false
		for _, r := range removed {
			gone = gone || git.SamePath(dir, r)
		}
		if !gone {
			kept[branch] = dir
		}
	}
	return kept
}

// forgetProject closes the project open on the folder, now deleted, and
// takes it out of its group.
func (a *app) forgetProject(repo string) {
	p := a.projects
	if p == nil || p.apps[repo] == nil || p.apps[repo] == a {
		return
	}
	p.remove(repo)
	p.apps[repo].close()
	delete(p.apps, repo)
	p.saveOpen()
}

// capitalize starts the reason with a capital, as a sentence of its own.
func capitalize(s string) string {
	if s == "" {
		return s
	}
	r, n := utf8.DecodeRuneInString(s)
	return string(unicode.ToUpper(r)) + s[n:]
}

// homePath shortens a path in the home folder to start at ~, as Finder
// shows it.
func homePath(p string) string {
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if rest, ok := strings.CutPrefix(p, home+"/"); ok {
			return "~/" + rest
		}
	}
	return p
}
