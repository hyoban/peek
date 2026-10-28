package definition

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestPositions(t *testing.T) {
	for _, text := range []string{"a中文😀b\nnext", "a中文😀b\r\nnext", "a\rb"} {
		for i := range text {
			if text[i] == '\n' && i > 0 && text[i-1] == '\r' {
				continue
			}
			p := PositionAt(text, i)
			if got := OffsetAt(text, p); got != i {
				t.Fatalf("%q at %d -> %+v -> %d", text, i, p, got)
			}
		}
	}
	if p := PositionAt("😀name", len("😀")); p.Character != 2 {
		t.Fatalf("emoji position %+v", p)
	}
	if got := OffsetAt("😀name\nnext", Position{Character: 100}); got != len("😀name") {
		t.Fatalf("clamped to %d", got)
	}
}

func TestLocations(t *testing.T) {
	uri := fileURI("/tmp/a #中文.ts")
	for _, raw := range []string{
		fmt.Sprintf(`{"uri":%q,"range":{"start":{"line":2,"character":3},"end":{"line":2,"character":7}}}`, uri),
		fmt.Sprintf(`[{"targetUri":%q,"targetRange":{"start":{"line":0,"character":0}},"targetSelectionRange":{"start":{"line":2,"character":3},"end":{"line":2,"character":7}}}]`, uri),
	} {
		locs, err := locations(json.RawMessage(raw))
		if err != nil || len(locs) != 1 || locs[0].Path != "/tmp/a #中文.ts" || locs[0].Range.Start != (Position{2, 3}) {
			t.Fatalf("locations %v: %v", locs, err)
		}
	}
	for _, uri := range []string{"https://example.com/a.ts", "file://remote/a.ts", "file:relative.ts"} {
		if _, err := filePath(uri); err == nil {
			t.Errorf("accepted %s", uri)
		}
	}
	if locs, err := locations(json.RawMessage("null")); err != nil || len(locs) != 0 {
		t.Fatalf("null: %v %v", locs, err)
	}
}

// A subprocess exercises the actual framing and server-to-client requests.
func TestDefinitionServerProcess(t *testing.T) {
	if os.Getenv("PEEK_DEFINITION_HELPER") != "1" {
		return
	}
	in := bufio.NewReader(os.Stdin)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n%s", len(b), b)
	}
	for {
		m, err := readMessage(in)
		if err != nil {
			os.Exit(0)
		}
		if m.Method == os.Getenv("PEEK_DEFINITION_STALL") {
			time.Sleep(time.Minute)
		}
		if len(m.ID) == 0 {
			continue
		}
		if m.Method == "initialize" {
			send(map[string]any{"jsonrpc": "2.0", "id": "config", "method": "workspace/configuration", "params": map[string]any{"items": []any{map[string]any{"section": "gopls"}}}})
			reply, err := readMessage(in)
			if err != nil || string(reply.Result) != "[null]" {
				os.Exit(2)
			}
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": map[string]any{"capabilities": map[string]any{}}})
		} else {
			send(map[string]any{"jsonrpc": "2.0", "method": "textDocument/publishDiagnostics", "params": map[string]any{}})
			send(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": nil})
		}
	}
}

func TestRPCRequestsAndCancellation(t *testing.T) {
	t.Setenv("PEEK_DEFINITION_HELPER", "1")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r, err := startRPC(ctx, t.TempDir(), os.Args[0], "-test.run=^TestDefinitionServerProcess$")
	if err != nil {
		t.Fatal(err)
	}
	defer r.stop()
	request, done := context.WithTimeout(ctx, 5*time.Second)
	defer done()
	var raw json.RawMessage
	if err := r.call(request, "initialize", struct{}{}, &raw); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := r.call(request, "textDocument/definition", struct{}{}, &raw); err != nil || string(raw) != "null" {
			t.Fatalf("definition: %s %v", raw, err)
		}
	}
	r.stop()
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop")
	}
	// Initialization can hang too; cancellation must release its pipes.
	t.Setenv("PEEK_DEFINITION_STALL", "initialize")
	r, err = startRPC(ctx, t.TempDir(), os.Args[0], "-test.run=^TestDefinitionServerProcess$")
	if err != nil {
		t.Fatal(err)
	}
	defer r.stop()
	request, done = context.WithTimeout(ctx, 150*time.Millisecond)
	defer done()
	if err := r.call(request, "initialize", struct{}{}, &raw); err == nil {
		t.Fatal("stalled server succeeded")
	}
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("canceled server did not exit")
	}
}

// Opt in so ordinary tests do not require locally installed language servers.
func TestInstalledServers(t *testing.T) {
	if os.Getenv("PEEK_LSP_TEST") != "1" {
		t.Skip("set PEEK_LSP_TEST=1 to test installed language servers")
	}
	for _, language := range []string{"go", "typescript"} {
		t.Run(language, func(t *testing.T) {
			dir := t.TempDir()
			files := map[string]string{}
			var source, target, name string
			if language == "go" {
				files["go.mod"] = "module example.com/navigation\n\ngo 1.24\n"
				source, target, name = "main.go", "greet.go", "greet"
				files[source] = "package main\nimport \"fmt\"\nfunc main() { _ = \"😀\"; fmt.Println(greet()) }\n"
				files[target] = "package main\nfunc greet() string { return \"hi\" }\n"
			} else {
				source, target, name = "main.tsx", "greet.ts", "greet"
				files["tsconfig.json"] = `{"compilerOptions":{"strict":true,"target":"ES2020","jsx":"preserve","baseUrl":".","paths":{"@lib/*":["./*"]}}}`
				files[source] = "import { greet } from '@lib/greet';\nconst emoji = '😀'; export const result = greet();\nexport const promise = Promise.resolve(result);\ninterface Box { value: string }\nexport const box: Box = { value: result };\n"
				files[target] = "export function greet() { return 'hi'; }\n"
			}
			for p, text := range files {
				if err := os.WriteFile(filepath.Join(dir, p), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			client := New(dir)
			defer client.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
			defer cancel()
			for range 2 {
				locs, err := client.Definition(ctx, filepath.Join(dir, source), files[source], strings.LastIndex(files[source], name))
				if err != nil || len(locs) == 0 {
					t.Fatalf("definition: %v %v", locs, err)
				}
				if filepath.Base(locs[0].Path) != target {
					t.Fatalf("target %s, want %s", locs[0].Path, target)
				}
				start := OffsetAt(files[target], locs[0].Range.Start)
				if !strings.HasPrefix(files[target][start:], name) {
					t.Fatalf("target offset %d", start)
				}
			}
			h, err := client.Hover(ctx, filepath.Join(dir, source), files[source], strings.LastIndex(files[source], name), 0)
			if err != nil || !strings.Contains(h.Markdown, "string") {
				t.Fatalf("hover %+v: %v", h, err)
			}
			if language == "typescript" {
				// A value's type names its interface, which expands.
				at := strings.LastIndex(files[source], "box")
				short, err := client.Hover(ctx, filepath.Join(dir, source), files[source], at, 0)
				if err != nil || !short.Expandable {
					t.Fatalf("hover %+v: %v", short, err)
				}
				long, err := client.Hover(ctx, filepath.Join(dir, source), files[source], at, 1)
				if err != nil || len(long.Markdown) <= len(short.Markdown) {
					t.Fatalf("expanded %q from %q: %v", long.Markdown, short.Markdown, err)
				}
			}
			// Standard-library definitions live outside the repository.
			external := "Println"
			if language == "typescript" {
				external = "Promise"
			}
			locs, err := client.Definition(ctx, filepath.Join(dir, source), files[source], strings.LastIndex(files[source], external))
			if err != nil || len(locs) == 0 || strings.HasPrefix(locs[0].Path, dir+string(filepath.Separator)) {
				t.Fatalf("external definition: %v %v", locs, err)
			}
			client.Close()
			if _, err := client.Definition(ctx, filepath.Join(dir, source), files[source], 0); err == nil {
				t.Fatal("closed client accepted a query")
			}
		})
	}
}

func TestRestartAfterQuery(t *testing.T) {
	t.Setenv("PEEK_DEFINITION_HELPER", "1")
	c := New(t.TempDir())
	defer c.Close()
	r, err := startRPC(c.ctx, c.root, os.Args[0], "-test.run=^TestDefinitionServerProcess$")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var raw json.RawMessage
	if err := r.call(ctx, "initialize", struct{}{}, &raw); err != nil {
		t.Fatal(err)
	}
	c.servers["go"] = r
	path := filepath.Join(c.root, "main.go")
	// A refresh while the server answers lets the answer through.
	err = c.query(ctx, "go", "go", path, "package main\n", func(r *rpc, uri string) error {
		c.Restart()
		return r.call(ctx, "textDocument/hover", struct{}{}, &raw)
	})
	if err != nil {
		t.Fatalf("query cut short: %v", err)
	}
	select {
	case <-r.done:
		t.Fatal("server stopped before the next query")
	case <-time.After(100 * time.Millisecond):
	}
	// The next query starts it anew.
	c.query(ctx, "go", "go", path, "package main\n", func(*rpc, string) error { return nil })
	select {
	case <-r.done:
	case <-time.After(3 * time.Second):
		t.Fatal("server kept after a restart")
	}
}
