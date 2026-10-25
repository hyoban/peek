package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// The system colors of macOS, which differ a little in the dark.
var (
	systemGreen  = [2]ui.Color{ui.Hex("#28a745"), ui.Hex("#30d158")}
	systemRed    = [2]ui.Color{ui.Hex("#ff3b30"), ui.Hex("#ff453a")}
	systemOrange = [2]ui.Color{ui.Hex("#f08c00"), ui.Hex("#ff9f0a")}
	systemPurple = [2]ui.Color{ui.Hex("#af52de"), ui.Hex("#bf5af2")}
)

func system(t *ui.Theme, c [2]ui.Color) ui.Color {
	if t.Dark {
		return c[1]
	}
	return c[0]
}

// statusColor colors a change as Xcode's source control does.
func statusColor(t *ui.Theme, s string) ui.Color {
	switch s {
	case "A":
		return system(t, systemGreen)
	case "D":
		return system(t, systemRed)
	case "R", "C":
		return system(t, systemPurple)
	}
	return system(t, systemOrange)
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
