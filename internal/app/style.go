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

// flat gives a borderless button the faces of its states, as toolbars'
// items have: pressed, or down where it holds a popover open, and hovered.
func flat(b ui.Element, t *ui.Theme, down bool) {
	switch {
	case down || b.Pressed():
		b.Background(t.SurfacePressed)
	case b.Hovered():
		b.Background(t.SurfaceHover)
	}
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

// sheet shows a dialog while *open is true, as AppKit's sheets: AppKit's
// own, in a window of its own, where there are windows, else attached
// under the toolbar, what is behind it waiting and a click there doing
// nothing. Escape and Return close it, Done being its default button,
// last of the buttons that buttons builds.
func (a *app) sheet(c *ui.Context, ch *child, open *bool, label string, width, height float32, body, buttons func(c *ui.Context)) {
	content := func(c *ui.Context, panel ui.Element) {
		t := c.Theme()
		panel.Gap(t.Space(3)).Padding(t.Space(5)).Role(ui.RoleDialog).Label(label).Children(func() {
			body(c)
			ui.Row(c).FillWidth().Shrink(0).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
				buttons(c)
				ui.PrimaryButton(c.Key("done"), "Done").OnClick(func() { *open = false })
			})
		})
	}
	if a.native() {
		a.nativeSheet(ch, open, int(width), int(height), func(c *ui.Context) {
			content(c, ui.Column(c).Fill().MinHeight(0))
			if c.Shortcut(0, ui.KeyEscape) || c.Shortcut(0, ui.KeyEnter) {
				*open = false
			}
		})
		return
	}
	if !*open {
		return
	}
	t := c.Theme()
	ui.Overlay(c, func() {
		back := ui.Column(c.Key("sheet:"+label)).Absolute().Left(0).Top(0).Right(0).Bottom(0).
			AlignItems(ui.Center).Padding(titleBarHeight, t.Space(4), t.Space(4), t.Space(4)).Modal()
		back.Children(func() {
			content(c, ui.Column(c).Width(width).Height(height).Shrink(1).MinHeight(0).MaxWidthPercent(100).
				Radius(12).Background(t.Background).Border(1, t.Border).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.25)))
		})
		if back.OverlayShortcut(0, ui.KeyEscape) || back.OverlayShortcut(0, ui.KeyEnter) {
			*open = false
		}
	})
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

// searchField is a search field as AppKit's, as ui.SearchField draws one,
// with a placeholder of its own and its input bound, for the menu bar to
// put the keys in it and for Down to leave it: a magnifying glass, the
// text, and a button clearing it while it holds any, as Escape does then.
func searchField(c *ui.Context, key string, query *string, placeholder, label string, input *ui.Handle, focus *bool, down func()) ui.Element {
	t := c.Theme()
	focused := input.FocusWithin(c)
	f := ui.Row(c.Key(key)).AlignItems(ui.Center).Gap(t.Space(1.5)).Padding(t.Space(1), t.Space(2)).
		Radius(t.Radius).Background(t.Surface).Border(1, t.Border)
	if focused {
		f.Border(1, t.Accent).Shadow(0, 0, 0, 3, t.Focus)
	}
	f.Children(func() {
		ui.Icon(c, searchIcon).Size(14, 14).Shrink(0).TextColor(t.TextMuted)
		in := ui.TextInputBase(c.Key(key+":input"), query).Bind(input).Grow(1).MinWidth(0).
			Placeholder(placeholder).Label(label).OnTextKey(func(k ui.TextKey) bool {
			switch {
			case k.Mods == 0 && k.Key == ui.KeyDown:
				down()
			case k.Mods == 0 && k.Key == ui.KeyEscape && *query != "":
				k.SetText("")
				*query = ""
			default:
				return false
			}
			return true
		})
		if *focus {
			*focus = false
			in.Focus()
		}
		if *query != "" {
			clear := ui.ButtonBase(c.Key(key+":clear")).Size(16, 16).Shrink(0).Center().Radius(8).
				Background(t.TextMuted.Alpha(0.5)).Label("Clear").KeepFocus()
			clear.OnClick(func() { *query = "" })
			clear.Children(func() { ui.Icon(c, clearIcon).Size(8, 8).TextColor(t.Background) })
		}
	})
	return f
}
