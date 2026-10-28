package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"peek/internal/definition"
)

// viewsFile keeps how each repository showed as its window closed or the
// app quit, which it shows again as it opens anew: the mode, the file
// shown and where in it, the navigator, the folders open and the
// previews; and the columns of the window, which the projects share.
const viewsFile = "views.json"

type views struct {
	Layout *layout              `json:"layout,omitempty"`
	Repos  map[string]*repoView `json:"repos,omitempty"`
}

// layout is the window's columns and terminal.
type layout struct {
	History        bool    `json:"history"`
	Tree           bool    `json:"tree"`
	HistoryWidth   float32 `json:"historyWidth"`
	TreeWidth      float32 `json:"treeWidth"`
	Terminal       bool    `json:"terminal"`
	TerminalHeight float32 `json:"terminalHeight"`
}

// repoView is how a repository showed: File is the file at the top of the
// diffs, or open in the files mode, where the caret and the scroll were.
type repoView struct {
	Files         bool                `json:"files,omitempty"`
	File          string              `json:"file,omitempty"`
	Anchor        definition.Position `json:"anchor"`
	Head          definition.Position `json:"head"`
	Scroll        float64             `json:"scroll,omitempty"`
	Conversations bool                `json:"conversations,omitempty"`
	Conversation  string              `json:"conversation,omitempty"`
	Open          []string            `json:"open,omitempty"`
	Previewed     []string            `json:"previewed,omitempty"`
}

// readViews reads the views kept, none for no file.
func readViews(dir string) views {
	var v views
	if data, err := os.ReadFile(filepath.Join(dir, viewsFile)); err == nil {
		json.Unmarshal(data, &v)
	}
	if v.Repos == nil {
		v.Repos = map[string]*repoView{}
	}
	return v
}

// keepViews keeps how the projects open show, and the window's columns,
// over what was kept of them; those closed before keep theirs. It writes
// at once, as the app quits.
func (p *projects) keepViews() {
	if p.views.Repos == nil {
		p.views.Repos = map[string]*repoView{}
	}
	for repo, a := range p.apps {
		p.views.Repos[repo] = a.keptView()
	}
	if a := p.cur; a != nil {
		p.views.Layout = &layout{
			History:      a.showHistory || a.collapsedHistory,
			Tree:         a.showTree || a.collapsedTree,
			HistoryWidth: a.historyWidth, TreeWidth: a.treeWidth,
			Terminal: a.terminal.shown, TerminalHeight: a.terminal.height,
		}
	}
	if p.dir == "" {
		return
	}
	data, err := json.MarshalIndent(p.views, "", "\t")
	if err != nil {
		return
	}
	os.MkdirAll(p.dir, 0o755)
	os.WriteFile(filepath.Join(p.dir, viewsFile), data, 0o644)
}

// keptView is how the project shows, to keep.
func (a *app) keptView() *repoView {
	if r := a.restoring; r != nil {
		return r // not shown yet, as it was
	}
	v := &repoView{Files: a.mode == modeFiles, Conversations: a.navigator == navConversations, File: a.file}
	if v.Files {
		v.File = a.openFile
		v.Open = slices.Sorted(a.allTree.state.Open.All())
		if a.code != nil && a.codePath == a.openFile {
			s, text := a.code.Selection(), a.code.Buffer.String()
			v.Anchor, v.Head = definition.PositionAt(text, s.Anchor), definition.PositionAt(text, s.Head)
			v.Scroll = a.code.ScrollY()
		}
	}
	if a.convRow >= 0 && a.convRow < len(a.conversations) {
		v.Conversation = a.conversations[a.convRow].ID
	}
	for p, on := range a.previewed {
		if on {
			v.Previewed = append(v.Previewed, p)
		}
	}
	slices.Sort(v.Previewed)
	return v
}

// restoreView shows the project as it was kept, the file once its
// commit's changes or files load (restoreFile, restoreCaret).
func (a *app) restoreView(v *repoView) {
	if v == nil {
		return
	}
	if v.Files {
		a.mode, a.modeChoice = modeFiles, modeFiles
		a.openFile = v.File // which loadFiles keeps open
		for _, dir := range v.Open {
			a.allTree.state.Open.Add(dir) // which the tree keeps where it has them
		}
	}
	if i := slices.IndexFunc(a.conversations, func(c *conversation) bool { return c.ID == v.Conversation }); i >= 0 {
		a.convRow = i
	}
	if v.Conversations {
		a.showNavigator(navConversations)
	}
	for _, p := range v.Previewed {
		if a.previewed == nil {
			a.previewed = map[string]bool{}
		}
		a.previewed[p] = true
	}
	a.restoring = v
}

// restoreFile shows the file kept at the top of the diffs, where the
// commit's changes, just loaded, have it.
func (a *app) restoreFile() {
	r := a.restoring
	if r == nil || r.Files {
		return
	}
	a.restoring = nil
	if _, ok := a.changes[r.File]; ok && r.File != a.file {
		a.changeTree.show(r.File) // which the diffs scroll to
	}
}

// restoreCaret puts the caret and the scroll where they were in the file
// kept, as its text, just loaded, shows.
func (a *app) restoreCaret(p string) {
	r := a.restoring
	if r == nil || !r.Files {
		return
	}
	a.restoring = nil
	if p != r.File || a.code == nil {
		return
	}
	text := a.code.Buffer.String()
	a.code.Select(definition.OffsetAt(text, r.Anchor), definition.OffsetAt(text, r.Head))
	a.code.SetScrollY(r.Scroll)
}
