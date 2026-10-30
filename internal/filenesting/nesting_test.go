package filenesting

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"
)

func TestParents(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files []string
		want  map[string]string
	}{
		{"go", []string{"go.mod", "go.sum", "main.go", "main_test.go", "other_test.go"}, map[string]string{"go.sum": "go.mod", "main_test.go": "main.go"}},
		{"package", []string{"package.json", "pnpm-lock.yaml", ".npmrc", "README.md"}, map[string]string{"pnpm-lock.yaml": "package.json", ".npmrc": "package.json"}},
		{"chain", []string{"a.tsx", "a.ts", "a.js", "a.js.map", "a.module.css", "a.test.ts", "b.ts"}, map[string]string{"a.ts": "a.tsx", "a.js": "a.tsx", "a.js.map": "a.tsx", "a.module.css": "a.tsx", "a.test.ts": "a.tsx"}},
		{"overlap", []string{"a.c", "a.cpp", "a.h"}, map[string]string{"a.h": "a.c"}},
		{"self", []string{"Dockerfile", "Dockerfile.dev", "pyproject.toml", "uv.lock"}, map[string]string{"Dockerfile.dev": "Dockerfile", "uv.lock": "pyproject.toml"}},
		{"case", []string{"Go.mod", "go.sum"}, map[string]string{}},
		{"literal brackets", []string{"[id].tsx", "[id].test.ts", "id.test.ts"}, map[string]string{"[id].test.ts": "[id].tsx"}},
		{"empty", nil, map[string]string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for range 2 {
				if got := Parents(tc.files); !maps.Equal(got, tc.want) {
					t.Fatalf("Parents(%q) = %v, want %v", tc.files, got, tc.want)
				}
				slices.Reverse(tc.files)
			}
		})
	}
}

func TestCycles(t *testing.T) {
	config := rules(map[string]string{"a": "b", "b": "a, c", "root": "a"})
	if got := parents([]string{"a", "b", "c"}, config); len(got) != 0 {
		t.Fatalf("cycle lost files: %v", got)
	}
	want := map[string]string{"a": "root", "b": "root", "c": "root"}
	if got := parents([]string{"a", "b", "c", "root"}, config); !maps.Equal(got, want) {
		t.Fatalf("cycle with a root: %v, want %v", got, want)
	}
}

// Updating the bundled asset must not silently introduce unsupported syntax.
func TestBundledPatterns(t *testing.T) {
	var config map[string]string
	if err := json.Unmarshal(patternsJSON, &config); err != nil {
		t.Fatal(err)
	}
	for parent, children := range config {
		for _, p := range append(strings.Split(children, ","), parent) {
			if strings.Count(p, "*") > 1 || strings.Contains(strings.ReplaceAll(p, "$(capture)", ""), "$") {
				t.Errorf("unsupported pattern %q", p)
			}
		}
	}
}

func BenchmarkParents(b *testing.B) {
	var files []string
	for i := range 2000 {
		for _, ext := range []string{".tsx", ".ts", ".js", ".test.ts", ".module.css"} {
			files = append(files, fmt.Sprintf("component%04d%s", i, ext))
		}
	}
	for b.Loop() {
		Parents(files)
	}
}

func TestLintSuppressions(t *testing.T) {
	got := Parents([]string{"package.json", "eslint-suppressions.json", "oxlint-suppressions.json"})
	for _, name := range []string{"eslint-suppressions.json", "oxlint-suppressions.json"} {
		if got[name] != "package.json" {
			t.Errorf("%s nests under %q, want package.json", name, got[name])
		}
	}
}
