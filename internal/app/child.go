package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// child is a window of AppKit's own over the project's window, a popover
// or a sheet, which shows while the *open it was opened for is true. Its
// view and the window's show the same state, so each redraws the other
// after a pass of its own; the pass a redraw asks for asks for none
// back, which would go on forever.
type child struct {
	win                   *mygo.Window
	fromParent, fromChild bool
	// build is the view's, as the window's last pass gave it.
	build func(c *ui.Context)
	// box is a popover's content, and height its height as last laid out.
	box    ui.Handle
	height int
}

// follow opens the child with open as *open turns true, closes it as it
// turns false, and redraws it after a pass of the project's window. The
// user closing it, as a click outside a popover does, sets *open false.
func (a *app) follow(ch *child, open *bool, create func(view func(c *ui.Context)) *mygo.Window, build func(c *ui.Context)) {
	ch.build = build
	switch {
	case *open && ch.win == nil:
		parent := a.win
		ch.win = create(func(c *ui.Context) {
			if ch.win == nil {
				return // closed meanwhile
			}
			a.childTheme(c)
			ch.build(c)
			if ch.fromParent {
				ch.fromParent = false
			} else {
				ch.fromChild = true
				parent.Invalidate()
			}
		})
		ch.win.OnClosed(func() {
			ch.win = nil
			*open = false
			parent.Invalidate()
		})
	case !*open && ch.win != nil:
		ch.win.Close()
	case ch.win != nil:
		if ch.fromChild {
			ch.fromChild = false
		} else {
			ch.fromParent = true
			ch.win.Invalidate()
		}
	}
}

// childTheme gives a child's pass the window's theme, its text at the
// app's size.
func (a *app) childTheme(c *ui.Context) {
	t := c.Theme()
	t.FontSize = fontSize * c.Preferences().TextScale
	c.SetTheme(t)
}

// native reports whether the project's popovers and sheets are AppKit's,
// in windows of their own: not in tests, which have no windows and see
// them in the window's view.
func (a *app) native() bool { return a.win != nil && !a.inline }

// popover shows build in an NSPopover under anchor while *open is true,
// width wide and as tall as its content: the system's material, arrow and
// animation, closing as the user clicks elsewhere or presses Escape.
func (a *app) popover(ch *child, open *bool, anchor ui.Element, width int, build func(c *ui.Context)) {
	r := anchor.Bounds()
	a.follow(ch, open, func(view func(c *ui.Context)) *mygo.Window {
		return mygo.NewFlyout(mygo.FlyoutOptions{
			Parent:    a.win,
			Anchor:    mygo.Rectangle{X: int(r.X), Y: int(r.Y), Width: int(r.W), Height: int(r.H)},
			Width:     width,
			Height:    max(ch.height, 120), // until its content is laid out
			Focusable: true,
			Popover:   true,
			Content:   ui.View(view),
		})
	}, func(c *ui.Context) {
		c.Root().Background(ui.Transparent) // over the popover's material
		ui.Column(c).Bind(&ch.box).FillWidth().Padding(popoverPadding).Children(func() { build(c) })
		// As tall as the content laid out last, which the next pass shows.
		if e := c.Resolve(ch.box); e.Valid() {
			if h := int(e.Bounds().H + 0.5); h > 0 && h != ch.height {
				ch.height = h
				ch.win.SetContentSize(width, h)
			}
		}
	})
}

// popoverPadding is the room between a popover's edge and its content.
const popoverPadding = 8

// nativeSheet shows build in a sheet of AppKit's while *open is true,
// width by height: it slides from under the toolbar, the window waiting
// behind it.
func (a *app) nativeSheet(ch *child, open *bool, width, height int, build func(c *ui.Context)) {
	a.follow(ch, open, func(view func(c *ui.Context)) *mygo.Window {
		return mygo.NewWindow(mygo.WindowOptions{
			Width:          width,
			Height:         height,
			UseContentSize: true,
			DisableResize:  true,
			Parent:         a.win,
			Modal:          true,
			Content:        ui.View(view),
		})
	}, build)
}
