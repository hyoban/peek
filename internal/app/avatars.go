package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
)

// avatars loads the pictures of commit authors by their email: GitHub's
// for its noreply addresses, Gravatar's for the others. Pictures are kept
// on disk, and an empty file marks an email with none, until they are old:
// then the picture kept shows while it downloads again.
type avatars struct {
	dir     string
	url     func(email, hash string) string
	loaded  map[string]*ui.Bitmap // nil for an email with no picture
	pending map[string]bool
	// shown are the pictures drawn: false after the frame that first
	// drew one, which gave it to the GPU, true once it let go of its
	// pixels in memory (shown).
	shown  map[*ui.Bitmap]bool
	slots  chan struct{} // limits the downloads at once
	client *http.Client
}

func newAvatars() *avatars {
	dir, err := os.UserCacheDir()
	if err == nil {
		dir = filepath.Join(dir, "peek", "avatars")
		os.MkdirAll(dir, 0o755)
	}
	return &avatars{
		dir:     dir,
		url:     avatarURL,
		loaded:  map[string]*ui.Bitmap{},
		pending: map[string]bool{},
		shown:   map[*ui.Bitmap]bool{},
		slots:   make(chan struct{}, 6),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
}

// How long a picture kept on disk lasts before it downloads again, and the
// shorter time for an email with none, which may since have gained one.
const (
	pictureAge = 7 * 24 * time.Hour
	missingAge = 24 * time.Hour
)

// avatar returns the picture of the email, nil until it loads or where
// there is none. It runs on the UI thread, which alone touches the maps.
func (a *app) avatar(email string) *ui.Bitmap {
	av := a.avatars
	if av == nil || email == "" {
		return nil
	}
	email = strings.ToLower(strings.TrimSpace(email))
	if bm, ok := av.loaded[email]; ok || av.pending[email] {
		return bm
	}
	av.pending[email] = true
	a.background(func() func() {
		bm, stale := av.load(email)
		return func() {
			av.loaded[email] = bm
			if !stale {
				delete(av.pending, email)
				return
			}
			a.background(func() func() {
				bm, changed := av.refresh(email)
				return func() {
					delete(av.pending, email)
					if changed {
						av.replace(email, bm)
					}
				}
			})
		}
	})
	return nil
}

// load returns the picture of the email kept on disk, or downloads it
// where there is none, and whether the one kept is old.
func (av *avatars) load(email string) (*ui.Bitmap, bool) {
	key, file := av.file(email)
	if file != "" {
		if info, err := os.Stat(file); err == nil {
			if data, err := os.ReadFile(file); err == nil {
				age := pictureAge
				if len(data) == 0 {
					age = missingAge
				}
				return decodeAvatar(data), time.Since(info.ModTime()) > age
			}
		}
	}
	data, err := av.download(email, key, file)
	if err != nil {
		return nil, false // offline: try again next launch
	}
	return decodeAvatar(data), false
}

// refresh downloads the picture of the email again, and returns it where
// it differs from the one kept.
func (av *avatars) refresh(email string) (*ui.Bitmap, bool) {
	key, file := av.file(email)
	old, _ := os.ReadFile(file)
	data, err := av.download(email, key, file)
	if err != nil || bytes.Equal(data, old) {
		return nil, false
	}
	return decodeAvatar(data), true
}

// replace shows the picture of the email in place of the one before. On
// the UI thread.
func (av *avatars) replace(email string, bm *ui.Bitmap) {
	if old := av.loaded[email]; old != nil {
		delete(av.shown, old)
	}
	av.loaded[email] = bm
}

// file returns the key of the email and the file keeping its picture,
// empty where there is no cache.
func (av *avatars) file(email string) (key, file string) {
	sum := sha256.Sum256([]byte(email))
	key = hex.EncodeToString(sum[:])
	if av.dir == "" {
		return key, ""
	}
	return key, filepath.Join(av.dir, key)
}

// download fetches the picture of the email and keeps it in file, which
// it writes again where the picture is unchanged, to make it new.
func (av *avatars) download(email, key, file string) ([]byte, error) {
	av.slots <- struct{}{}
	data, err := av.fetch(av.url(email, key))
	<-av.slots
	if err != nil {
		return nil, err
	}
	if file != "" {
		os.WriteFile(file, data, 0o644)
	}
	return data, nil
}

func decodeAvatar(data []byte) *ui.Bitmap {
	if len(data) == 0 {
		return nil
	}
	bm, err := ui.DecodeBitmap(data)
	if err != nil {
		return nil
	}
	return bm
}

// show notes a frame drawing the picture: from the next, which comes
// after the GPU took it, it lets go of its pixels in memory, decoding
// them again where a renderer needs them. On the UI thread.
func (av *avatars) show(bm *ui.Bitmap) {
	switch released, drawn := av.shown[bm]; {
	case !drawn:
		av.shown[bm] = false
	case !released:
		bm.ReleasePixels()
		av.shown[bm] = true
	}
}

// fetch returns the picture at url, empty where there is none.
func (av *avatars) fetch(url string) ([]byte, error) {
	resp, err := av.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return []byte{}, nil
	case resp.StatusCode != http.StatusOK:
		return nil, fmt.Errorf("%s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 1<<20))
}

var noreply = regexp.MustCompile(`^(?:(\d+)\+)?([^@]+)@users\.noreply\.github\.com$`)

func avatarURL(email, hash string) string {
	if m := noreply.FindStringSubmatch(email); m != nil {
		if m[1] != "" {
			return "https://avatars.githubusercontent.com/u/" + m[1] + "?s=64"
		}
		return "https://github.com/" + m[2] + ".png?size=64"
	}
	return "https://www.gravatar.com/avatar/" + hash + "?s=64&d=404"
}
