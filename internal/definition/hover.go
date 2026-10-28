package definition

import (
	"context"
	"encoding/json"
	"strings"
)

// Hover is what a language server says of the text at an offset, as
// Markdown, as a type and its documentation, and the range it says it
// of, when it gives one. Expandable reports that a greater verbosity
// shows more of the type, as the types it names written out.
type Hover struct {
	Markdown   string
	Range      *Range
	Expandable bool
}

// Hover asks the language server of the file at path, or npmx where it
// is a package manifest, what the text at offset is, its types expanded
// verbosity levels deep.
func (c *Client) Hover(ctx context.Context, path, text string, offset, verbosity int) (Hover, error) {
	key, language, err := serverOf(path)
	if err != nil {
		return Hover{}, err
	}
	var raw struct {
		Contents json.RawMessage `json:"contents"`
		Range    *Range          `json:"range"`
		// TypeScript 7's, and typescript-language-server's.
		CanIncreaseVerbosity      bool `json:"canIncreaseVerbosity"`
		CanIncreaseVerbosityLevel bool `json:"canIncreaseVerbosityLevel"`
	}
	err = c.query(ctx, key, language, path, text, func(r *rpc, uri string) error {
		return r.call(ctx, "textDocument/hover", map[string]any{
			"textDocument": map[string]any{"uri": uri}, "position": PositionAt(text, offset), "verbosityLevel": verbosity,
		}, &raw)
	})
	if err != nil {
		return Hover{}, err
	}
	return Hover{
		Markdown: strings.TrimSpace(hoverMarkdown(raw.Contents)), Range: raw.Range,
		Expandable: raw.CanIncreaseVerbosity || raw.CanIncreaseVerbosityLevel,
	}, nil
}

// hoverMarkdown reads a hover's contents: MarkupContent, a MarkedString,
// which is Markdown or code of a language, or a list of them.
func hoverMarkdown(raw json.RawMessage) string {
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return s
	}
	var list []json.RawMessage
	if json.Unmarshal(raw, &list) == nil {
		var parts []string
		for _, item := range list {
			if md := strings.TrimSpace(hoverMarkdown(item)); md != "" {
				parts = append(parts, md)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	var v struct {
		Kind     string `json:"kind"`
		Language string `json:"language"`
		Value    string `json:"value"`
	}
	if json.Unmarshal(raw, &v) != nil {
		return ""
	}
	switch {
	case v.Language != "":
		return "```" + v.Language + "\n" + v.Value + "\n```"
	case v.Kind == "plaintext":
		return "```\n" + v.Value + "\n```"
	}
	return v.Value
}
