package codex

import (
	"path"
	"sort"
	"strconv"
	"strings"
)

// Tiers of a file's diff, in the order rankDiffs embeds them, and the most
// of each file's diff it keeps.
const (
	tierCode    = iota
	tierSupport // tests, docs and config
	tierBulk    // lockfiles and generated files
)

var tierCaps = [...]int{tierCode: 8 * 1024, tierSupport: 8 * 1024, tierBulk: 2 * 1024}

var lockfiles = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
	"bun.lock": true, "bun.lockb": true, "go.sum": true, "cargo.lock": true, "gemfile.lock": true,
	"poetry.lock": true, "uv.lock": true, "pipfile.lock": true, "composer.lock": true,
	"podfile.lock": true, "package.resolved": true, "flake.lock": true, "mix.lock": true,
}

// rankDiffs splits a diff by file and keeps what fits the budget: code
// first, then tests, docs and config, then lockfiles and generated files,
// each cut to its tier's cap. complete is false when a code, test, doc or
// config diff was cut or left out; trimming bulk files does not count.
func rankDiffs(diff string, budget int) (ranked string, complete bool) {
	type file struct {
		text string
		tier int
	}
	var files []file
	for _, text := range splitDiff(diff) {
		files = append(files, file{text, diffTier(text)})
	}
	sort.SliceStable(files, func(i, j int) bool { return files[i].tier < files[j].tier })
	var b strings.Builder
	complete, left := true, false
	for _, f := range files {
		text := f.text
		if limit := tierCaps[f.tier]; len(text) > limit {
			text = cutDiff(text, limit)
			complete = complete && f.tier == tierBulk
		}
		if b.Len()+len(text) > budget {
			left = true
			complete = complete && f.tier == tierBulk
			continue
		}
		b.WriteString(text)
	}
	if left {
		b.WriteString("[Diffs of the other files in the summary are left out for space.]\n")
	}
	return b.String(), complete
}

// splitDiff cuts a unified Git diff at each file header.
func splitDiff(diff string) []string {
	var files []string
	for diff != "" {
		next := strings.Index(diff, "\ndiff --git ")
		if next < 0 {
			return append(files, diff)
		}
		files, diff = append(files, diff[:next+1]), diff[next+1:]
	}
	return files
}

func cutDiff(text string, limit int) string {
	keep := text[:limit]
	if i := strings.LastIndexByte(keep, '\n'); i >= 0 {
		keep = keep[:i+1]
	}
	return keep + "[This file's diff is cut here; " + strconv.Itoa(len(text)-len(keep)) + " more bytes.]\n"
}

// diffPath reads the new path from a file's diff header, which is enough to
// classify it even where a quoted or spaced path makes the header ambiguous.
func diffPath(text string) string {
	header, _, _ := strings.Cut(text, "\n")
	if i := strings.LastIndex(header, " b/"); i >= 0 {
		return strings.TrimSuffix(header[i+3:], `"`)
	}
	if i := strings.LastIndex(header, ` "b/`); i >= 0 {
		return strings.TrimSuffix(header[i+4:], `"`)
	}
	return header
}

func diffTier(text string) int {
	p := strings.ToLower(diffPath(text))
	name, ext := path.Base(p), path.Ext(p)
	switch {
	case lockfiles[name], ext == ".lock", ext == ".map", ext == ".snap", ext == ".svg",
		strings.HasSuffix(name, ".min.js"), strings.HasSuffix(name, ".min.css"),
		strings.Contains(text, "Code generated") && strings.Contains(text, "DO NOT EDIT"):
		return tierBulk
	case strings.Contains(name, "_test."), strings.Contains(name, ".test."), strings.Contains(name, ".spec."),
		strings.HasPrefix(name, "."), strings.HasPrefix(name, "readme"), strings.HasPrefix(name, "license"),
		strings.Contains("/"+p, "/test/"), strings.Contains("/"+p, "/tests/"),
		strings.Contains("/"+p, "/__tests__/"), strings.Contains("/"+p, "/testdata/"):
		return tierSupport
	}
	switch ext {
	case ".md", ".mdx", ".txt", ".rst", ".json", ".yaml", ".yml", ".toml", ".ini", ".xml", ".cfg", ".conf", ".env", "":
		return tierSupport
	}
	return tierCode
}
