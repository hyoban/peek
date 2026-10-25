package app

import (
	"context"
	_ "embed"
	"fmt"
	"path"
	"strconv"
	"strings"
	"time"

	"peek/internal/git"

	"github.com/egoist/mygo"
)

// Peek's item in the menu bar, where the user chooses to show it, is
// there whether windows show or not. A click opens its menu, of the recent
// repositories, what each has to commit and to pull, and a right click
// brings Peek to the front; the count beside the icon is of the commits
// to pull of them all.
type menuBar struct {
	tray *mygo.Tray
	// status is that of the repositories without a window, read every
	// while; those with one tell their own.
	status  map[string]repoStatus
	fetched map[string]time.Time // when a repository without a window was last fetched
	pulling map[string]bool
	title   string
	stop    func() // takes the item out of the menu bar
}

type repoStatus struct {
	branch          string
	changed, behind int
	loaded          bool
}

// trayIcon is the SF Symbol arrow.triangle.branch, as trayicon.swift
// renders it.
//
//go:embed trayicon.png
var trayIcon []byte

// useDock shows Peek's icon in the Dock and the app switcher, or takes it
// out, as an app of the menu bar's has none.
func useDock(dock bool) {
	if dock {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyRegular)
	} else {
		mygo.App.SetActivationPolicy(mygo.ActivationPolicyAccessory)
	}
}

// fetchClosedEvery is how often the remotes of recent repositories
// without a window are fetched, less often than those that show.
const fetchClosedEvery = 15 * time.Minute

// showMenuBar puts Peek's item in the menu bar in place of its Dock
// icon, reading the status of the recent repositories again every while,
// or takes it out for the Dock icon again.
func (p *projects) showMenuBar(on bool) {
	if p.inline {
		return
	}
	if !on {
		if p.bar != nil {
			p.bar.stop()
			p.bar = nil
		}
		useDock(true)
		return
	}
	if p.bar != nil {
		return
	}
	tray, err := mygo.NewTray(mygo.TrayOptions{ID: "peek", Icon: trayIcon, IconIsTemplate: true, ToolTip: "Peek"})
	if err != nil {
		useDock(true) // not without either
		return
	}
	useDock(false)
	ctx, cancel := context.WithCancel(context.Background())
	bar := &menuBar{tray: tray, status: map[string]repoStatus{}, fetched: map[string]time.Time{}, pulling: map[string]bool{}}
	p.bar = bar
	// The menu is made for each click, of the status as it is then,
	// AppKit placing it under the item.
	popUp := func() {
		// The mouse going down on the item deactivates Peek, as the menu
		// bar is the app's before it while Peek has no Dock icon: Peek
		// comes back in front once the menu closes where it was, shown
		// again where it hid as inactive.
		active := wasActive()
		tray.SetMenu(mygo.NewMenu(p.menuBarItems()))
		tray.PopUpMenu() // until the menu closes
		tray.SetMenu(nil)
		if active {
			// Once AppKit has told of the deactivation, which may come
			// after the menu closes.
			time.AfterFunc(100*time.Millisecond, func() {
				mygo.App.WhenReady(func() {
					if !appActive {
						mygo.App.Show()
						mygo.App.Focus()
					}
				})
			})
		}
		p.readStatus() // for the next
	}
	tray.OnClick(popUp)
	tray.OnRightClick(p.activate)
	offQuit := mygo.App.OnWillQuit(func(*mygo.QuitEvent) { tray.Destroy() })
	bar.stop = func() {
		cancel()
		offQuit()
		tray.Destroy()
	}
	p.readStatus()
	go func() {
		tick := time.NewTicker(fetchEvery)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				mygo.App.WhenReady(func() {
					if p.bar == bar {
						p.readStatus()
					}
				})
			}
		}
	}()
}

// menuBarItems are the menu's: the recent repositories, the one shown
// checked, as the project switcher checks it; the commits to
// pull; the File menu's Open Folder; the app menu's choices; and Quit.
func (p *projects) menuBarItems() []*mygo.MenuItem {
	var items, pulls []*mygo.MenuItem
	for _, repo := range p.recent {
		s := p.statusOf(repo)
		items = append(items, &mygo.MenuItem{
			Label:   p.label(repo) + repoDetail(p.apps[repo], s),
			ToolTip: repo,
			Type:    mygo.MenuItemCheckbox,
			Checked: p.cur != nil && p.cur.repo == repo,
			Click: func(it *mygo.MenuItem, _ *mygo.Window) {
				it.SetChecked(true) // checked as it shows, not toggled
				p.openRepo(repo)
			},
		})
		if pulling := p.bar.pulling[repo] || p.apps[repo] != nil && p.apps[repo].pull.running; pulling || s.behind > 0 {
			label := fmt.Sprintf("Pull %s into %s", count(s.behind, "New Commit"), p.label(repo))
			if pulling {
				label = "Pulling " + p.label(repo) + "…"
			}
			pulls = append(pulls, &mygo.MenuItem{Label: label, Disabled: pulling, ToolTip: "Fast-forward only",
				Click: func(*mygo.MenuItem, *mygo.Window) { p.pullRepo(repo) }})
		}
	}
	if len(items) == 0 {
		items = append(items, &mygo.MenuItem{Label: "No Recent Repositories", Disabled: true})
	}
	if len(pulls) > 0 {
		items = append(append(items, mygo.Separator()), pulls...)
	}
	return append(items,
		mygo.Separator(),
		&mygo.MenuItem{Label: "Open Folder…", Accelerator: "Cmd+O", Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() }},
		mygo.Separator(),
		// Here too, to go back to the Dock where the app menu does not show.
		&mygo.MenuItem{Label: "Show in Menu Bar", Type: mygo.MenuItemCheckbox, Checked: true,
			ToolTip: "Show Peek in the Dock instead",
			Click:   func(*mygo.MenuItem, *mygo.Window) { p.setMenuBarOn(false) }},
		&mygo.MenuItem{Label: "Hide When Inactive", Type: mygo.MenuItemCheckbox, Checked: p.hideInactive,
			ToolTip: "Hide Peek as another app comes to the front",
			Click:   func(*mygo.MenuItem, *mygo.Window) { p.setHideInactive(!p.hideInactive) }},
		mygo.Separator(),
		&mygo.MenuItem{Label: "Quit Peek", Accelerator: "Cmd+Q", Click: func(*mygo.MenuItem, *mygo.Window) { mygo.App.Quit() }},
	)
}

// repoDetail follows a repository's name in the menu: its branch, the
// files it changes and what its window is busy with.
func repoDetail(a *app, s repoStatus) string {
	if !s.loaded {
		return ""
	}
	parts := []string{s.branch}
	if s.changed > 0 {
		parts = append(parts, count(s.changed, "file")+" changed")
	}
	switch {
	case a == nil:
	case a.push.running:
		parts = append(parts, "Shipping…")
	case a.review.running:
		parts = append(parts, "Reviewing…")
	case a.switcher.running != "":
		parts = append(parts, "Checking out…")
	}
	return " — " + strings.Join(parts, ", ")
}

// statusOf is the status of a recent repository: its window's where it
// has one, else the one read last.
func (p *projects) statusOf(repo string) repoStatus {
	if a := p.apps[repo]; a != nil && a.commits != nil {
		return repoStatus{branch: a.branch, changed: a.dirty, behind: a.behind, loaded: true}
	}
	if p.bar == nil {
		return repoStatus{}
	}
	return p.bar.status[repo]
}

// readStatus reads the status of the recent repositories without a
// window off the main thread, fetching their remotes every while.
func (p *projects) readStatus() {
	bar := p.bar
	if bar == nil {
		return
	}
	for _, repo := range p.recent {
		if p.apps[repo] != nil {
			continue
		}
		fetch := networkAllows() && time.Since(bar.fetched[repo]) >= fetchClosedEvery
		if fetch {
			bar.fetched[repo] = time.Now()
		}
		go func() {
			if fetch {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				git.FetchUpstream(ctx, repo)
				cancel()
			}
			s := readRepoStatus(repo)
			mygo.App.WhenReady(func() {
				bar.status[repo] = s
				p.syncTitle()
			})
		}()
	}
}

func readRepoStatus(repo string) repoStatus {
	changed, _ := git.CountChanges(repo)
	behind, _ := git.BehindUpstream(repo)
	return repoStatus{branch: git.CurrentBranch(repo), changed: changed, behind: behind, loaded: true}
}

// syncTitle counts beside the icon the commits the recent repositories'
// remotes have that their branches have not, where any have.
func (p *projects) syncTitle() {
	if p.bar == nil || p.bar.tray == nil {
		return
	}
	n := 0
	for _, repo := range p.recent {
		n += p.statusOf(repo).behind
	}
	title := ""
	if n > 0 {
		title = strconv.Itoa(n)
	}
	if title != p.bar.title {
		p.bar.title = title
		p.bar.tray.SetTitle(title)
		tip := "Peek"
		if n > 0 {
			tip += " — " + count(n, "commit") + " to pull"
		}
		p.bar.tray.SetToolTip(tip)
	}
}

// activate brings Peek to the front, as a right click on its item asks:
// the window, opened again where it was closed, with the projects open at
// quitting, or the latest.
func (p *projects) activate() {
	if p.win == nil {
		p.restore()
	}
	mygo.App.Show() // where it hid as inactive
	mygo.App.Focus()
	if p.win != nil {
		p.win.Focus()
	}
}

// openRepo shows the repository in the window, in front.
func (p *projects) openRepo(repo string) {
	p.open(repo)
	mygo.App.Show()
	mygo.App.Focus()
	if p.win != nil {
		p.win.Focus()
	}
}

// pullRepo brings in the upstream's new commits by fast-forward: in the
// repository's window where it has one.
func (p *projects) pullRepo(repo string) {
	if a := p.apps[repo]; a != nil {
		a.startPull()
		return
	}
	bar := p.bar
	if bar.pulling[repo] {
		return
	}
	bar.pulling[repo] = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		err := git.PullUpstream(ctx, repo)
		cancel()
		s := readRepoStatus(repo)
		mygo.App.WhenReady(func() {
			delete(bar.pulling, repo)
			bar.status[repo] = s
			p.syncTitle()
			if err != nil {
				mygo.Dialog.Message(mygo.MessageOptions{Type: mygo.MessageWarning, Message: "Couldn’t Pull " + path.Base(repo), Detail: err.Error()})
			}
		})
	}()
}
