package app

import (
	"fmt"
	"path"

	"peek/internal/git"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// confirmState is an alert asking before a change that cannot be undone,
// as discarding, over the window until the user answers it; shown, once
// AppKit shows it.
type confirmState struct {
	open, shown            bool
	title, message, button string
	do                     func()
}

// confirm asks in an alert before doing what button names, as AppKit's
// with a destructive button and Cancel.
func (a *app) confirm(title, message, button string, do func()) {
	a.confirming = confirmState{open: true, title: title, message: message, button: button, do: do}
}

// confirmations shows the alert as alerts does: in an NSAlert, a sheet of
// the window, where there are windows.
func (a *app) confirmations(c *ui.Context) {
	if !a.native() {
		if ui.AlertDialog(c.Key("confirm"), &a.confirming.open, a.confirming.title, a.confirming.message, "Cancel", a.confirming.button) == 1 {
			a.confirming.do()
		}
		return
	}
	if !a.confirming.open || a.confirming.shown {
		return
	}
	a.confirming.shown = true
	asked, win := a.confirming, a.win
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: win, Type: mygo.MessageWarning,
			Message: asked.title, Detail: asked.message, Buttons: []string{asked.button, "Cancel"}, CancelButton: 1})
		win.Update(func() {
			a.confirming = confirmState{}
			if err == nil && res.Button == 0 {
				asked.do()
			}
		})
	}()
}

// discardable reports whether the changes shown can be discarded: the
// work tree's, with nothing running that commits or checks them out.
func (a *app) discardable() bool {
	cm, ok := a.current()
	busy := a.push.running || a.review.running || a.pull.running || a.switcher.running != "" || a.discarding
	return ok && cm.Hash == git.Worktree && a.mode == modeChanges && len(a.diffs) > 0 && !busy
}

// discardMenu is the item of a context menu that discards the changes to
// the files of the diffs, after asking.
func (a *app) discardMenu(m *ui.Menu, diffs []*fileDiff) {
	if m.Item("Discard Changes…").Disabled(!a.discardable() || len(diffs) == 0).Chosen() {
		a.confirmDiscard(diffs)
	}
}

// confirmDiscard asks before discarding the changes to the files of the
// diffs, all the changes where they are all of them, as Xcode does.
func (a *app) confirmDiscard(diffs []*fileDiff) {
	if !a.discardable() || len(diffs) == 0 {
		return
	}
	changes := make([]git.Change, len(diffs))
	for i, d := range diffs {
		changes[i] = d.change
	}
	title := fmt.Sprintf("Discard changes to %d files?", len(changes))
	switch {
	case len(changes) == len(a.diffs):
		title = "Discard all changes?"
	case len(changes) == 1:
		title = fmt.Sprintf("Discard changes to “%s”?", path.Base(changes[0].Path))
	}
	const message = "The files go back to how the last commit has them, and new files are deleted. You can’t undo this."
	a.confirm(title, message, "Discard Changes", func() { a.discard(changes) })
}

// discard discards the changes off the UI thread, then shows the work
// tree as it is.
func (a *app) discard(changes []git.Change) {
	if a.discarding {
		return
	}
	a.discarding = true
	repo := a.repo
	a.background(func() func() {
		err := git.Discard(repo, changes)
		return func() {
			a.discarding = false
			if err != nil {
				a.showAlert("Couldn’t Discard Changes", err.Error())
			}
			a.refresh()
		}
	})
}
