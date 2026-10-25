package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// codexTask is Codex's work for a button of the toolbar, Review's or
// Explain's: whether it is under way, what stops it, its stage as Codex's
// events tell it, and since when.
type codexTask struct {
	running       bool
	cancel        context.CancelFunc
	phase, detail string
	started       time.Time
}

func (k *codexTask) stop() {
	if k.cancel != nil {
		k.cancel()
	}
}

// finish marks the task done, as its result comes.
func (k *codexTask) finish() { k.running, k.cancel = false, nil }

// activity shows the task under way in the toolbar: its stage and how
// long it has taken so far, else text, and the button that stops it.
func (k *codexTask) activity(c *ui.Context, key, text, tooltip, stop string) {
	if k.phase != "" {
		if k.started.IsZero() {
			k.started = c.Now()
		}
		elapsed := c.Now().Sub(k.started).Truncate(time.Second)
		text, tooltip = fmt.Sprintf("%s %s", k.phase, elapsed), k.detail
		c.After(time.Second)
	}
	activity(c, key, text, tooltip, stop, k.stop)
}

// codexProgress reports the task's stages from off the UI thread, while
// it runs and current tells that it is still the one they are of.
func (a *app) codexProgress(ctx context.Context, k *codexTask, current func() bool) func(phase, detail string) {
	win, inline := a.win, a.inline
	return func(phase, detail string) {
		if inline {
			return
		}
		win.Update(func() {
			if ctx.Err() == nil && k.running && current() {
				k.phase, k.detail = phase, detail
			}
		})
	}
}

// changing reports whether the repository is being changed: shipped,
// pulled or checked out, which Codex waits for, to read it as it is.
func (a *app) changing() bool {
	return a.push.running || a.pull.running || a.switcher.running != ""
}

// resultSheet shows what Codex answered in a sheet: its title and scope
// over body, Copy apart at the start, copying text, and the button named
// again, which asks once more, before Done.
func resultSheet(c *ui.Context, open *bool, title, scope string, body func(), text, again string, disabled bool, onAgain func()) {
	t := c.Theme()
	services := c.Services()
	sheet(c, open, title, 620, 480, func() {
		ui.Text(c, title).FontWeight(600).FontSize(fontSize + 3).Shrink(0)
		ui.Text(c, scope).TextColor(t.TextMuted).Shrink(0)
		ui.Scroll(c).Grow(1).FillWidth().MinHeight(0).Children(func() {
			ui.Column(c).FillWidth().Gap(t.Space(3)).Children(body)
		})
	}, func() {
		// Copy apart at the start, as a sheet's secondary action, and the
		// default button last.
		ui.Button(c.Key("copy:"+title), "Copy").Disabled(text == "").OnClick(func() { services.WriteClipboard(text) })
		ui.Spacer(c)
		ui.Button(c.Key("again:"+title), again).Disabled(disabled).OnClick(onAgain)
	})
}
