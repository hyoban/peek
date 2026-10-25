package codex

import (
	"context"
	"peek/internal/filekind"
	"peek/internal/git"
	"regexp"
	"strconv"
	"strings"
)

// Groups of files in the order rankDiffs embeds them, the change list's,
// lockfiles and generated files last. Each file's diff is cut to its
// group's cap.
const groupBulk = int(filekind.Generated)

var (
	groupNames = [...]string{filekind.Code: "Code", filekind.Test: "Tests", filekind.Docs: "Docs", groupBulk: "Lockfiles and generated files"}
	groupCaps  = [...]int{filekind.Code: 8 * 1024, filekind.Test: 8 * 1024, filekind.Docs: 8 * 1024, groupBulk: 2 * 1024}
)

// generatedMarker finds the comment line that marks a generated file, Go's
// convention or the @generated tag, on either side of the diff.
var generatedMarker = regexp.MustCompile(`(?m)^[-+ ](// Code generated .* DO NOT EDIT\.$|\s*(//|#|/?\*+|--)\s*@generated\b)`)

// rankDiffs splits a diff by file and keeps what fits the budget, in the
// change list's groups and under their names, then lockfiles and generated
// files, each cut to its group's cap. generated holds the files that
// .gitattributes marks linguist-generated, or explicitly not, which wins
// over the common rules. complete is false when a diff other than a
// lockfile's or generated file's was cut or left out.
func rankDiffs(diff string, budget int, generated map[string]bool) (ranked string, complete bool) {
	groups := make([][]string, len(groupNames))
	for _, text := range splitDiff(diff) {
		g := diffGroup(text, generated)
		groups[g] = append(groups[g], text)
	}
	var b strings.Builder
	complete, left := true, false
	for g, files := range groups {
		heading := "## " + groupNames[g] + "\n"
		for _, text := range files {
			if limit := groupCaps[g]; len(text) > limit {
				text = cutDiff(text, limit)
				complete = complete && g == groupBulk
			}
			if b.Len()+len(heading)+len(text) > budget {
				left = true
				complete = complete && g == groupBulk
				continue
			}
			b.WriteString(heading + text)
			heading = ""
		}
	}
	if left {
		b.WriteString("[Diffs of the other files in the summary are left out for space.]\n")
	}
	return b.String(), complete
}

// splitDiff cuts a unified Git diff at each file header.
func splitDiff(diff string) []string {
	var files []string
	for diff != "" {
		next := strings.Index(diff, "\ndiff --git ")
		if next < 0 {
			return append(files, diff)
		}
		files, diff = append(files, diff[:next+1]), diff[next+1:]
	}
	return files
}

// cutDiff cuts a file's diff to about limit bytes keeping its whole shape,
// not only its start, which for a large change is the imports and the
// first declarations alone: its header, every hunk's @@ line, which names
// the function the hunk is in, and as many of each hunk's changed lines as
// fit, the same number for every hunk, without the context lines.
func cutDiff(text string, limit int) string {
	header, hunks := splitHunks(text)
	if len(hunks) == 0 {
		return cutBytes(text, limit, "")
	}
	limit -= 160 // room for the note
	most := 0
	for _, h := range hunks {
		most = max(most, len(h.changed))
	}
	// The most changed lines a hunk keeps, found by halving.
	lo, hi := 0, most
	for lo < hi {
		n := (lo + hi + 1) / 2
		if len(excerpt(header, hunks, n)) <= limit {
			lo = n
		} else {
			hi = n - 1
		}
	}
	cut, kept, total := excerpt(header, hunks, lo), 0, 0
	for _, h := range hunks {
		kept, total = kept+min(lo, len(h.changed)), total+len(h.changed)
	}
	note := "[This file's diff is cut here: " + strconv.Itoa(total-kept) + " of its " + strconv.Itoa(total) +
		" changed lines and its context lines are left out; every hunk's @@ line is kept.]\n"
	if kept == total {
		note = "[This file's diff is cut here: its context lines are left out, its changed lines all kept.]\n"
	}
	if len(cut) > limit { // too many hunks for even their @@ lines
		return cutBytes(cut, limit, note)
	}
	return cut + note
}

// cutBytes cuts text at the last line that ends within limit, and marks
// the cut with note, or by how much where note is "".
func cutBytes(text string, limit int, note string) string {
	keep := text[:limit]
	if i := strings.LastIndexByte(keep, '\n'); i >= 0 {
		keep = keep[:i+1]
	}
	if note == "" {
		note = "[This file's diff is cut here; " + strconv.Itoa(len(text)-len(keep)) + " more bytes.]\n"
	}
	return keep + note
}

// hunk is a hunk of a file's diff: its @@ line and its changed lines,
// without the context ones.
type hunk struct {
	head    string
	changed []string
}

// splitHunks reads a file's diff as its header, the lines before its first
// hunk, and its hunks.
func splitHunks(text string) (header string, hunks []hunk) {
	at := strings.Index(text, "\n@@ ")
	if at < 0 {
		return text, nil
	}
	header = text[:at+1]
	for _, line := range strings.SplitAfter(text[at+1:], "\n") {
		switch {
		case strings.HasPrefix(line, "@@ "):
			hunks = append(hunks, hunk{head: line})
		case strings.HasPrefix(line, "+"), strings.HasPrefix(line, "-"):
			h := &hunks[len(hunks)-1]
			h.changed = append(h.changed, line)
		}
	}
	return header, hunks
}

// excerpt is the header and the hunks, each with its first n changed lines
// and a note of how many more it has.
func excerpt(header string, hunks []hunk, n int) string {
	var b strings.Builder
	b.WriteString(header)
	for _, h := range hunks {
		b.WriteString(h.head)
		keep := min(n, len(h.changed))
		for _, line := range h.changed[:keep] {
			b.WriteString(line)
		}
		if more := len(h.changed) - keep; more > 0 {
			b.WriteString("[" + strconv.Itoa(more) + " more changed lines]\n")
		}
	}
	return b.String()
}

// diffPath reads the new path from a file's diff header, which is enough to
// classify it even where a quoted or spaced path makes the header ambiguous.
func diffPath(text string) string {
	header, _, _ := strings.Cut(text, "\n")
	if i := strings.LastIndex(header, " b/"); i >= 0 {
		return strings.TrimSuffix(header[i+3:], `"`)
	}
	if i := strings.LastIndex(header, ` "b/`); i >= 0 {
		return strings.TrimSuffix(header[i+4:], `"`)
	}
	return header
}

// diffPaths lists the paths of a diff's files, as diffPath reads them.
func diffPaths(diff string) []string {
	var paths []string
	for _, text := range splitDiff(diff) {
		paths = append(paths, diffPath(text))
	}
	return paths
}

func diffGroup(text string, generated map[string]bool) int {
	p := diffPath(text)
	if g, ok := generated[p]; ok {
		if g {
			return groupBulk
		}
	} else if filekind.IsGenerated(p) || generatedMarker.MatchString(text) {
		return groupBulk
	}
	return int(filekind.Written(p))
}

// generatedAttrs reads linguist-generated for these paths from the
// .gitattributes of the tree, so a change to them counts in its own commit.
// Paths it leaves out have the attribute unspecified. It reads none where
// Git fails, as Git before 2.40 does without check-attr --source, since the
// common rules can still group the files.
func generatedAttrs(ctx context.Context, repo, tree string, paths []string) map[string]bool {
	if len(paths) == 0 {
		return nil
	}
	out, err := git.Run(ctx, repo, append([]string{"check-attr", "-z", "--source", tree, "linguist-generated", "--"}, paths...)...)
	if err != nil {
		return nil
	}
	generated := map[string]bool{}
	fields := strings.Split(out, "\x00")
	for i := 0; i+2 < len(fields); i += 3 {
		switch fields[i+2] {
		case "set", "true":
			generated[fields[i]] = true
		case "unset", "false":
			generated[fields[i]] = false
		}
	}
	return generated
}
