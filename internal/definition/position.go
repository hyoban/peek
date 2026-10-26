// Package definition finds declarations in the working tree through language servers.
package definition

import (
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// Position counts lines from zero and columns in UTF-16 code units, as LSP does.
type Position struct {
	Line      int `json:"line"`
	Character int `json:"character"`
}

type Range struct {
	Start Position `json:"start"`
	End   Position `json:"end"`
}

type Location struct {
	Path  string
	Range Range
}

// PositionAt translates the editor's UTF-8 byte offset to LSP's position.
func PositionAt(text string, offset int) Position {
	offset = max(0, min(offset, len(text)))
	var p Position
	for i := 0; i < offset; {
		r, n := utf8.DecodeRuneInString(text[i:])
		if i+n > offset {
			break
		}
		i += n
		if r == '\r' || r == '\n' {
			if r == '\r' && i < offset && text[i] == '\n' {
				i++
			}
			p.Line++
			p.Character = 0
		} else {
			p.Character++
			if r > 0xffff {
				p.Character++
			}
		}
	}
	return p
}

// OffsetAt converts a server position back, clamping it to its line.
func OffsetAt(text string, p Position) int {
	i := 0
	for line := 0; line < p.Line && i < len(text); line++ {
		for i < len(text) && text[i] != '\r' && text[i] != '\n' {
			i++
		}
		if i < len(text) {
			cr := text[i] == '\r'
			i++
			if cr && i < len(text) && text[i] == '\n' {
				i++
			}
		}
	}
	for col := 0; col < p.Character && i < len(text) && text[i] != '\r' && text[i] != '\n'; {
		r, n := utf8.DecodeRuneInString(text[i:])
		units := 1
		if r > 0xffff {
			units = 2
		}
		if col+units > p.Character {
			break
		}
		i += n
		col += units
	}
	return i
}

func fileURI(path string) string {
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

func filePath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "file" || (u.Host != "" && u.Host != "localhost") || !filepath.IsAbs(u.Path) || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("definition is not a local file: %s", uri)
	}
	return filepath.Clean(u.Path), nil
}

func Language(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".go":
		return "go"
	case ".ts", ".mts", ".cts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	}
	return ""
}
