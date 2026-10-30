// Package filenesting groups sibling files with the rules from
// https://github.com/antfu/vscode-file-nesting-config (updated 2026-06-24).
// The bundled snapshot works offline; its MIT license is in LICENSE. It also
// nests oxlint-suppressions.json as the rules nest eslint-suppressions.json.
package filenesting

import (
	_ "embed"
	"encoding/json"
	"slices"
	"strings"
)

//go:embed patterns.json
var patternsJSON []byte

var defaults = loadRules()

type pattern struct {
	prefix, suffix string
	star           bool
}

func compile(s string) pattern {
	prefix, suffix, star := strings.Cut(s, "*")
	return pattern{prefix, suffix, star}
}

func (p pattern) match(name string) (string, bool) {
	if !p.star {
		return "", name == p.prefix
	}
	if len(name) < len(p.prefix)+len(p.suffix) || !strings.HasPrefix(name, p.prefix) || !strings.HasSuffix(name, p.suffix) {
		return "", false
	}
	return name[len(p.prefix) : len(name)-len(p.suffix)], true
}

type rule struct {
	parent   pattern
	children []string
}

func loadRules() []rule {
	var config map[string]string
	if err := json.Unmarshal(patternsJSON, &config); err != nil {
		panic(err) // a malformed bundled asset
	}
	return rules(config)
}

func rules(config map[string]string) []rule {
	var result []rule
	for parent, children := range config {
		var patterns []string
		for child := range strings.SplitSeq(children, ",") {
			if child = strings.TrimSpace(child); child != "" {
				patterns = append(patterns, child)
			}
		}
		result = append(result, rule{compile(parent), patterns})
	}
	return result
}

// Parents maps each nested filename to its parent. Pass only files in one
// directory, without their paths. Matching is case-sensitive and supports
// VS Code's single-star patterns and $(capture) substitutions.
// Chains flatten to one level. A file with several roots appears only once,
// under the first root by name, so selection has a unique, stable identity.
// Files without a root (cycles) stay at the top instead of disappearing.
func Parents(names []string) map[string]string { return parents(names, defaults) }

func parents(names []string, rules []rule) map[string]string {
	names = slices.Clone(names)
	slices.Sort(names)
	names = slices.Compact(names)
	ancestors := map[string][]string{}
	for _, parent := range names {
		for _, rule := range rules {
			capture, ok := rule.parent.match(parent)
			if !ok {
				continue
			}
			for _, child := range rule.children {
				p := compile(strings.ReplaceAll(child, "$(capture)", capture))
				// Most rules have a literal prefix; only visit that range
				// of the directory, rather than scan it for every pattern.
				i, _ := slices.BinarySearch(names, p.prefix)
				for ; i < len(names) && strings.HasPrefix(names[i], p.prefix); i++ {
					name := names[i]
					if _, ok := p.match(name); ok && name != parent {
						ancestors[name] = append(ancestors[name], parent)
					}
					if !p.star {
						break
					}
				}
			}
		}
	}
	result := map[string]string{}
	for _, name := range names {
		seen := map[string]bool{}
		root := ""
		var visit func(string)
		visit = func(p string) {
			if seen[p] {
				return
			}
			seen[p] = true
			if len(ancestors[p]) == 0 && (root == "" || p < root) {
				root = p
			}
			for _, parent := range ancestors[p] {
				visit(parent)
			}
		}
		visit(name)
		if root != "" && root != name {
			result[name] = root
		}
	}
	return result
}
