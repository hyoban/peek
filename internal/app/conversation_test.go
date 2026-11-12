package app

import (
	"os"
	"path/filepath"
	"testing"
)

// Code in an answer naming a file of the repository links to it, at the
// line it gives; code naming no file, or one outside, stays text.
func TestFileLink(t *testing.T) {
	repo := t.TempDir()
	if err := os.MkdirAll(filepath.Join(repo, "internal", "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(repo, "internal", "app", "diffs.go")
	if err := os.WriteFile(file, []byte("package app\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outside := filepath.Join(t.TempDir(), "x.go")
	if err := os.WriteFile(outside, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	for code, want := range map[string]string{
		"internal/app/diffs.go":        "file://" + file,
		"internal/app/diffs.go:480":    "file://" + file + "#L480",
		"internal/app/diffs.go:480:12": "file://" + file + "#L480",
		file + ":3":                    "file://" + file + "#L3",
		"internal/app":                 "",
		"internal/app/missing.go":      "",
		"../x.go":                      "",
		outside:                        "",
	} {
		if got := fileLink(repo, code); got != want {
			t.Errorf("%s: %q, want %q", code, got, want)
		}
	}
	a := &app{repo: repo}
	if _, ok := a.codeLink("fmt.Println(x)"); ok {
		t.Error("a call linked")
	}
	if got, ok := a.codeLink("internal/app/diffs.go:2"); !ok || got != "file://"+file+"#L2" {
		t.Errorf("a path linked to %q, %v", got, ok)
	}
}
