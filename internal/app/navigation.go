package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"peek/internal/definition"
	"peek/internal/git"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type definitionClient interface {
	Definition(context.Context, string, string, int) ([]definition.Location, error)
	Close()
}

type filePlace struct {
	path       string // repository-relative, or absolute for files outside the tree
	start, end definition.Position
	scroll     float64
	restore    bool
}

type navigationSource struct {
	code    *editor.State
	version uint64
	gen     int
	path    string
}

type navigationState struct {
	client        definitionClient
	cancel        context.CancelFunc
	seq           int
	source        *navigationSource
	busy          bool
	note          string
	targets       []definition.Location
	back, forward []filePlace
	editor        ui.Handle
	focus         bool
}

func (a *app) worktreeFiles() bool {
	cm, ok := a.current()
	return !a.closed && ok && cm.Hash == git.Worktree && a.commitRow == a.loadedRow && a.mode == modeFiles && a.shownConversation() == nil
}

func (a *app) readableSource() bool {
	return a.worktreeFiles() && a.code != nil && a.codePath == a.openFile && a.codeHash == git.Worktree && a.codeNote == ""
}

func (a *app) canDefine() bool {
	return a.readableSource() && !a.previewing() && definition.Language(a.openFile) != ""
}

func (a *app) configureNavigation() {
	if a.code == nil {
		return
	}
	a.code.OnDefinition = nil
	a.code.ContextMenu = nil
	if a.canDefine() {
		a.code.OnDefinition = a.goDefinition
		a.code.ContextMenu = func(m *ui.Menu) {
			if m.Item("Go to Definition").Chosen() {
				a.goDefinition(a.code.Cursor())
			}
		}
	}
}

func (a *app) navigationSource() navigationSource {
	return navigationSource{code: a.code, version: a.code.Buffer.Version(), gen: a.textGen, path: a.openFile}
}

func (a *app) sourceIs(s navigationSource) bool {
	return a.readableSource() && a.code == s.code && a.openFile == s.path && a.textGen == s.gen && a.code.Buffer.Version() == s.version
}

func (a *app) cancelNavigation() {
	n := &a.navigation
	if n.cancel != nil {
		n.cancel()
		n.cancel = nil
	}
	n.seq++
	n.busy, n.source, n.targets, n.note = false, nil, nil, ""
}

// Servers have no unsaved documents between queries. A repository refresh
// drops their caches too, including changes in unopened files and config.
func (a *app) invalidateDefinitions() {
	a.cancelNavigation()
	if a.navigation.client != nil {
		a.navigation.client.Close()
		a.navigation.client = nil
	}
}

func (a *app) checkNavigation() {
	n := &a.navigation
	if n.source != nil && !a.sourceIs(*n.source) {
		a.cancelNavigation()
	}
	if cm, ok := a.current(); ok && (cm.Hash != git.Worktree || a.commitRow != a.loadedRow) {
		n.back, n.forward = nil, nil
	}
}

func (a *app) goDefinition(offset int) {
	if !a.canDefine() {
		return
	}
	a.cancelNavigation()
	n := &a.navigation
	if n.client == nil {
		n.client = definition.New(a.repo)
	}
	client, source, seq := n.client, a.navigationSource(), n.seq
	text := a.code.Buffer.String()
	path := a.sourcePath(a.openFile)
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	n.cancel, n.source, n.busy, n.note = cancel, &source, true, "Finding definition…"
	a.background(func() func() {
		locations, err := client.Definition(ctx, path, text, offset)
		cancel()
		return func() {
			if seq != a.navigation.seq || !a.sourceIs(source) {
				return
			}
			n := &a.navigation
			n.busy, n.cancel = false, nil
			switch {
			case err != nil:
				n.note = "Could not find definition: " + err.Error()
			case len(locations) == 0:
				n.note = "No definition found"
			case len(locations) == 1:
				a.openDefinition(locations[0])
			default:
				n.note, n.targets = "Choose a definition", locations
			}
		}
	})
}

func (a *app) sourcePath(p string) string {
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(a.repo, filepath.FromSlash(p))
}

func (a *app) filePlace() filePlace {
	s := a.code.Selection()
	text := a.code.Buffer.String()
	return filePlace{path: a.openFile, start: definition.PositionAt(text, s.Anchor), end: definition.PositionAt(text, s.Head), scroll: a.code.ScrollY(), restore: true}
}

func (a *app) openDefinition(loc definition.Location) {
	p := loc.Path
	if rel, err := filepath.Rel(a.repo, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		// Ignored dependencies need not be in the sidebar to be opened.
		if a.allTree.has(filepath.ToSlash(rel)) {
			p = filepath.ToSlash(rel)
		}
	}
	a.visit(filePlace{path: p, start: loc.Range.Start, end: loc.Range.End}, 0)
}

func (a *app) goBack(step int) {
	if !a.readableSource() || a.navigation.busy {
		return
	}
	stack := a.navigation.back
	if step > 0 {
		stack = a.navigation.forward
	}
	if len(stack) > 0 {
		a.visit(stack[len(stack)-1], step)
	}
}

// visit only changes the file and history once the destination is readable.
// A failed lookup leaves both the source and the Back command intact.
func (a *app) visit(target filePlace, step int) {
	if !a.readableSource() {
		return
	}
	from, source := a.filePlace(), a.navigationSource()
	a.cancelNavigation()
	n := &a.navigation
	seq, repo := n.seq, a.repo
	n.busy, n.source, n.note = true, &source, "Opening definition…"
	a.background(func() func() {
		text, err := sourceText(repo, git.Worktree, target.path, maxTextSize)
		_, note := fileView(target.path, text, err)
		var buf *editor.Buffer
		if note == "" {
			buf = codeBuffer(target.path, text)
			// Editor offsets refer to its normalized LF text, even when the
			// file on disk uses CRLF. LSP line/column positions work in both.
			text = buf.String()
		}
		return func() {
			if seq != a.navigation.seq || !a.sourceIs(source) {
				return
			}
			n := &a.navigation
			n.busy, n.source, n.note = false, nil, ""
			if buf == nil {
				n.note = "Could not open definition: " + note
				return
			}
			switch {
			case step < 0:
				n.back = n.back[:len(n.back)-1]
				n.forward = appendPlace(n.forward, from)
			case step > 0:
				n.forward = n.forward[:len(n.forward)-1]
				n.back = appendPlace(n.back, from)
			default:
				n.back = appendPlace(n.back, from)
				n.forward = nil
			}
			a.textGen++
			a.openFile, a.codePath, a.codeHash = target.path, target.path, git.Worktree
			a.code = codeOf(buf)
			a.code.Select(definition.OffsetAt(text, target.start), definition.OffsetAt(text, target.end))
			if target.restore {
				a.code.SetScrollY(target.scroll)
			}
			a.picture, a.preview, a.codeNote = nil, nil, ""
			a.showFileInTree(target.path)
			n.focus = true
		}
	})
}

func appendPlace(stack []filePlace, p filePlace) []filePlace {
	if len(stack) > 0 && stack[len(stack)-1] == p {
		return stack
	}
	if len(stack) >= 100 {
		stack = stack[1:]
	}
	return append(stack, p)
}

func (a *app) showFileInTree(p string) {
	if a.allTree.has(p) {
		// A filtered-out destination must still be selected and revealed.
		if a.allTree.keep != nil && !a.allTree.keep[p] {
			a.allTree.filter = ""
		}
		a.allTree.show(p)
	} else {
		a.allTree.row, a.allTree.reveal = -1, ""
	}
}

// sourceText is bounded for external dependencies too. They are only read
// in the working tree; git continues to supply every historical file.
func sourceText(repo, hash, p string, limit int) (string, error) {
	if !filepath.IsAbs(p) {
		return git.FileTextAtMost(repo, hash, p, limit)
	}
	if hash != git.Worktree {
		return "", errors.New("external files are only available in the working tree")
	}
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", errors.New("not a regular file")
	}
	if limit <= 0 {
		limit = maxTextSize
	}
	b, err := io.ReadAll(io.LimitReader(f, int64(limit)+1))
	if len(b) > limit {
		return "", git.ErrTooLarge
	}
	return string(b), err
}

func (a *app) navigationTools(c *ui.Context) {
	if !a.worktreeFiles() {
		return
	}
	for _, b := range []struct {
		name        string
		svg         *ui.SVG
		step, count int
	}{
		{"Go Back", backIcon, -1, len(a.navigation.back)},
		{"Go Forward", forwardIcon, 1, len(a.navigation.forward)},
	} {
		ui.Button(c.Key(b.name), "").Label(b.name).Tooltip(b.name).Width(24).Height(22).
			Disabled(b.count == 0 || !a.readableSource() || a.navigation.busy).
			Children(func() { ui.Icon(c, b.svg).Size(14, 14) }).OnClick(func() { a.goBack(b.step) })
	}
}

func (a *app) definitionChoices(c *ui.Context) {
	n := &a.navigation
	if !a.worktreeFiles() || n.note == "" {
		return
	}
	ui.Row(c.Key("definition-status")).FillWidth().Shrink(0).Padding(6, 10).Gap(6).AlignItems(ui.Center).Children(func() {
		if n.busy {
			ui.Spinner(c).Size(12, 12)
		}
		ui.Text(c, n.note).FontSize(smallSize).TextColor(c.Theme().TextMuted).SingleLine().Ellipsis("…").Tooltip(n.note).Grow(1).MinWidth(0)
		ui.Button(c.Key("dismiss"), "Dismiss").OnClick(a.cancelNavigation)
	})
	if len(n.targets) == 0 {
		return
	}
	ui.Scroll(c.Key("definitions")).FillWidth().MaxHeight(180).Children(func() {
		for i, loc := range n.targets {
			name := loc.Path
			if rel, err := filepath.Rel(a.repo, name); err == nil && !strings.HasPrefix(rel, "..") {
				name = rel
			}
			label := fmt.Sprintf("%s:%d:%d", name, loc.Range.Start.Line+1, loc.Range.Start.Character+1)
			ui.Button(c.Key(fmt.Sprint(i)), label).FillWidth().OnClick(func() { a.openDefinition(loc) })
		}
	})
}
