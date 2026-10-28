package definition

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
)

// Manifest returns the language npmx opens the file at path as, where it
// lists dependencies npmx knows of, or "".
func Manifest(path string) string {
	switch filepath.Base(path) {
	case "package.json":
		return "json"
	case "pnpm-workspace.yaml", ".yarnrc.yml":
		return "yaml"
	}
	return ""
}

// Link is a range of a manifest that opens Target, a package's page on
// npmx.dev.
type Link struct {
	Range   Range
	Target  string
	Tooltip string
}

// Severity is LSP's: 1 an error, 2 a warning, 3 information, 4 a hint.
type Severity int

const (
	SeverityError Severity = iota + 1
	SeverityWarning
	SeverityInformation
	SeverityHint
)

// Diagnostic is what npmx finds of a dependency: that it is deprecated,
// vulnerable, has a replacement, a newer version, and the like.
type Diagnostic struct {
	Range    Range
	Severity Severity
	Message  string
	Source   string
}

// Links lists the dependencies of the manifest at path, as text, with
// the pages they open.
func (c *Client) Links(ctx context.Context, path, text string) ([]Link, error) {
	language := Manifest(path)
	if language == "" {
		return nil, errors.New("not a package manifest")
	}
	var raw []struct {
		Range   Range  `json:"range"`
		Target  string `json:"target"`
		Tooltip string `json:"tooltip"`
	}
	err := c.query(ctx, "npmx", language, path, text, func(r *rpc, uri string) error {
		return r.call(ctx, "textDocument/documentLink", map[string]any{"textDocument": map[string]any{"uri": uri}}, &raw)
	})
	if err != nil {
		return nil, err
	}
	var out []Link
	for _, l := range raw {
		if l.Target != "" {
			out = append(out, Link{Range: l.Range, Target: l.Target, Tooltip: l.Tooltip})
		}
	}
	return out, nil
}

// Diagnostics asks npmx what it finds of the dependencies of the manifest
// at path, as text. It looks them up on the registry and OSV.
func (c *Client) Diagnostics(ctx context.Context, path, text string) ([]Diagnostic, error) {
	language := Manifest(path)
	if language == "" {
		return nil, errors.New("not a package manifest")
	}
	var report struct {
		Items []struct {
			Range    Range           `json:"range"`
			Severity Severity        `json:"severity"`
			Message  string          `json:"message"`
			Source   json.RawMessage `json:"source"`
		} `json:"items"`
	}
	err := c.query(ctx, "npmx", language, path, text, func(r *rpc, uri string) error {
		return r.call(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}}, &report)
	})
	if err != nil {
		return nil, err
	}
	out := make([]Diagnostic, 0, len(report.Items))
	for _, d := range report.Items {
		var source string
		json.Unmarshal(d.Source, &source)
		if d.Severity == 0 {
			d.Severity = SeverityError
		}
		out = append(out, Diagnostic{Range: d.Range, Severity: d.Severity, Message: d.Message, Source: source})
	}
	return out, nil
}

// Installed finds the package.json of the dependency named at offset of
// the package.json at path, as installed under node_modules.
func (c *Client) Installed(ctx context.Context, path, text string, offset int) ([]Location, error) {
	if filepath.Base(path) != "package.json" {
		return nil, nil
	}
	var raw json.RawMessage
	err := c.query(ctx, "npmx", "json", path, text, func(r *rpc, uri string) error {
		return r.call(ctx, "textDocument/definition", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "position": PositionAt(text, offset),
		}, &raw)
	})
	if err != nil {
		return nil, err
	}
	return locations(raw)
}
