package app

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"peek/internal/git"
	"peek/internal/review"
	"peek/internal/testutil"

	"github.com/egoist/mygo/ui/editor"
)

func TestDiffLayout(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
	if a.diffs[0].state == nil || a.diffs[0].state.Layout != editor.DiffUnified {
		t.Fatal("the diffs do not start unified")
	}
	if err := tt.Click("Show Side by Side"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if !a.split || a.diffs[0].state.Layout != editor.DiffSplit {
		t.Errorf("split %v, layout %v after Show Side by Side", a.split, a.diffs[0].state.Layout)
	}
	if err := tt.Click("Show Unified"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if a.split || a.diffs[0].state.Layout != editor.DiffUnified {
		t.Error("Show Unified left the diffs side by side")
	}
}

// changesRepo is Repo's repository with files of code that change two
// places each, far apart.
func changesRepo(t *testing.T, files int) string {
	repo := testutil.Repo(t)
	lines := make([]string, 60)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %d", i)
	}
	for i := range files {
		name := filepath.Join(repo, fmt.Sprintf("z%02d.go", i))
		os.WriteFile(name, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
		testutil.Git(t, repo, "add", name)
	}
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Add files")
	changed := append([]string{}, lines...)
	changed[5], changed[50] = "changed 5", "changed 50"
	for i := range files {
		os.WriteFile(filepath.Join(repo, fmt.Sprintf("z%02d.go", i)), []byte(strings.Join(changed, "\n")+"\n"), 0o644)
	}
	return repo
}

func TestChangeNavigation(t *testing.T) {
	a, tt := testApp(t, changesRepo(t, 20), nil)
	settle(tt)
	type stop struct {
		file string
		hunk int
	}
	var stops []stop
	for range 6 {
		a.goChange(1)
		settle(tt)
		stops = append(stops, stop{a.change.file, a.change.hunk})
		if a.change.reveal {
			t.Fatalf("change %+v not revealed", a.change)
		}
	}
	want := []stop{{"main.go", 0}, {"z00.go", 0}, {"z00.go", 1}, {"z01.go", 0}, {"z01.go", 1}, {"z02.go", 0}}
	if fmt.Sprint(stops) != fmt.Sprint(want) {
		t.Errorf("Next Change went to %v, want %v", stops, want)
	}
	a.goChange(-1)
	settle(tt)
	if a.change.file != "z01.go" || a.change.hunk != 1 {
		t.Errorf("Previous Change went to %+v, want z01.go's second", a.change)
	}

	// Far down, to a file whose diff has not loaded: it loads and shows.
	for range 30 {
		a.goChange(1)
		settle(tt)
	}
	if a.change.file != "z16.go" || a.change.reveal {
		t.Fatalf("after 30 more changes at %+v", a.change)
	}
	i := a.diffIndex("z16.go")
	if first, last := a.diffList.Visible(); i < 0 || headerRow(i)+1 < first || headerRow(i)+1 > last {
		t.Errorf("rows %d–%d in view, want z16.go's diff, row %d", first, last, headerRow(i)+1)
	}
	if a.file != "z16.go" || a.changeTree.chosen() != "z16.go" {
		t.Errorf("file %q, tree %q, want z16.go", a.file, a.changeTree.chosen())
	}
}

func TestLineReferences(t *testing.T) {
	ch := git.Change{Status: "R", Old: "old.go", Path: "new.go"}
	if got := lineReference(ch, editor.DiffLineRange{Side: editor.DiffNew, Start: 9, End: 19}); got != "new.go:10-20" {
		t.Errorf("reference %q", got)
	}
	if got := lineReference(ch, editor.DiffLineRange{Side: editor.DiffOld, Start: 2, End: 2}); got != "old.go:3 (before)" {
		t.Errorf("reference to an old line %q", got)
	}
	// GitHub anchors a commit's file by the SHA-256 of its path.
	link := lineLink("https://github.com/o/r", "abc", ch, editor.DiffLineRange{Side: editor.DiffOld, Start: 0, End: 1})
	want := fmt.Sprintf("https://github.com/o/r/commit/abc#diff-%xL1-L2", sha256.Sum256([]byte("new.go")))
	if link != want {
		t.Errorf("link %q, want %q", link, want)
	}
}

func TestComments(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
	d := a.diffOf["main.go"]
	a.addComment(d, editor.DiffLineRange{Side: editor.DiffNew, Start: 2, End: 2})
	tt.Frame()
	if !tt.HasText("Comment") {
		t.Fatalf("no comment field, texts %q", tt.Texts())
	}
	if _, comments := a.shownComments(); len(comments) != 0 {
		t.Error("a drafted comment counts")
	}
	cm := a.commentsOf(d)[0]
	cm.draft = "Name this"
	if err := tt.Click("Comment"); err != nil {
		t.Fatal(err)
	}
	tt.Frame()
	if cm.editing || cm.text != "Name this" {
		t.Fatalf("comment %+v after saving", cm)
	}
	if !tt.HasText("Name this") || !tt.HasText("1 comment") {
		t.Errorf("texts %q", tt.Texts())
	}
	want := "main.go:3\n> func main() {}\nName this"
	if got := a.commentsPrompt(); got != want {
		t.Errorf("prompt %q, want %q", got, want)
	}
	a.deleteComment(d, cm)
	tt.Frame()
	if tt.HasText("1 comment") {
		t.Error("the comment stayed")
	}
}

func TestFindingsUnderLines(t *testing.T) {
	a, tt := testApp(t, testutil.Repo(t), nil)
	a.review.result = &review.Result{Summary: "One issue", Findings: []review.Finding{
		{Priority: 1, Title: "Empty main", Body: "It does nothing.", Path: "main.go", Line: 3},
		{Priority: 3, Title: "Elsewhere", Path: "other.go", Line: 1},
	}}
	tt.Frame()
	if !tt.HasText("[P1] Empty main") || !tt.HasText("It does nothing.") {
		t.Errorf("the finding does not show under its line, texts %q", tt.Texts())
	}
	if tt.HasText("[P3] Elsewhere") {
		t.Error("a finding about a file not shown shows")
	}
	anns := a.diffOf["main.go"].state.Annotations
	if len(anns) != 1 || anns[0].Line != 2 || anns[0].Height <= 0 {
		t.Errorf("annotations %+v", anns)
	}
	// Out of date, it shows no more.
	a.review.stale = true
	tt.Frame()
	if tt.HasText("[P1] Empty main") {
		t.Error("a stale review's finding shows")
	}
}

func TestLargeFileShowsPatch(t *testing.T) {
	repo := testutil.Repo(t)
	big := strings.Repeat(strings.Repeat("x", 99)+"\n", maxTextSize/100+10)
	name := filepath.Join(repo, "big.go")
	os.WriteFile(name, []byte(big), 0o644)
	testutil.Git(t, repo, "add", name)
	testutil.Git(t, repo, "-c", "user.name=Ada", "-c", "user.email=ada@example.com", "commit", "-q", "-m", "Big")
	os.WriteFile(name, []byte("changed\n"+big[100:]), 0o644)
	a, tt := testApp(t, repo, nil)
	a.changeTree.show("big.go")
	settle(tt)
	d := a.diffOf["big.go"]
	if d == nil || d.state == nil || !d.patch || d.note != "" {
		t.Fatalf("big.go: patch %v, note %q", d != nil && d.patch, d.note)
	}
	if added, deleted := d.state.Stats(); added != 1 || deleted != 1 {
		t.Errorf("patch counts +%d −%d", added, deleted)
	}
}
