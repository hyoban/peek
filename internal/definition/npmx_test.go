package definition

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManifest(t *testing.T) {
	for p, want := range map[string]string{
		"package.json": "json", "web/package.json": "json", "pnpm-workspace.yaml": "yaml",
		".yarnrc.yml": "yaml", "package-lock.json": "", "main.go": "",
	} {
		if got := Manifest(p); got != want {
			t.Errorf("%s: %q, want %q", p, got, want)
		}
	}
}

// Opt in: npmx asks npm's registry and OSV.
func TestInstalledNpmx(t *testing.T) {
	if os.Getenv("PEEK_LSP_TEST") != "1" {
		t.Skip("set PEEK_LSP_TEST=1 to test installed language servers")
	}
	dir := t.TempDir()
	text := "{\n  \"dependencies\": {\n    \"request\": \"^2.88.0\",\n    \"vue\": \"latest\"\n  }\n}\n"
	path := filepath.Join(dir, "package.json")
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(dir, "node_modules", "vue", "package.json")
	os.MkdirAll(filepath.Dir(installed), 0700)
	if err := os.WriteFile(installed, []byte(`{"name":"vue","version":"3.5.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	client := New(dir)
	defer client.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	links, err := client.Links(ctx, path, text)
	if err != nil || len(links) != 2 || !strings.HasPrefix(links[0].Target, "https://npmx.dev/package/") {
		t.Fatalf("links %+v: %v", links, err)
	}
	locs, err := client.Definition(ctx, path, text, strings.Index(text, "vue"))
	if err != nil || len(locs) != 1 {
		t.Fatalf("installed %+v: %v", locs, err)
	}
	if want, _ := filepath.EvalSymlinks(installed); locs[0].Path != installed && locs[0].Path != want {
		t.Fatalf("installed at %s, want %s", locs[0].Path, installed)
	}
	if locs, err := client.Definition(ctx, path, text, strings.Index(text, "request")); err != nil || len(locs) != 0 {
		t.Fatalf("uninstalled %+v: %v", locs, err)
	}
	h, err := client.Hover(ctx, path, text, strings.Index(text, "vue"), 0)
	if err != nil || !strings.Contains(h.Markdown, "npmx.dev") {
		t.Fatalf("hover %q: %v", h.Markdown, err)
	}
	found, err := client.Diagnostics(ctx, path, text)
	if err != nil {
		t.Fatal(err)
	}
	var deprecated bool
	for _, d := range found {
		deprecated = deprecated || d.Range.Start.Line == 2 && strings.Contains(d.Message, "deprecated")
	}
	if !deprecated {
		t.Fatalf("diagnostics %+v", found)
	}
}

func TestHoverMarkdown(t *testing.T) {
	for raw, want := range map[string]string{
		`{"kind":"markdown","value":"**x**"}`:                 "**x**",
		`{"kind":"plaintext","value":"int"}`:                  "```\nint\n```",
		`{"language":"typescript","value":"const x: number"}`: "```typescript\nconst x: number\n```",
		`["doc", {"language":"go","value":"var x int"}, ""]`:  "doc\n\n```go\nvar x int\n```",
		`"plain"`: "plain",
	} {
		if got := hoverMarkdown(json.RawMessage(raw)); got != want {
			t.Errorf("%s: %q, want %q", raw, got, want)
		}
	}
}
