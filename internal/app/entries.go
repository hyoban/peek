package app

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// entriesFile keeps the entry of the history each repository showed last:
// the uncommitted changes, the branch's or a commit, by its hash, which
// the repository shows again as it opens anew.
const entriesFile = "entries.json"

// readEntries reads the entries kept, an empty map for none.
func readEntries(dir string) map[string]string {
	entries := map[string]string{}
	if data, err := os.ReadFile(filepath.Join(dir, entriesFile)); err == nil {
		json.Unmarshal(data, &entries)
	}
	return entries
}

// keepEntry keeps the entry the repository shows, where it changed. The
// file is small and written at once, so that writes never land out of
// order.
func (p *projects) keepEntry(repo, hash string) {
	if was, ok := p.entries[repo]; ok && was == hash {
		return
	}
	if p.entries == nil {
		p.entries = map[string]string{}
	}
	p.entries[repo] = hash
	if p.dir == "" {
		return
	}
	data, err := json.Marshal(p.entries)
	if err != nil {
		return
	}
	os.MkdirAll(p.dir, 0o755)
	os.WriteFile(filepath.Join(p.dir, entriesFile), data, 0o644)
}

// keptRow is the row of the entry the repository showed last, where the
// history still has it: a range stands for the branch's changes, however
// the branch moved since.
func (a *app) keptRow() (int, bool) {
	if a.projects == nil {
		return 0, false
	}
	hash, ok := a.projects.entries[a.repo]
	if !ok {
		return 0, false
	}
	row := a.rowOf(hash)
	return row, row >= 0
}
