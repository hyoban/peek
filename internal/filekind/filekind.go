// Package filekind tells what a file holds by its path, for the change
// list's groups and the order Ship shows Codex the diffs in.
package filekind

import (
	"path"
	"slices"
	"strings"
)

// Kind is what a file holds, grouped code first, then tests, docs, and
// lockfiles and generated files, which say little for their size.
type Kind int

const (
	Code Kind = iota
	Test
	Docs
	Generated
)

// Names are the groups' names, by kind.
var Names = [...]string{Code: "Code", Test: "Tests", Docs: "Docs", Generated: "Generated"}

var (
	testDirs = []string{"test", "tests", "__tests__", "__mocks__", "testdata", "spec", "e2e"}
	docsDirs = []string{"doc", "docs"}
	docsExts = []string{".md", ".mdx", ".markdown", ".rst", ".adoc", ".org", ".txt"}
	docsStem = []string{"readme", "license", "licence", "changelog", "changes", "notice", "authors", "contributing", "copying"}
)

var lockfiles = map[string]bool{
	"package-lock.json": true, "npm-shrinkwrap.json": true, "pnpm-lock.yaml": true, "yarn.lock": true,
	"bun.lock": true, "bun.lockb": true, "go.sum": true, "cargo.lock": true, "gemfile.lock": true,
	"poetry.lock": true, "uv.lock": true, "pipfile.lock": true, "composer.lock": true,
	"podfile.lock": true, "package.resolved": true, "flake.lock": true, "mix.lock": true,
}

// Of tells what the file holds by its name and where it is.
func Of(p string) Kind {
	if IsGenerated(p) {
		return Generated
	}
	return Written(p)
}

// IsGenerated reports whether the file's name is a lockfile's or a
// generated file's by the common rules.
func IsGenerated(p string) bool {
	name := strings.ToLower(path.Base(p))
	ext := path.Ext(name)
	return lockfiles[name] || ext == ".lock" || ext == ".map" || ext == ".snap" ||
		strings.HasSuffix(name, ".min.js") || strings.HasSuffix(name, ".min.css") ||
		strings.HasSuffix(name, ".pb.go") || strings.HasSuffix(name, "_pb2.py") ||
		strings.HasSuffix(name, ".g.dart") || strings.HasSuffix(name, ".freezed.dart") ||
		strings.Contains(name, ".generated.")
}

// Written tells what a file written by hand holds, as Of does for a file
// not generated.
func Written(p string) Kind {
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
