package fileicons

import (
	"regexp"
	"testing"
)

func TestRulesCompile(t *testing.T) {
	for _, r := range rules {
		if _, err := regexp.Compile(r.pattern); err != nil {
			t.Errorf("%s: %v", r.icon, err)
		}
		if _, ok := glyphs[r.icon]; !ok {
			t.Errorf("%s has no glyph", r.icon)
		}
	}
}

func TestFor(t *testing.T) {
	for p, want := range map[string]string{
		"main.go":                  "go",
		"go.mod":                   "config-go",
		"package.json":             "npm",
		"Dockerfile":               "docker",
		".gitignore":               "git",
		"src/App.tsx":              "tsx",
		"src/index.ts":             "ts",
		"a.test.ts":                "test-ts",
		"tests/foo_test.py":        "test-python", // by the path
		"roles/web/tasks/main.yml": "ansible",     // a rule rewritten for Go
		"x.json.dist":              "json",        // past a template's suffix
		"CHANGELOG.md":             "markdown",
		"x.unknownext":             "text",
	} {
		if got := For(p, false).Name; got != want {
			t.Errorf("For(%q) = %s, want %s", p, got, want)
		}
	}
	if For("main.go", true).Color == For("x.unknownext", true).Color {
		t.Error("a Go file has no colour")
	}
}
