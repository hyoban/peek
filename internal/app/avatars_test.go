package app

import (
	"bytes"
	"image"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

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

func TestAvatarDownloadsAgainOnceOld(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	picture, requests := []byte(nil), 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if picture == nil {
			http.NotFound(w, r)
			return
		}
		w.Write(picture)
	}))
	defer server.Close()
	av := newAvatars()
	av.dir = t.TempDir()
	av.url = func(email, hash string) string { return server.URL }
	const email = "ada@example.com"

	if bm, stale := av.load(email); bm != nil || stale || requests != 1 {
		t.Fatalf("first load: picture %v, stale %v, %d requests", bm != nil, stale, requests)
	}
	// The author adds a picture, which shows once the empty file is old.
	picture = buf.Bytes()
	if bm, stale := av.load(email); bm != nil || stale || requests != 1 {
		t.Fatalf("load while new: picture %v, stale %v, %d requests", bm != nil, stale, requests)
	}
	_, file := av.file(email)
	day := time.Now().Add(-missingAge - time.Minute)
	if err := os.Chtimes(file, day, day); err != nil {
		t.Fatal(err)
	}
	if _, stale := av.load(email); !stale {
		t.Fatal("an empty file older than a day is not stale")
	}
	if bm, changed := av.refresh(email); bm == nil || !changed || requests != 2 {
		t.Fatalf("refresh: picture %v, changed %v, %d requests", bm != nil, changed, requests)
	}
	// The picture downloaded again is unchanged, and is new again.
	if bm, stale := av.load(email); bm == nil || stale {
		t.Fatalf("load after refresh: picture %v, stale %v", bm != nil, stale)
	}
	week := time.Now().Add(-pictureAge - time.Minute)
	if err := os.Chtimes(file, week, week); err != nil {
		t.Fatal(err)
	}
	if bm, changed := av.refresh(email); bm != nil || changed || requests != 3 {
		t.Fatalf("unchanged refresh: picture %v, changed %v, %d requests", bm != nil, changed, requests)
	}
	if _, stale := av.load(email); stale {
		t.Error("an unchanged picture stays stale after downloading it again")
	}
}
