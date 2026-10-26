package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// The system colors of macOS, light and dark, and with Increase Contrast,
// as NSColor's systemGreen and the others resolve in each appearance.
var (
	systemGreen  = colors{"#28cd41", "#32d74b", "#007d1b", "#5fe07a"}
	systemRed    = colors{"#ff3b30", "#ff453a", "#d70015", "#ff6961"}
	systemOrange = colors{"#ff9500", "#ff9f0a", "#c93400", "#ffb340"}
	systemPurple = colors{"#af52de", "#bf5af2", "#8944ab", "#da8fff"}
)

// colors is a system color in light, dark, and the two with Increase
// Contrast.
type colors [4]string

func system(c *ui.Context, sc colors) ui.Color {
	i := 0
	if c.Theme().Dark {
		i = 1
	}
	if c.Preferences().HighContrast {
		i += 2
	}
	return ui.Hex(sc[i])
}

// statusColor colors a change as Xcode's source control does.
func statusColor(c *ui.Context, s string) ui.Color {
	switch s {
	case "A":
		return system(c, systemGreen)
	case "D":
		return system(c, systemRed)
	case "R", "C":
		return system(c, systemPurple)
	}
	return system(c, systemOrange)
}

// flat gives a borderless button the face of its state, as toolbars'
// items have: pressed, or down where it holds a popover open, and nothing
// under the pointer.
func flat(b ui.Element, t *ui.Theme, down bool) {
	if down || b.Pressed() {
		b.Background(t.SurfacePressed)
	}
}

// unhovered keeps buttons as they are under the pointer, as AppKit's: the
// theme's hover colors are those of their rest.
func unhovered(t *ui.Theme) {
	t.SurfaceHover, t.AccentHover = t.Surface, t.Accent
}

// withFocus builds a list in the theme its focus asks for.
func (a *app) withFocus(c *ui.Context, focused bool, build func()) {
	if focused {
		c.SetTheme(a.listTheme)
	} else {
		c.SetTheme(a.inactiveTheme)
	}
	build()
	c.SetTheme(a.theme)
}

// shortDate tells when, as Mail's list does: the time today, then
// Yesterday, the day of the week within a week, and the date before.
func shortDate(t, now time.Time) string {
	y, m, d := now.Date()
	today := time.Date(y, m, d, 0, 0, 0, 0, now.Location())
	switch {
	case !t.Before(today):
		return t.Format("15:04")
	case !t.Before(today.AddDate(0, 0, -1)):
		return "Yesterday"
	case !t.Before(today.AddDate(0, 0, -6)):
		return t.Format("Monday")
	case t.Year() == now.Year():
		return t.Format("Jan 2")
	}
	return t.Format("2006/1/2")
}

// toolButton is a button of the toolbar as the state has it: what it
// shows, whether it may be clicked and what a click does; or, while its
// work is under way (busy), what that is, and the button stopping it,
// named stop.
type toolButton struct {
	label, tooltip string
	disabled       bool
	click          func()
	busy           bool
	// elapsed is how long the work under way has taken, stop names the
	// button that stops it and cancel stops it.
	elapsed string
	stop    string
	cancel  func()
}

// drawTool draws a button of the toolbar, or the activity of its work.
func drawTool(c *ui.Context, key string, b toolButton) {
	if b.busy {
		text := b.label
		if b.elapsed != "" {
			text += " " + b.elapsed
		}
		activity(c, key, text, b.tooltip, b.stop, b.cancel)
		return
	}
	ui.Button(c.Key(key), b.label).Shrink(0).Disabled(b.disabled).Tooltip(b.tooltip).OnClick(b.click)
}

// activity is a task under way in the toolbar, as Xcode's activity view
// beside its Stop button: a small spinner, what it does, muted, and the
// button that stops it, named stop.
func activity(c *ui.Context, key, text, tooltip, stop string, cancel func()) {
	t := c.Theme()
	ui.Row(c.Key(key)).Shrink(1).MinWidth(0).Gap(t.Space(1.5)).AlignItems(ui.Center).Tooltip(tooltip).Children(func() {
		ui.Spinner(c).Size(12, 12).Shrink(0)
		ui.Text(c, text).TextColor(t.TextMuted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	})
	ui.Button(c.Key(key+"-stop"), "").Shrink(0).Label(stop).Tooltip(stop).
		Children(func() { ui.Icon(c, stopIcon).Size(12, 12) }).OnClick(cancel)
}

// alertState is an alert about what failed meanwhile, such as a push,
// over the window until the user answers it; shown, once AppKit shows it.
type alertState struct {
	open, shown    bool
	title, message string
}

// showAlert tells of a failure in an alert, as AppKit's.
func (a *app) showAlert(title, message string) {
	a.alert = alertState{open: true, title: title, message: message}
}

// alerts shows the alert as the project shows: in an NSAlert, a sheet of
// the window, where there are windows.
func (a *app) alerts(c *ui.Context) {
	if !a.native() {
		ui.AlertDialog(c.Key("alert"), &a.alert.open, a.alert.title, a.alert.message, "OK")
		return
	}
	if !a.alert.open || a.alert.shown {
		return
	}
	a.alert.shown = true
	alert, win := a.alert, a.win
	go func() {
		mygo.Dialog.Message(mygo.MessageOptions{Parent: win, Type: mygo.MessageWarning, Message: alert.title, Detail: alert.message})
		win.Update(func() {
			if a.alert == alert {
				a.alert = alertState{}
			}
		})
	}()
}

// xcodeGrays gives the theme the neutral grays of Xcode's and AppKit's
// windows in place of its own, cooler and darker ones: the text over the
// editor's gray, the controls' fills and the separators between them,
// and the labels' grays, as they are over it. The accent is the
// system's still, and Increase Contrast sharpens them as it does the
// theme's.
func xcodeGrays(t *ui.Theme, highContrast bool) {
	if t.Dark {
		t.Background = ui.Hex("#262626")
		t.Surface, t.SurfaceHover, t.SurfacePressed = ui.Hex("#323232"), ui.Hex("#3a3a3a"), ui.Hex("#454545")
		t.Border = ui.Hex("#3c3c3c")
		t.Text, t.TextMuted = ui.Hex("#dfdfdf"), ui.Hex("#9f9f9f")
	} else {
		t.Background = ui.Hex("#ffffff")
		t.Surface, t.SurfaceHover, t.SurfacePressed = ui.Hex("#f2f2f2"), ui.Hex("#e8e8e8"), ui.Hex("#dddddd")
		t.Border = ui.Hex("#e3e3e3")
		t.Text, t.TextMuted = ui.Hex("#262626"), ui.Hex("#808080")
	}
	if highContrast {
		t.Border = t.Border.Mix(t.Text, 0.45)
		t.TextMuted = t.TextMuted.Mix(t.Text, 0.4)
	}
}

// chromeColor is the band of the toolbar and the jump bar over the text,
// a shade off the text's, as Xcode's over its editor.
func chromeColor(t *ui.Theme) ui.Color {
	if t.Dark {
		return t.Background.Mix(ui.Hex("#ffffff"), 0.035)
	}
	return t.Background.Mix(ui.Hex("#000000"), 0.025)
}
