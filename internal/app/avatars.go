package app

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"peek/internal/git"
)

// avatars loads the pictures of commit authors by their email: GitHub's
// for its noreply addresses and the emails GitHub tells the account of,
// Gravatar's for the others. Pictures are kept on disk, and an empty file
// marks an email with none, until they are old: then the picture kept
// shows while it downloads again.
type avatars struct {
	dir     string
	url     func(email, hash, user string) string
	loaded  map[string]*ui.Bitmap // nil for an email with no picture
	pending map[string]bool
	// users are the GitHub accounts' IDs by email, kept on disk, and asked
	// the repositories whose authors GitHub was asked of this launch.
	users map[string]string
	asked map[string]bool
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
	av := &avatars{
		dir:     dir,
		url:     avatarURL,
		loaded:  map[string]*ui.Bitmap{},
		pending: map[string]bool{},
		users:   map[string]string{},
		asked:   map[string]bool{},
		shown:   map[*ui.Bitmap]bool{},
		slots:   make(chan struct{}, 6),
		client:  &http.Client{Timeout: 10 * time.Second},
	}
	if data, err := os.ReadFile(av.usersFile()); err == nil {
		json.Unmarshal(data, &av.users)
	}
	return av
}

// usersFile keeps the GitHub accounts' IDs by email, "" where there is no
// cache.
func (av *avatars) usersFile() string {
	if av.dir == "" {
		return ""
	}
	return filepath.Join(av.dir, "github-users.json")
}

// learnUsers asks GitHub, once a launch, whose accounts the authors of the
// repository whose page this is are, and loads again the pictures of those
// it learns the account of where they had none.
func (a *app) learnUsers(page string) {
	av := a.avatars
	if av == nil || page == "" || av.asked[page] {
		return
	}
	av.asked[page] = true
	repo := a.repo
	a.background(func() func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		users, err := git.GitHubUsers(ctx, repo, page)
		if err != nil {
			return func() {}
		}
		return func() {
			changed := false
			for email, id := range users {
				if av.users[email] == id {
					continue
				}
				av.users[email], changed = id, true
				if bm, ok := av.loaded[email]; ok && bm == nil && !av.pending[email] {
					delete(av.loaded, email)
				}
			}
			if !changed || av.usersFile() == "" {
				return
			}
			if data, err := json.Marshal(av.users); err == nil {
				file := av.usersFile()
				go os.WriteFile(file, data, 0o644)
			}
		}
	})
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
	user := av.users[email]
	a.background(func() func() {
		bm, stale := av.load(email, user)
		return func() {
			av.loaded[email] = bm
			if !stale {
				delete(av.pending, email)
				return
			}
			a.background(func() func() {
				bm, changed := av.refresh(email, user)
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
// where there is none, and whether the one kept is old. user is the ID of
// the email's GitHub account, "" where it is not known.
func (av *avatars) load(email, user string) (*ui.Bitmap, bool) {
	key, file := av.file(email, user)
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
	data, err := av.download(email, key, user, file)
	if err != nil {
		return nil, false // offline: try again next launch
	}
	return decodeAvatar(data), false
}

// refresh downloads the picture of the email again, and returns it where
// it differs from the one kept.
func (av *avatars) refresh(email, user string) (*ui.Bitmap, bool) {
	key, file := av.file(email, user)
	old, _ := os.ReadFile(file)
	data, err := av.download(email, key, user, file)
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
// empty where there is no cache: the GitHub account's own where it is
// known, apart from the one Gravatar had or had not.
func (av *avatars) file(email, user string) (key, file string) {
	sum := sha256.Sum256([]byte(email))
	key = hex.EncodeToString(sum[:])
	if av.dir == "" {
		return key, ""
	}
	if user != "" {
		return key, filepath.Join(av.dir, "github-"+user)
	}
	return key, filepath.Join(av.dir, key)
}

// download fetches the picture of the email and keeps it in file, which
// it writes again where the picture is unchanged, to make it new.
func (av *avatars) download(email, key, user, file string) ([]byte, error) {
	av.slots <- struct{}{}
	data, err := av.fetch(av.url(email, key, user))
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

// avatarURL is where the picture of the email is: of user, the ID of its
// GitHub account where it is known, else by the email.
func avatarURL(email, hash, user string) string {
	if user != "" {
		return "https://avatars-githubusercontent.webp.se/u/" + user + "?s=64"
	}
	if m := noreply.FindStringSubmatch(email); m != nil {
		if m[1] != "" {
			return "https://avatars-githubusercontent.webp.se/u/" + m[1] + "?s=64"
		}
		return "https://github.com/" + m[2] + ".png?size=64"
	}
	return "https://www.gravatar.com/avatar/" + hash + "?s=64&d=404"
}
