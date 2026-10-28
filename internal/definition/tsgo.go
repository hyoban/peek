package definition

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// tsgo is TypeScript 7's language server, its compiler's own, where the
// repository asks for it as VS Code reads it: js/ts.experimental.useTsgo
// with js/ts.tsdk.path in .vscode/settings.json, or TypeScript 7 installed
// in its node_modules. Content mappers, which run the repository's code to
// type imports such as an OpenAPI document, run where the settings enable
// js/ts.contentMappers.enabled.
func tsgo(root string) (program string, args []string, options any, ok bool) {
	var settings struct {
		UseTsgo        bool   `json:"js/ts.experimental.useTsgo"`
		TSDK           string `json:"js/ts.tsdk.path"`
		ContentMappers bool   `json:"js/ts.contentMappers.enabled"`
	}
	if b, err := os.ReadFile(filepath.Join(root, ".vscode", "settings.json")); err == nil {
		json.Unmarshal(jsonc(b), &settings)
	}
	dirs := []string{filepath.Join(root, "node_modules", "typescript")}
	if settings.UseTsgo && settings.TSDK != "" {
		dir := settings.TSDK
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, dir)
		}
		if filepath.Base(dir) == "lib" {
			dir = filepath.Dir(dir)
		}
		dirs = append([]string{dir}, dirs...)
	}
	for _, dir := range dirs {
		if p := filepath.Join(dir, "bin", "tsc"); executable(p) && typescriptMajor(dir) >= 7 {
			return p, []string{"--lsp", "--stdio"}, map[string]any{"runExternalCode": settings.ContentMappers}, true
		}
	}
	return "", nil, nil, false
}

func typescriptMajor(dir string) int {
	var pkg struct {
		Version string `json:"version"`
	}
	b, err := os.ReadFile(filepath.Join(dir, "package.json"))
	if err != nil || json.Unmarshal(b, &pkg) != nil {
		return 0
	}
	major, _, _ := strings.Cut(pkg.Version, ".")
	n, _ := strconv.Atoi(major)
	return n
}

// jsonc makes VS Code's settings JSON: without comments, and without the
// commas that end lists and objects.
func jsonc(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); i++ {
		switch {
		case b[i] == '"':
			j := i + 1
			for j < len(b) && b[j] != '"' {
				if b[j] == '\\' {
					j++
				}
				j++
			}
			j = min(j, len(b)-1)
			out = append(out, b[i:j+1]...)
			i = j
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '/':
			for i < len(b) && b[i] != '\n' {
				i++
			}
			i--
		case b[i] == '/' && i+1 < len(b) && b[i+1] == '*':
			end := strings.Index(string(b[i+2:]), "*/")
			if end < 0 {
				return out
			}
			i += end + 3
		case b[i] == ',':
			j := i + 1
			for j < len(b) && strings.IndexByte(" \t\r\n", b[j]) >= 0 {
				j++
			}
			if j < len(b) && (b[j] == '}' || b[j] == ']') {
				continue
			}
			out = append(out, ',')
		default:
			out = append(out, b[i])
		}
	}
	return out
}
