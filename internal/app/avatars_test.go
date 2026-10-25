package app

import (
	"bytes"
	"image"
	"image/png"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAvatarURL(t *testing.T) {
	for email, want := range map[string]string{
		"41898282+github-actions[bot]@users.noreply.github.com": "https://avatars.githubusercontent.com/u/41898282?s=64",
		"hyoban@users.noreply.github.com":                       "https://github.com/hyoban.png?size=64",
		"ada@example.com":                                       "https://www.gravatar.com/avatar/HASH?s=64&d=404",
	} {
		if got := avatarURL(email, "HASH"); got != want {
			t.Errorf("avatarURL(%q) = %q, want %q", email, got, want)
		}
	}
}

func TestAvatarReleasesPixelsOnceShown(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	bm, err := ui.DecodeBitmap(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	av := newAvatars()
	av.show(bm)
	if released, drawn := av.shown[bm]; !drawn || released {
		t.Fatalf("after the first frame: drawn %v, released %v", drawn, released)
	}
	av.show(bm)
	av.show(bm)
	if !av.shown[bm] {
		t.Error("the picture kept its pixels after it was shown")
	}
	if w, h := bm.Size(); w != 8 || h != 8 {
		t.Errorf("the released picture is %d×%d", w, h)
	}
}
