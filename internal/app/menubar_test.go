package app

import (
	"bytes"
	"image/png"
	"path"
	"path/filepath"
	"testing"
	"time"

	"peek/internal/testutil"
)

func TestMenuBarItems(t *testing.T) {
	open, closed := testutil.Repo(t), testutil.Repo(t)
	p := &projects{apps: map[string]*app{}, inline: true}
	p.open(closed)
	p.open(open)
	delete(p.apps, closed) // its window closed
	p.bar = &menuBar{status: map[string]repoStatus{}, pulling: map[string]bool{}}
	p.bar.status[closed] = repoStatus{branch: "main", changed: 1, behind: 2, loaded: true}

	if s := readRepoStatus(open); s.branch != "main" || s.changed != 3 || !s.loaded {
		t.Errorf("status read %+v, want main with 3 files changed", s)
	}
	var labels []string
	checked := map[string]bool{}
	for _, it := range p.menuBarItems() {
		labels = append(labels, it.Label)
		checked[it.Label] = it.Checked
	}
	openLabel := p.label(open) + " — main, 3 files changed"
	closedLabel := p.label(closed) + " — main, 1 file changed"
	pull := "Pull 2 New Commits into " + path.Base(closed)
	want := []string{openLabel, closedLabel, "", pull, "", "Open Folder…", "", "Show in Menu Bar", "Hide When Inactive", "Settings…", "", "Quit Peek"}
	if len(labels) != len(want) {
		t.Fatalf("items %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("item %d %q, want %q", i, labels[i], want[i])
		}
	}
	if !checked[openLabel] || checked[closedLabel] {
		t.Errorf("checked %v, want the repository shown alone", checked)
	}
}

func TestTrayIcon(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(trayIcon))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != 36 || b.Dy() != 36 {
		t.Errorf("icon %v, want 36×36 pixels", b)
	}
}

func TestHideWhenInactiveKept(t *testing.T) {
	dir := t.TempDir()
	p := &projects{apps: map[string]*app{}, inline: true, dir: dir}
	kept := func() settings {
		for i := 0; i < 100; i++ {
			if exists(filepath.Join(dir, settingsFile)) && readSettings(dir).HideInactive == p.prefs().HideInactive {
				break
			}
			time.Sleep(10 * time.Millisecond) // written off the main thread
		}
		return readSettings(dir)
	}
	p.setHideInactive(true)
	if !p.prefs().HideInactive || !kept().HideInactive {
		t.Fatal("the choice was not kept")
	}
	p.setHideInactive(false)
	if p.prefs().HideInactive || kept().HideInactive {
		t.Error("the choice was not taken back")
	}
}
