package app

import (
	"os"
	"strings"

	"github.com/egoist/mygo/ui"
)

// shot is a view the app opens its project at, for the README's
// screenshots (scripts/screenshots.sh): the commit whose hash
// PEEK_SHOT_COMMIT begins, PEEK_SHOT_FILE shown in its changes, and with
// PEEK_SHOT_MODE=files the files mode then, as a user would choose them.
type shot struct {
	commit, file string
	files        bool
	row          int // the commit's, once found; -1 before
}

// shotFromEnv is the view the environment asks for, nil for none.
func shotFromEnv() *shot {
	commit := os.Getenv("PEEK_SHOT_COMMIT")
	if commit == "" {
		return nil
	}
	return &shot{commit: commit, file: os.Getenv("PEEK_SHOT_FILE"), files: os.Getenv("PEEK_SHOT_MODE") == "files", row: -1}
}

// applyShot takes the app a step toward its shot in a pass, as what it
// needs loads: the commit, once the history shows it, then the file,
// once its changes are, then the files mode.
func (a *app) applyShot(c *ui.Context) {
	s := a.shot
	if s == nil {
		return
	}
	defer c.Services().Invalidate() // until it is done
	switch {
	case s.row < 0:
		for i, cm := range a.commits {
			if strings.HasPrefix(cm.Hash, s.commit) {
				s.row, a.commitRow = i, i
				return
			}
		}
	case a.loadedRow != s.row:
	case s.file != "" && a.file != s.file:
		if _, ok := a.changes[s.file]; ok {
			a.changeTree.show(s.file)
		}
	default:
		if s.files {
			a.setMode(modeFiles)
		}
		a.shot = nil
	}
}
