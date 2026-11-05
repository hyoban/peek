package app

import (
	"os"
	"strings"

	"github.com/egoist/mygo/ui"
)

// shot is a view the app opens its project at, for the README's
// screenshots (scripts/screenshots.sh): the commit whose hash
// PEEK_SHOT_COMMIT begins, PEEK_SHOT_FILE shown in its changes, with
// PEEK_SHOT_MODE=files the files mode then, and last the terminal, running
// the command PEEK_SHOT_TERMINAL holds, as a user would choose them.
type shot struct {
	commit, file string
	files        bool
	terminal     bool   // the terminal shown
	command      string // to run in the terminal, until it is sent
	row          int    // the commit's, once found; -1 before
}

// shotTerminal is the terminal's height in a shot, for its command's
// output to show whole.
const shotTerminal = 220

// shotFromEnv is the view the environment asks for, nil for none.
func shotFromEnv() *shot {
	commit := os.Getenv("PEEK_SHOT_COMMIT")
	if commit == "" {
		return nil
	}
	return &shot{commit: commit, file: os.Getenv("PEEK_SHOT_FILE"), files: os.Getenv("PEEK_SHOT_MODE") == "files",
		terminal: os.Getenv("PEEK_SHOT_TERMINAL") != "", command: os.Getenv("PEEK_SHOT_TERMINAL"), row: -1}
}

// applyShot takes the app a step toward its shot in a pass, as what it
// needs loads: the commit, once the history shows it, then the file,
// once its changes are, then the files mode, then the terminal, and the
// command once its shell has started.
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
	case s.files != (a.mode == modeFiles):
		// The mode and the terminal are set either way, over the project's
		// saved view, which a shot before may have left.
		if s.files {
			a.setMode(modeFiles)
		} else {
			a.setMode(modeChanges)
		}
	case s.terminal != a.terminal.shown:
		a.toggleTerminal()
		a.terminal.height = max(a.terminal.height, shotTerminal)
		if a.projects != nil {
			a.projects.syncMenu()
		}
	case s.command != "":
		// The command goes once the shell shows its prompt, not to be
		// echoed ahead of it.
		switch {
		case a.terminal.err != "":
			s.command = ""
		case a.terminal.term != nil && strings.TrimSpace(a.terminal.term.Text()) != "":
			a.terminal.term.Send([]byte(s.command + "\r"))
			s.command = ""
		}
	default:
		a.shot = nil
	}
}
