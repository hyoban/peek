package app

import (
	"bytes"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
)

// A swipe down a touchpad slants a little: the diffs scroll down, and
// their text must not slide sideways with it.
func TestSlantedSwipeKeepsDiffsInPlace(t *testing.T) {
	repo := testutil.Repo(t)
	long := "package main\n\n// " + strings.Repeat("a long comment that runs past the edge ", 12) + "\n"
	os.WriteFile(filepath.Join(repo, "main.go"), []byte(long+strings.Repeat("var x = 1\n", 80)), 0o644)
	_, tt := testApp(t, repo, func(a *app) { a.showHistory, a.showTree = false, false })
	tt.Wheel(600, 400, 0, 0) // the pointer over the diff
	settle(tt)
	before := bytes.Clone(tt.Image().Pix)
	for range 6 {
		tt.Wheel(600, 400, 30, 0) // sideways alone: nothing should move
	}
	settle(tt)
	if after := tt.Image().Pix; !bytes.Equal(before, after) {
		t.Error("the diffs moved sideways under a sideways swipe")
	}
}
