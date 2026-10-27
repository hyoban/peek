package app

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"peek/internal/git"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/terminal"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

// The terminal's height bounds, and the room it leaves the text above it.
const minTerminal, minAboveTerminal = 100, 160

// terminalState is the project's terminal, under the text as Xcode's debug
// area: its shell, in the repository, started as the terminal first shows
// and ended with the project, or by itself, which hides the terminal.
type terminalState struct {
	term   *terminal.Terminal
	err    string  // why the shell did not start
	size   float32 // the font's, as the text scales
	focus  bool    // to take the keyboard focus on its next frame
	height float32 // the terminal's, which the window's resizing keeps
	shown  bool
	// at is where the file a Command+click opens is to show its caret.
	at *terminalPlace
}

// terminalPlace is a file the terminal printed, with its line and column,
// from 1, 0 for none.
type terminalPlace struct {
	path      string
	line, col int
}

// toggleTerminal shows or hides the terminal, which takes the keyboard
// focus as it shows, as Xcode's console does.
func (a *app) toggleTerminal() {
	t := &a.terminal
	t.shown = !t.shown
	t.focus = t.shown
}

// terminalTip is the tooltip of the terminal's button, which tells its
// key too, for the terminal to be found and called up from the keyboard.
func terminalTip(shown bool) string {
	return showOrHide(shown, "Terminal") + " (⌘J)"
}

// withTerminal builds pane, the text, over the terminal, which slides up
// from the bottom as the columns slide in from the sides (reveal). The
// line between them drags as AppKit's split views' do, the terminal
// keeping its height as the window resizes.
func (a *app) withTerminal(c *ui.Context, pane func()) {
	t := &a.terminal
	shown := a.reveal(c, "terminal", t.shown)
	ui.Column(c).Grow(1).FillWidth().MinHeight(0).Clip().Children(pane)
	if shown <= 0 {
		return
	}
	a.startTerminal(c)
	_, h := c.Size()
	t.height = max(min(t.height, h-minAboveTerminal), minTerminal)
	height := t.height
	ui.Column(c).Height(height * shown).Shrink(0).FillWidth().Clip().Children(func() {
		ui.Divider(c)
		ui.Column(c).Height(height - 1).Shrink(0).FillWidth().Children(func() {
			if t.term == nil {
				emptyNote(c, t.err)
				return
			}
			view := terminal.View(c.Key("terminal"), t.term).Fill()
			if t.focus {
				t.focus = false
				view.Focus()
			}
		})
		// The line's handle reaches into the terminal's padding, the
		// pointer showing it resizes, as AppKit's thin dividers do.
		handle := ui.Box(c).Absolute().Top(0).Left(0).Right(0).Height(5).Cursor(ui.CursorResizeNS)
		if _, dy, held := handle.Dragged(); held && dy != 0 {
			t.height = max(min(t.height-dy, h-minAboveTerminal), minTerminal)
		}
	})
}

// startTerminal starts the shell in the repository as the terminal first
// shows, its text at the size of code, and resizes the text as the user
// scales it.
func (a *app) startTerminal(c *ui.Context) {
	t := &a.terminal
	size := codeSize * c.Preferences().TextScale
	if t.term != nil {
		if size != t.size {
			t.size = size
			t.term.SetFont(terminal.Font{Size: size})
		}
		return
	}
	if t.err != "" {
		return
	}
	var term *terminal.Terminal
	term, err := terminal.New(terminal.Options{
		Dir:  a.repo,
		Font: terminal.Font{Size: size},
		OpenLink: func(link string) bool {
			return a.openTerminalLink(term, link)
		},
		// A shell that ends hides the terminal, the next to show starting
		// anew, as Xcode's console and VS Code's terminal close.
		OnExit: func(int) {
			if a.win == nil {
				return
			}
			a.win.Update(func() {
				if a.terminal.term == term {
					a.terminal.term, a.terminal.shown = nil, false
					term.Close()
					if a.projects != nil {
						a.projects.syncMenu()
					}
				}
			})
		},
	})
	if err != nil {
		t.err = "Couldn’t start the terminal: " + err.Error()
		return
	}
	t.term, t.size = term, size
}

// closeTerminal hangs up the shell, as the project closes.
func (a *app) closeTerminal() {
	if t := a.terminal.term; t != nil {
		a.terminal.term = nil
		t.Close()
	}
}

// placeSuffix is the line and column a path is printed with, as compilers
// and grep print them.
var placeSuffix = regexp.MustCompile(`:(\d+)(?::(\d+))?:?$`)

// parsePlace splits what the terminal printed into its path, line and
// column.
func parsePlace(link string) terminalPlace {
	m := placeSuffix.FindStringSubmatchIndex(link)
	if m == nil {
		return terminalPlace{path: link}
	}
	p := terminalPlace{path: link[:m[0]]}
	p.line, _ = strconv.Atoi(link[m[2]:m[3]])
	if m[4] >= 0 {
		p.col, _ = strconv.Atoi(link[m[4]:m[5]])
	}
	return p
}

// openTerminalLink opens a path the terminal printed and the user
// Command+clicked: a file of the repository in the files of the work tree,
// at its line, and anything else as the Finder opens it. A URL it leaves
// to the system.
func (a *app) openTerminalLink(term *terminal.Terminal, link string) bool {
	if strings.Contains(link, "://") {
		return false
	}
	place := parsePlace(link)
	p := place.path
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(p) {
		dir := term.Dir() // where the shell is, which it reports
		if dir == "" {
			dir = a.repo
		}
		p = filepath.Join(dir, p)
	}
	info, err := os.Stat(p)
	if err != nil {
		return false
	}
	rel, inside := repoPath(a.repo, p)
	worktree := len(a.commits) > 0 && a.commits[0].Hash == git.Worktree
	if info.IsDir() || !inside || !worktree {
		return mygo.Shell.OpenPath(p) == nil
	}
	place.path = rel
	a.openWorktreeFile(place)
	return true
}

// repoPath returns the path relative to the repository, with slashes, and
// whether the repository has it, through the links either is reached by.
func repoPath(repo, p string) (string, bool) {
	inside := func(repo, p string) (string, bool) {
		rel, err := filepath.Rel(repo, p)
		if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return "", false
		}
		return filepath.ToSlash(rel), true
	}
	if rel, ok := inside(repo, p); ok {
		return rel, true
	}
	realRepo, err1 := filepath.EvalSymlinks(repo)
	realP, err2 := filepath.EvalSymlinks(p)
	if err1 != nil || err2 != nil {
		return "", false
	}
	return inside(realRepo, realP)
}

// openWorktreeFile shows the file in the files of the work tree, its caret
// at the place, choosing the work tree in the history and the files mode
// where they are not shown. The text loads as its files do (sync), and
// takes the caret then (placeCaret).
func (a *app) openWorktreeFile(place terminalPlace) {
	a.cancelNavigation()
	a.mode, a.modeChoice, a.navigator = modeFiles, modeFiles, navFiles
	a.terminal.at = &place
	if a.commitRow == 0 && a.loadedRow == 0 && a.filesFor == 0 {
		// A file the files do not list, as one git ignores, by its path.
		if !a.openInFiles(git.Worktree, place.path) {
			a.openInFiles(git.Worktree, a.sourcePath(place.path))
		}
		return
	}
	a.commitRow = 0 // which sync loads, with its files then the file
	a.openFile = place.path
}

// placeCaret puts the caret where the terminal's link pointed, once the
// text of its file shows, and gives the text the keyboard focus.
func (a *app) placeCaret(p string, code *editor.State) {
	at := a.terminal.at
	if at == nil || code == nil || (at.path != p && a.sourcePath(at.path) != p) {
		return
	}
	a.terminal.at = nil
	if at.line > 0 {
		code.GoTo(editor.Pos{Line: at.line - 1, Col: max(at.col-1, 0)})
	}
	a.navigation.focus = true
}
