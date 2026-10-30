package git

import (
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"testing"
)

// Discarding brings back what HEAD has of a changed, a deleted and a
// renamed file, staged or not, and removes the new ones, with the folders
// they leave empty, leaving the other changes as they are.
func TestDiscard(t *testing.T) {
	repo := testutil.Repo(t)
	write := func(name, text string) {
		p := filepath.Join(repo, name)
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("staged/new.go", "package staged\n")
	write("keep.txt", "keep\n")
	testutil.Git(t, repo, "add", "staged/new.go")
	write("staged/new.go", "package staged // edited since\n")
	changes, err := LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	var discard []Change
	for _, ch := range changes {
		if ch.Path != "keep.txt" {
			discard = append(discard, ch)
		}
	}
	if err := Discard(repo, discard); err != nil {
		t.Fatal(err)
	}
	after, err := LoadWorktreeChanges(repo)
	if err != nil {
		t.Fatal(err)
	}
	if len(after) != 1 || after[0].Path != "keep.txt" {
		t.Errorf("changes after discarding %+v, want keep.txt's alone", after)
	}
	if b, _ := os.ReadFile(filepath.Join(repo, "main.go")); string(b) != "package main\n" {
		t.Errorf("main.go %q, want HEAD's", b)
	}
	for _, gone := range []string{"docs", "staged"} {
		if _, err := os.Stat(filepath.Join(repo, gone)); !os.IsNotExist(err) {
			t.Errorf("%s still there", gone)
		}
	}

	// A staged rename: the old path back, the new gone.
	testutil.Git(t, repo, "mv", "main.go", "app.go")
	changes, _ = LoadWorktreeChanges(repo)
	var renamed []Change
	for _, ch := range changes {
		if ch.Status == "R" {
			renamed = append(renamed, ch)
		}
	}
	if len(renamed) != 1 {
		t.Fatalf("changes %+v, want a rename", changes)
	}
	if err := Discard(repo, renamed); err != nil {
		t.Fatal(err)
	}
	if after, _ := LoadWorktreeChanges(repo); len(after) != 1 || after[0].Path != "keep.txt" {
		t.Errorf("changes after discarding the rename %+v, want keep.txt's alone", after)
	}
}
