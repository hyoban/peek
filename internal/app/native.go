package app

import (
	"path"
	"time"

	"github.com/egoist/mygo"
)

// appActive is whether Peek is the frontmost app, as AppKit tells, on the
// main thread, and resignedAt when it last stopped being it.
var (
	appActive  = true
	resignedAt time.Time
)

// wasActive reports whether Peek is frontmost, or was until a moment ago,
// as a click on its menu bar item leaves it.
func wasActive() bool { return appActive || time.Since(resignedAt) < 300*time.Millisecond }

// followApp keeps appActive, clears the notifications of what the user
// comes back to see, as Mail and Messages do, and hides Peek as another
// app comes to the front where the user chose so, as ⌘H hides it.
func (p *projects) followApp() {
	mygo.App.OnDidBecomeActive(func() {
		appActive = true
		mygo.ClearNotifications()
	})
	mygo.App.OnDidResignActive(func() {
		appActive, resignedAt = false, time.Now()
		if p.hideInactive && !p.quitting {
			mygo.App.Hide()
		}
	})
}

// notify tells of work finished, as a push or a review, in a notification
// where the user is not looking at its project; a click shows it in the
// window, in front, and runs then on its thread.
func (a *app) notify(title, body string, then func()) {
	shown := a.projects == nil || a.projects.cur == a
	if a.inline || a.win == nil || appActive && a.win.IsFocused() && shown {
		return
	}
	win := a.win
	n := mygo.NewNotification(mygo.NotificationOptions{
		ID:       title + "\x00" + a.repo, // one of each kind a project
		Title:    title,
		Subtitle: path.Base(a.repo),
		Body:     body,
	})
	n.OnClick(func() {
		win.Update(func() {
			if a.projects != nil && a.projects.apps[a.repo] == a {
				a.projects.openRepo(a.repo)
			}
			win.Focus()
			if then != nil {
				then()
			}
		})
		n.Close()
	})
	go n.Show() // which waits for the user's answer the first time
}

// networkAllows reports whether the remote may be fetched in the
// background: online, and not on a connection that counts its data, as a
// phone's hotspot or Low Data Mode.
func networkAllows() bool {
	s := mygo.Network.Status()
	return s.Online && !s.Metered
}

// followNetwork fetches the projects' remotes as the network comes back,
// rather than at the next fetch.
func (p *projects) followNetwork() {
	was := networkAllows()
	mygo.Network.OnChanged(func(mygo.NetworkStatus) {
		now := networkAllows()
		if now && !was {
			mygo.App.WhenReady(func() {
				for _, a := range p.apps {
					a.fetchRemote()
				}
			})
		}
		was = now
	})
}

// syncBadge counts on the Dock icon the commits the remotes of the
// projects open have that their branches have not, as Mail counts unread
// messages.
func (p *projects) syncBadge() {
	n := 0
	for _, a := range p.apps {
		n += a.behind
	}
	if n != p.badge && !p.inline {
		p.badge = n
		mygo.App.SetBadgeCount(n)
	}
}

// setDockMenu adds Open Folder to the Dock icon's menu, over the recent
// projects macOS lists there.
func (p *projects) setDockMenu() {
	mygo.App.Dock.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
		{Label: "Open Folder…", Click: func(*mygo.MenuItem, *mygo.Window) { p.choose() }},
	}))
}
