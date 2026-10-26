package definition

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Client owns the language servers of one repository. Queries serialize
// document synchronization; Close also interrupts initialization and queries.
type Client struct {
	root    string
	ctx     context.Context
	cancel  context.CancelFunc
	gate    chan struct{}
	servers map[string]*rpc
}

func New(root string) *Client {
	ctx, cancel := context.WithCancel(context.Background())
	return &Client{root: root, ctx: ctx, cancel: cancel, gate: make(chan struct{}, 1), servers: map[string]*rpc{}}
}

func (c *Client) Close() { c.cancel() }

func (c *Client) Definition(ctx context.Context, path, text string, offset int) ([]Location, error) {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-c.ctx.Done():
		return nil, c.ctx.Err()
	}
	if err := c.ctx.Err(); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	language := Language(path)
	if language == "" {
		return nil, errors.New("definition lookup supports Go, TypeScript and Kotlin files")
	}
	key := language
	if key == "typescriptreact" {
		key = "typescript"
	}
	root, serverKey := c.root, key
	if key == "kotlin" {
		root = kotlinRoot(c.root, path)
		serverKey += "\x00" + root
	}
	r := c.servers[serverKey]
	if r != nil {
		select {
		case <-r.done:
			delete(c.servers, serverKey)
			r = nil
		default:
		}
	}
	if r == nil {
		program, args, err := serverCommand(root, key)
		if err != nil {
			return nil, err
		}
		r, err = startRPC(c.ctx, root, program, args...)
		if err != nil {
			return nil, err
		}
		var initialized struct {
			Capabilities struct {
				PositionEncoding string `json:"positionEncoding"`
				Experimental     struct {
					WorkspaceImport bool `json:"workspaceImport"`
				} `json:"experimental"`
			} `json:"capabilities"`
		}
		var options any
		if key == "typescript" {
			// The syntax server can answer the first click with only the local
			// import alias while the semantic project is still loading.
			options = map[string]any{"tsserver": map[string]any{"useSyntaxServer": "never"}, "disableAutomaticTypingAcquisition": true}
		}
		err = r.call(ctx, "initialize", map[string]any{
			"initializationOptions": options,
			"processId":             os.Getpid(), "rootUri": fileURI(root),
			"clientInfo":       map[string]any{"name": "Peek"},
			"workspaceFolders": []any{map[string]any{"uri": fileURI(root), "name": filepath.Base(root)}},
			"capabilities": map[string]any{
				"general":      map[string]any{"positionEncodings": []string{"utf-16"}},
				"textDocument": map[string]any{"definition": map[string]any{"linkSupport": true}},
				"workspace":    map[string]any{"configuration": true, "workspaceFolders": true},
			},
		}, &initialized)
		if err == nil && initialized.Capabilities.PositionEncoding != "" && initialized.Capabilities.PositionEncoding != "utf-16" {
			err = errors.New("language server did not select UTF-16 positions")
		}
		if err == nil {
			err = r.notify("initialized", struct{}{})
		}
		if err == nil && key == "kotlin" && initialized.Capabilities.Experimental.WorkspaceImport {
			err = r.waitImport(ctx)
		}
		if err != nil {
			r.stop()
			return nil, err
		}
		c.servers[serverKey] = r
	}
	// didOpen sends exactly the visible source. Closing after the query lets
	// the server read later disk changes instead of retaining a stale overlay.
	uri := fileURI(path)
	stop := context.AfterFunc(ctx, r.stop)
	defer stop()
	if err := r.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": language, "version": 1, "text": text,
	}}); err != nil {
		r.stop()
		delete(c.servers, serverKey)
		return nil, err
	}
	var raw json.RawMessage
	err := r.call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri}, "position": PositionAt(text, offset),
	}, &raw)
	if err != nil {
		r.stop()
		delete(c.servers, serverKey)
		return nil, err
	}
	if err := r.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}}); err != nil {
		r.stop()
		delete(c.servers, serverKey)
		return nil, err
	}
	return locations(raw)
}

func serverCommand(root, language string) (string, []string, error) {
	var name, install string
	var args []string
	switch language {
	case "go":
		name, install = "gopls", "go install golang.org/x/tools/gopls@latest"
	case "typescript":
		name, install = "typescript-language-server", "npm install -g typescript-language-server typescript@6"
		args = []string{"--stdio"}
		if p := filepath.Join(root, "node_modules", ".bin", name); executable(p) {
			return p, args, nil
		}
	case "kotlin":
		name, install = "kotlin-lsp", "brew install JetBrains/utils/kotlin-lsp"
		args = []string{"--stdio"}
	default:
		return "", nil, fmt.Errorf("unsupported language: %s", language)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, args, nil
	}
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".local", "bin"), filepath.Join(home, "go", "bin")}
	if d := os.Getenv("GOBIN"); d != "" {
		dirs = append([]string{d}, dirs...)
	}
	for _, d := range filepath.SplitList(os.Getenv("GOPATH")) {
		dirs = append(dirs, filepath.Join(d, "bin"))
	}
	for _, d := range dirs {
		if p := filepath.Join(d, name); executable(p) {
			return p, args, nil
		}
	}
	return "", nil, fmt.Errorf("%s is not installed. Install it with: %s", name, install)
}

func executable(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0111 != 0
}

func locations(raw json.RawMessage) ([]Location, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	var entries []json.RawMessage
	if strings.HasPrefix(strings.TrimSpace(string(raw)), "[") {
		if err := json.Unmarshal(raw, &entries); err != nil {
			return nil, err
		}
	} else {
		entries = []json.RawMessage{raw}
	}
	var out []Location
	seen := map[Location]bool{}
	for _, entry := range entries {
		var l struct {
			URI                  string `json:"uri"`
			Range                Range  `json:"range"`
			TargetURI            string `json:"targetUri"`
			TargetSelectionRange Range  `json:"targetSelectionRange"`
		}
		if err := json.Unmarshal(entry, &l); err != nil {
			return nil, err
		}
		if l.TargetURI != "" {
			l.URI, l.Range = l.TargetURI, l.TargetSelectionRange
		}
		path, err := filePath(l.URI)
		if err != nil {
			return nil, err
		}
		loc := Location{Path: path, Range: l.Range}
		if !seen[loc] {
			out = append(out, loc)
			seen[loc] = true
		}
	}
	return out, nil
}
