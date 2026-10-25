package app

import (
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
)

// The files shown as pictures: bitmaps in both modes, and SVGs in the
// files mode, whose diffs read better as their text.
var (
	bitmapExts = []string{".png", ".jpg", ".jpeg", ".gif", ".webp", ".bmp"}
	vectorExts = []string{".svg"}
)

const maxPictureSize = 32 << 20

func extIn(p string, exts []string) bool {
	return slices.Contains(exts, strings.ToLower(path.Ext(p)))
}

func isBitmap(p string) bool  { return extIn(p, bitmapExts) }
func isPicture(p string) bool { return isBitmap(p) || extIn(p, vectorExts) }

// decodePicture reads the file's picture, or tells why it does not show.
func decodePicture(p, data string) (ui.ImageSource, string) {
	if len(data) > maxPictureSize {
		return nil, "This image is too large to show"
	}
	var (
		src ui.ImageSource
		err error
	)
	if isBitmap(p) {
		src, err = ui.DecodeBitmap([]byte(data))
	} else {
		src, err = ui.ParseSVG([]byte(data))
	}
	if err != nil {
		return nil, "Could not read this image"
	}
	return src, ""
}

// fileView reads the picture of a file the files mode opens, or tells
// why the file does not show; neither where its text shows.
func fileView(p, text string, err error) (ui.ImageSource, string) {
	switch {
	case err != nil:
		return nil, "Could not read this file"
	case isPicture(p):
		return decodePicture(p, text)
	}
	return nil, textNote(text)
}

// pictureSize says the picture's size in pixels, as "64 × 64".
func pictureSize(src ui.ImageSource) string {
	var w, h float32
	switch s := src.(type) {
	case *ui.Bitmap:
		bw, bh := s.Size()
		w, h = float32(bw), float32(bh)
	case *ui.SVG:
		w, h = s.Size()
	}
	return fmt.Sprintf("%g × %g", w, h)
}

// pictureDiff is a changed picture, before and after the commit, nil on
// the side where the file is not.
type pictureDiff struct {
	before, after ui.ImageSource
}

// decodePictures reads the pictures of a changed file, or tells why they
// do not show.
func decodePictures(p, before, after string) (*pictureDiff, string) {
	pd := &pictureDiff{}
	for _, side := range []struct {
		data string
		src  *ui.ImageSource
	}{{before, &pd.before}, {after, &pd.after}} {
		if side.data == "" {
			continue
		}
		src, note := decodePicture(p, side.data)
		if note != "" {
			return nil, note
		}
		*side.src = src
	}
	return pd, ""
}

const maxPictureHeight = 480

// pictureView shows the picture at its size, or smaller where it does
// not fit, over its caption.
func pictureView(c *ui.Context, src ui.ImageSource, caption string, color ui.Color) {
	t := c.Theme()
	ui.Column(c).Grow(1).Basis(0).MinWidth(0).AlignItems(ui.Center).Gap(t.Space(2)).Children(func() {
		ui.Image(c, src).Fit(ui.ScaleDown).MaxHeight(maxPictureHeight).MaxWidthPercent(100).
			BorderColor(t.Border.Alpha(0.6)).BorderWidth(1)
		ui.Text(c, caption).FontSize(smallSize).TextColor(color).SingleLine()
	})
}

// pictureDiffView shows the picture before the commit beside the one
// after, as GitHub's 2-up.
func pictureDiffView(c *ui.Context, pd *pictureDiff) {
	t := c.Theme()
	ui.Row(c).FillWidth().AlignItems(ui.Start).Padding(t.Space(4)).Gap(t.Space(4)).Children(func() {
		if pd.before != nil {
			pictureView(c, pd.before, "Before · "+pictureSize(pd.before), statusColor(t, "D"))
		}
		if pd.after != nil {
			pictureView(c, pd.after, "After · "+pictureSize(pd.after), statusColor(t, "A"))
		}
	})
}
