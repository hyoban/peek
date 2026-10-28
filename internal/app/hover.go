package app

import (
	"context"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"peek/internal/definition"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"
)

type hoverClient interface {
	Hover(context.Context, string, string, int, int) (definition.Hover, error)
}

// hover shows what the language server says of the text under the
// pointer, as its type, in the files the definitions are looked up in.
func (a *app) hover(req editor.HoverRequest) {
	if !a.canDefine() {
		return
	}
	n := &a.navigation
	if n.client == nil {
		n.client = definition.New(a.repo)
	}
	a.showHover(n.client, req, 0)
}

// packageHover shows what npmx says of the dependency under the pointer:
// its links, and whether its versions are signed by their builds.
func (a *app) packageHover(req editor.HoverRequest) {
	if a.canOpenPackage() {
		a.showHover(a.packageClient(), req, 0)
	}
}

// showHover shows the hover of req, its types expanded level times, with
// buttons that expand and collapse them a level, as VS Code's.
func (a *app) showHover(client hoverClient, req editor.HoverRequest, level int) {
	// A query canceled stops its server, so a hover the pointer has left
	// is not canceled: it is skipped if it has not started yet.
	n := &a.navigation
	seq := n.hovers.Add(1)
	code, hovers := a.code, &n.hovers
	text, path := code.Buffer.String(), a.sourcePath(a.openFile)
	a.background(func() func() {
		if hovers.Load() != seq {
			return func() {}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		h, err := client.Hover(ctx, path, text, req.Offset, level)
		cancel()
		return func() {
			if err != nil || h.Markdown == "" || hovers.Load() != seq || a.code != code || code.Buffer.Version() != req.Version {
				return
			}
			start, end := wordAt(text, req.Offset)
			if h.Range != nil {
				start, end = definition.OffsetAt(text, h.Range.Start), definition.OffsetAt(text, h.Range.End)
			}
			opts := &markdown.Options{WrapCode: true, OnLink: a.hoverLink}
			code.ShowHover(start, end, "", func(c *ui.Context) {
				markdown.View(c, h.Markdown, opts)
				if h.Expandable || level > 0 {
					verbosityButtons(c, h.Expandable, level > 0, func(step int) { a.showHover(client, req, level+step) })
				}
			})
		}
	})
}

// verbosityButtons expand the types of a hover a level more, or less.
func verbosityButtons(c *ui.Context, more, less bool, step func(int)) {
	t := c.Theme()
	ui.Row(c.Key("verbosity")).FillWidth().Justify(ui.End).Gap(2).Children(func() {
		for _, b := range []struct {
			name    string
			svg     *ui.SVG
			step    int
			enabled bool
		}{
			{"Collapse Type", minusIcon, -1, less},
			{"Expand Type", plusIcon, 1, more},
		} {
			button := ui.ButtonBase(c.Key(b.name)).Size(20, 18).Shrink(0).Center().Radius(4).
				Label(b.name).Tooltip(b.name).TextColor(t.TextMuted).Disabled(!b.enabled)
			flat(button, t, false)
			button.Children(func() { ui.Icon(c, b.svg).Size(12, 12) }).OnClick(func() { step(b.step) })
		}
	})
}

// hoverLink opens a file a hover links, as a content mapper's backend
// implementation, in the files at the line its #L names; pages open in
// the browser.
func (a *app) hoverLink(link string) {
	u, err := url.Parse(link)
	if err != nil {
		return
	}
	if u.Scheme != "file" {
		if a.openURL != nil && (u.Scheme == "http" || u.Scheme == "https") {
			a.openURL(link)
		}
		return
	}
	line, _ := strconv.Atoi(strings.TrimPrefix(u.Fragment, "L"))
	at := definition.Position{Line: max(line-1, 0)}
	a.openDefinition(definition.Location{Path: u.Path, Range: definition.Range{Start: at, End: at}})
}

// wordAt is the identifier at offset, for a hover the server gives no
// range of.
func wordAt(text string, offset int) (int, int) {
	word := func(r rune) bool { return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.IsDigit(r) }
	start, end := offset, offset
	for start > 0 {
		r, n := utf8.DecodeLastRuneInString(text[:start])
		if !word(r) {
			break
		}
		start -= n
	}
	for end < len(text) {
		r, n := utf8.DecodeRuneInString(text[end:])
		if !word(r) {
			break
		}
		end += n
	}
	return start, end
}
