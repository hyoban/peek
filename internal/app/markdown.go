package app

import (
	"errors"
	"maps"
	"net/url"
	"path"
	"strings"
	"sync"

	"peek/internal/git"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

var markdownExts = []string{".md", ".markdown", ".mdx"}

func isMarkdown(p string) bool { return extIn(p, markdownExts) }

// mdPreview is the rendered Markdown of the file the files mode shows,
// which a file shows once asked, its source showing until then.
type mdPreview struct {
	text string // the file's source
	opts markdown.Options
	// pictures is those of its HTML, by their addresses, nil while they
	// load and where they do not.
	pictures map[string]ui.ImageSource
	// read is the files of the pictures it read, by their paths, for a
	// refresh to tell whether they changed; the views read them on
	// goroutines of their own.
	mu   sync.Mutex
	read map[string]string
	repo string
	hash string
}

// newPreview is the preview of the Markdown file of the commit, its
// pictures read from the commit too.
func (a *app) newPreview(hash, p, text string) *mdPreview {
	md := &mdPreview{text: text, pictures: map[string]ui.ImageSource{}, read: map[string]string{}, repo: a.repo, hash: hash}
	load := func(u string) (ui.ImageSource, error) {
		pic, ok := picturePath(p, u)
		if !ok {
			return nil, errors.New("not a picture of the repository")
		}
		data, err := git.FileText(md.repo, hash, pic)
		md.mu.Lock()
		md.read[pic] = data
		md.mu.Unlock()
		if err != nil {
			return nil, err
		}
		src, note := decodePicture(pic, data)
		if note != "" {
			return nil, errors.New(note)
		}
		return src, nil
	}
	md.opts = markdown.Options{
		Theme:     &markdown.Theme{CodeFont: monoFamily},
		WrapCode:  true,
		LoadImage: load,
		HTML: func(c *ui.Context, html string) {
			a.htmlBlock(c, md, html, func(u string) {
				a.background(func() func() {
					src, _ := load(u)
					return func() { md.pictures[u] = src }
				})
			})
		},
	}
	return md
}

// stale reports, off the UI thread, whether the preview no longer shows
// the file's text, or the pictures it read.
func (md *mdPreview) stale(text string) bool {
	if text != md.text {
		return true
	}
	md.mu.Lock()
	read := maps.Clone(md.read)
	md.mu.Unlock()
	for pic, data := range read {
		if now, _ := git.FileText(md.repo, md.hash, pic); now != data {
			return true
		}
	}
	return false
}

// picturePath is the path of a picture the Markdown file links to, a
// path within the repository: relative to the file, or to the root where
// it starts with a slash.
func picturePath(file, link string) (string, bool) {
	u, err := url.Parse(link)
	if err != nil || u.Scheme != "" || u.Host != "" || u.Path == "" {
		return "", false
	}
	p := u.Path
	if strings.HasPrefix(p, "/") {
		p = path.Clean(p[1:])
	} else {
		p = path.Join(path.Dir(file), p)
	}
	if p == ".." || strings.HasPrefix(p, "../") || !isPicture(p) {
		return "", false
	}
	return p, true
}

// previewing reports whether the file open shows its preview.
func (a *app) previewing() bool {
	return a.preview != nil && a.previewed[a.openFile]
}

// previewButton switches the Markdown file open between its source and
// its preview, which it keeps for the file as long as the app runs.
func (a *app) previewButton(c *ui.Context) {
	on := a.previewed[a.openFile]
	label := "Show Preview"
	if on {
		label = "Show Source"
	}
	p := a.openFile
	t := c.Theme()
	// Borderless, as the buttons of Xcode's jump bar: the icon alone, a
	// fill under the pointer, and the accent while the preview shows.
	b := ui.ButtonBase(c.Key("preview")).Size(22, 22).Shrink(0).Center().Radius(5).
		Label(label).Tooltip(label)
	flat(b, t, false)
	color := t.TextMuted
	if on {
		color = t.Accent
		if !b.Hovered() && !b.Pressed() {
			b.Background(t.Accent.Alpha(0.12))
		}
	}
	b.OnClick(func() {
		if a.previewed == nil {
			a.previewed = map[string]bool{}
		}
		a.previewed[p] = !on
	})
	b.Children(func() { ui.Icon(c, previewIcon).Size(15, 15).TextColor(color) })
}

// previewView shows the Markdown as it reads, in a column no wider than
// GitHub shows a README.
func previewView(c *ui.Context, md *mdPreview) {
	t := c.Theme()
	ui.Scroll(c.Key("preview")).Grow(1).FillWidth().Children(func() {
		ui.Column(c).FillWidth().AlignItems(ui.Center).Padding(t.Space(6), t.Space(8)).Children(func() {
			ui.Column(c).FillWidth().MaxWidth(880).Children(func() {
				markdown.View(c, md.text, &md.opts)
			})
		})
	})
}
