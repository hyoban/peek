// Package filekind tells what a file holds by its path, for the change
// list's groups and the order Ship shows Codex the diffs in.
package filekind

import (
	"path"
	"slices"
	"strings"
)

// Kind is what a file holds, grouped code first, then tests, then docs.
type Kind int

const (
	Code Kind = iota
	Test
	Docs
)

// Names are the groups' names, by kind.
var Names = [...]string{Code: "Code", Test: "Tests", Docs: "Docs"}

var (
	testDirs = []string{"test", "tests", "__tests__", "__mocks__", "testdata", "spec", "e2e"}
	docsDirs = []string{"doc", "docs"}
	docsExts = []string{".md", ".mdx", ".markdown", ".rst", ".adoc", ".org", ".txt"}
	docsStem = []string{"readme", "license", "licence", "changelog", "changes", "notice", "authors", "contributing", "copying"}
)

// Of tells what the file holds by its name and where it is.
func Of(p string) Kind {
	dirs := strings.Split(path.Dir(p), "/")
	name := path.Base(p)
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	lower := strings.ToLower(stem)
	switch {
	case slices.ContainsFunc(dirs, func(d string) bool { return slices.Contains(testDirs, d) }),
		strings.HasSuffix(lower, "_test"), strings.HasSuffix(lower, "_spec"),
		strings.HasSuffix(lower, ".test"), strings.HasSuffix(lower, ".spec"),
		// Button.stories.tsx, but not .storybook's config
		strings.HasSuffix(lower, ".stories"), strings.HasSuffix(lower, ".story"),
		strings.HasPrefix(lower, "test_") && ext == ".py",
		// FooTest.java, FooTests.swift
		strings.HasSuffix(stem, "Test") && stem != "Test", strings.HasSuffix(stem, "Tests") && stem != "Tests":
		return Test
	case slices.ContainsFunc(dirs, func(d string) bool { return slices.Contains(docsDirs, d) }),
		slices.Contains(docsExts, strings.ToLower(ext)),
		slices.ContainsFunc(docsStem, func(s string) bool { return lower == s || strings.HasPrefix(lower, s+"-") }):
		return Docs
	}
	return Code
}
