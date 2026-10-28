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

// serverOf names the server for the file at path, and the language it
// opens the file as: npmx's for package manifests, where it finds the
// package.json of a dependency as installed.
func serverOf(path string) (key, language string, err error) {
	if language = Manifest(path); language != "" {
		return "npmx", language, nil
	}
	switch language = Language(path); language {
	case "":
		return "", "", errors.New("language servers support Go, TypeScript, Kotlin and package manifests")
	case "typescriptreact":
		return "typescript", language, nil
	}
	return language, language, nil
}

func (c *Client) Definition(ctx context.Context, path, text string, offset int) ([]Location, error) {
	key, language, err := serverOf(path)
	if err != nil {
		return nil, err
	}
	var raw json.RawMessage
	err = c.query(ctx, key, language, path, text, func(r *rpc, uri string) error {
		return r.call(ctx, "textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "position": PositionAt(text, offset),
		}, &raw)
	})
	if err != nil {
		return nil, err
	}
	return locations(raw)
}

// query opens text as the document at path on the server for key, asks
// it, and closes the document again.
func (c *Client) query(ctx context.Context, key, language, path, text string, ask func(r *rpc, uri string) error) error {
	select {
	case c.gate <- struct{}{}:
		defer func() { <-c.gate }()
	case <-ctx.Done():
		return ctx.Err()
	case <-c.ctx.Done():
		return c.ctx.Err()
	}
	if err := c.ctx.Err(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
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
		var err error
		if r, err = c.start(ctx, root, key); err != nil {
			return err
		}
		c.servers[serverKey] = r
	}
	// didOpen sends exactly the visible source. Closing after the query lets
	// the server read later disk changes instead of retaining a stale overlay.
	uri := fileURI(path)
	stop := context.AfterFunc(ctx, r.stop)
	defer stop()
	err := r.notify("textDocument/didOpen", map[string]any{"textDocument": map[string]any{
		"uri": uri, "languageId": language, "version": 1, "text": text,
	}})
	if err == nil {
		err = ask(r, uri)
	}
	if err == nil {
		err = r.notify("textDocument/didClose", map[string]any{"textDocument": map[string]any{"uri": uri}})
	}
	if err != nil {
		r.stop()
		delete(c.servers, serverKey)
	}
	return err
}

func (c *Client) start(ctx context.Context, root, key string) (*rpc, error) {
	var program string
	var args []string
	var options any
	var err error
	ts7 := false
	if key == "typescript" {
		program, args, options, ts7 = tsgo(root)
	}
	if !ts7 {
		if program, args, err = serverCommand(root, key); err != nil {
			return nil, err
		}
	}
	r, err := startRPC(c.ctx, root, program, args...)
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
	if key == "typescript" && !ts7 {
		// The syntax server can answer the first click with only the local
		// import alias while the semantic project is still loading.
		options = map[string]any{"tsserver": map[string]any{"useSyntaxServer": "never"}, "disableAutomaticTypingAcquisition": true, "supportsHoverVerbosity": true}
	}
	err = r.call(ctx, "initialize", map[string]any{
		"initializationOptions": options,
		"processId":             os.Getpid(), "rootUri": fileURI(root),
		"clientInfo":       map[string]any{"name": "Peek"},
		"workspaceFolders": []any{map[string]any{"uri": fileURI(root), "name": filepath.Base(root)}},
		"capabilities": map[string]any{
			"general": map[string]any{"positionEncodings": []string{"utf-16"}},
			"textDocument": map[string]any{
				"definition":   map[string]any{"linkSupport": true},
				"documentLink": map[string]any{"tooltipSupport": true},
				"hover":        map[string]any{"contentFormat": []string{"markdown", "plaintext"}},
				// Diagnostics are asked for, not pushed after each change.
				"diagnostic": map[string]any{},
			},
			"workspace": map[string]any{"configuration": true, "workspaceFolders": true},
			// TypeScript 7 expands the types of hovers, as VS Code's do.
			"experimental": map[string]any{"hoverVerbosityLevel": true},
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
	return r, nil
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
	case "npmx":
		name, install = "npmx-language-server", "npm install -g npmx-language-server"
		args = []string{"--stdio"}
		if p := filepath.Join(root, "node_modules", ".bin", name); executable(p) {
			return p, args, nil
		}
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
