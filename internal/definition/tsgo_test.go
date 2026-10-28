package definition

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestJSONC(t *testing.T) {
	in := `{
  // the compiler
  "js/ts.tsdk.path": "a//b", /* not a comment: "x" */
  "list": ["\"q\"", 1,],
}`
	want := "{\n  \n  \"js/ts.tsdk.path\": \"a//b\", \n  \"list\": [\"\\\"q\\\"\", 1],\n}"
	want = strings.Replace(want, ",\n}", "\n}", 1)
	if got := string(jsonc([]byte(in))); got != want {
		t.Errorf("jsonc\n%s\nwant\n%s", got, want)
	}
}

// fakeTypeScript installs a TypeScript of the version under dir.
func fakeTypeScript(t *testing.T, dir, version string) string {
	t.Helper()
	os.MkdirAll(filepath.Join(dir, "bin"), 0o755)
	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"version":"`+version+`"}`), 0o644)
	tsc := filepath.Join(dir, "bin", "tsc")
	os.WriteFile(tsc, []byte("#!/bin/sh\n"), 0o755)
	return tsc
}

func TestTsgo(t *testing.T) {
	root := t.TempDir()
	if _, _, _, ok := tsgo(root); ok {
		t.Fatal("found TypeScript 7 in an empty repository")
	}
	fakeTypeScript(t, filepath.Join(root, "node_modules", "typescript"), "6.0.2")
	if _, _, _, ok := tsgo(root); ok {
		t.Fatal("TypeScript 6 ran as TypeScript 7")
	}
	// As dify's: the compiler of a package, its content mappers enabled.
	tsc := fakeTypeScript(t, filepath.Join(root, "packages", "mapper", "node_modules", "typescript"), "7.1.0-beta")
	os.MkdirAll(filepath.Join(root, ".vscode"), 0o755)
	os.WriteFile(filepath.Join(root, ".vscode", "settings.json"), []byte(`{
  "js/ts.experimental.useTsgo": true,
  // VS Code's settings take comments
  "js/ts.tsdk.path": "./packages/mapper/node_modules/typescript",
  "js/ts.contentMappers.enabled": true,
}`), 0o644)
	program, args, options, ok := tsgo(root)
	if !ok || program != tsc || !reflect.DeepEqual(args, []string{"--lsp", "--stdio"}) ||
		!reflect.DeepEqual(options, map[string]any{"runExternalCode": true}) {
		t.Fatalf("tsgo %s %v %v %v", program, args, options, ok)
	}
	// Installed in the repository, TypeScript 7 runs no mapper unasked.
	os.Remove(filepath.Join(root, ".vscode", "settings.json"))
	tsc = fakeTypeScript(t, filepath.Join(root, "node_modules", "typescript"), "7.0.0")
	if program, _, options, ok := tsgo(root); !ok || program != tsc || !reflect.DeepEqual(options, map[string]any{"runExternalCode": false}) {
		t.Fatalf("tsgo %s %v %v", program, options, ok)
	}
}

// Opt in with a checkout of dify, whose OpenAPI content mapper needs
// TypeScript 7.1 installed there.
func TestInstalledContentMapper(t *testing.T) {
	root := os.Getenv("PEEK_DIFY")
	if root == "" {
		t.Skip("set PEEK_DIFY to a dify checkout to test its content mapper")
	}
	path := filepath.Join(root, "packages", "openapi-content-mapper", "examples", "petstore", "demo.ts")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(b)
	client := New(root)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	call := strings.Index(text, "await api.pets.byPetId.get(") + len("await api.pets.byPetId.g")
	h, err := client.Hover(ctx, path, text, call, 0)
	if err != nil || !strings.Contains(h.Markdown, `"/pets/{petId}"`) {
		t.Fatalf("hover %q: %v", h.Markdown, err)
	}
	if !h.Expandable {
		t.Errorf("TypeScript 7 does not expand %q", h.Markdown)
	}
	locs, err := client.Definition(ctx, path, text, call)
	if err != nil || len(locs) == 0 || filepath.Base(locs[0].Path) != "pets.openapi.yaml" {
		t.Fatalf("definition %v: %v", locs, err)
	}
}
