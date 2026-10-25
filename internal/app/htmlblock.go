package app

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"golang.org/x/net/html"
)

// htmlImage is a picture an HTML block of Markdown shows, as READMEs put
// them: an <img>, or a <picture> whose <source>s tell the one for dark
// appearance from the one for light.
type htmlImage struct {
	src, alt string
	width    float32 // the width it asks for, 0 for its own
}

// htmlImages reads the pictures of an HTML block for the appearance, and
// whether it centers them, as <p align="center"> and <div align="center">
// do; none where the block holds anything else but links around them.
func htmlImages(block string, dark bool) (images []htmlImage, centered bool) {
	z := html.NewTokenizer(strings.NewReader(block))
	inPicture, chosen := false, ""
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			return images, centered
		case html.TextToken:
			if strings.TrimSpace(string(z.Text())) != "" {
				return nil, false
			}
			continue
		case html.CommentToken, html.DoctypeToken:
			continue
		}
		tok := z.Token()
		attr := func(name string) string {
			for _, at := range tok.Attr {
				if at.Key == name {
					return at.Val
				}
			}
			return ""
		}
		switch tok.Data {
		case "picture":
			inPicture, chosen = tt == html.StartTagToken, ""
		case "source":
			media := strings.ReplaceAll(attr("media"), " ", "")
			if inPicture && chosen == "" && (dark && strings.Contains(media, "prefers-color-scheme:dark") ||
				!dark && strings.Contains(media, "prefers-color-scheme:light")) {
				chosen = firstSrc(attr("srcset"))
			}
		case "img":
			im := htmlImage{src: attr("src"), alt: attr("alt")}
			if inPicture && chosen != "" {
				im.src = chosen
			}
			if w, err := strconv.ParseFloat(strings.TrimSuffix(attr("width"), "px"), 32); err == nil && w > 0 {
				im.width = float32(w)
			}
			if im.src != "" {
				images = append(images, im)
			}
		case "p", "div":
			if strings.EqualFold(attr("align"), "center") {
				centered = true
			}
		case "a", "br", "span":
		default:
			return nil, false
		}
	}
}

// firstSrc is the address of a srcset's first picture.
func firstSrc(srcset string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(srcset), ",")
	if f := strings.Fields(first); len(f) > 0 {
		return f[0]
	}
	return ""
}

// htmlBlock shows the pictures of an HTML block of Markdown, which load
// asks for those not loaded yet, or else its HTML, as the views show it.
func (a *app) htmlBlock(c *ui.Context, md *mdPreview, block string, load func(url string)) {
	t := c.Theme()
	images, centered := htmlImages(block, t.Dark)
	if len(images) == 0 {
		ui.Text(c, block).Font(monoFamily).FontSize(codeSize).TextColor(t.TextMuted)
		return
	}
	row := ui.Row(c).Wrap().Gap(t.Space(2)).AlignSelf(ui.Stretch)
	if centered {
		row.Justify(ui.Center)
	}
	row.Children(func() {
		for _, im := range images {
			src, ok := md.pictures[im.src]
			if !ok {
				md.pictures[im.src] = nil // loading
				load(im.src)
			}
			if src == nil {
				if im.alt != "" {
					ui.Text(c, im.alt).FontSize(smallSize).TextColor(t.TextMuted).
						Padding(t.Space(2), t.Space(3)).Radius(5).Background(t.Text.Alpha(0.05))
				}
				continue
			}
			img := ui.Image(c, src).Radius(5).MaxWidthPercent(100).Label(im.alt)
			var w, h float32
			switch s := src.(type) {
			case *ui.Bitmap:
				bw, bh := s.Size()
				w, h = float32(bw), float32(bh)
			case *ui.SVG:
				w, h = s.Size()
			}
			if w > 0 && h > 0 {
				img.AspectRatio(w / h)
			}
			if im.width > 0 {
				img.Width(im.width)
			}
		}
	})
}
