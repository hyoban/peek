package app

import (
	"path"

	"github.com/egoist/mygo"
)

// appActive is whether Peek is the frontmost app, as AppKit tells, on the
// main thread.
var appActive = true

// followApp keeps appActive, and clears the notifications of what the
// user comes back to see, as Mail and Messages do.
func followApp() {
	mygo.App.OnDidBecomeActive(func() {
		appActive = true
		mygo.ClearNotifications()
	})
	mygo.App.OnDidResignActive(func() { appActive = false })
}

// notify tells of work finished, as a push or a review, in a notification
// where the user is not looking at its window; a click brings the window
// to the front and runs then on its thread.
func (a *app) notify(title, body string, then func()) {
	if a.inline || a.win == nil || appActive && a.win.IsFocused() {
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
