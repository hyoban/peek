package app

import (
	"bytes"
	"image/png"
	"path"
	"testing"

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
	want := []string{openLabel, closedLabel, "", pull, "", "Open Folder…", "", "Quit Peek"}
	if len(labels) != len(want) {
		t.Fatalf("items %q, want %q", labels, want)
	}
	for i := range want {
		if labels[i] != want[i] {
			t.Errorf("item %d %q, want %q", i, labels[i], want[i])
		}
	}
	if !checked[openLabel] || checked[closedLabel] {
		t.Errorf("checked %v, want the repository with a window alone", checked)
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
