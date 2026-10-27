package codex

import (
	"context"
	"peek/internal/git"
	"regexp"
	"slices"
	"strings"
)

// conventionalSubject is a subject of the Conventional Commits, as
// "feat(review)!: explain a change".
var conventionalSubject = regexp.MustCompile(`^[a-z]+(\([^()\n]+\))?!?: \S`)

// conventionConfigs are the files of commitlint and commitizen, which a
// repository that checks its messages against the Conventional Commits
// keeps at its top.
var conventionConfigs = []string{
	".commitlintrc", ".commitlintrc.json", ".commitlintrc.yaml", ".commitlintrc.yml",
	".commitlintrc.js", ".commitlintrc.cjs", ".commitlintrc.mjs", ".commitlintrc.ts", ".commitlintrc.cts",
	"commitlint.config.js", "commitlint.config.cjs", "commitlint.config.mjs",
	"commitlint.config.ts", "commitlint.config.cts", "commitlint.config.mts",
	".czrc", ".cz.json", ".cz.toml", "cz.json",
}

// conventionDocs are the notes to agents and contributors that may say
// how the repository writes its messages.
var conventionDocs = []string{"AGENTS.md", "CLAUDE.md", "CONTRIBUTING.md", ".github/CONTRIBUTING.md", "docs/CONTRIBUTING.md"}

// conventionNoteLimit bounds the part of a note the prompt quotes.
const conventionNoteLimit = 3000

// commitConvention is what the prompt says of the repository's commit
// convention, "" where it has none: the Conventional Commits, where its
// notes name them, where commitlint or commitizen checks its messages,
// or where most of its recent subjects follow them, with the part of the
// note that says so. All is read from the staged tree and HEAD, as the
// rest of the prompt is.
func commitConvention(ctx context.Context, repo string, snapshot git.Snapshot) string {
	source, note := "", ""
	paths := append(append([]string{"package.json"}, conventionConfigs...), conventionDocs...)
	out, _ := git.Run(ctx, repo, append([]string{"ls-tree", "--name-only", "--full-tree", snapshot.Tree, "--"}, paths...)...)
	have := strings.Fields(out)
	for _, doc := range conventionDocs {
		if !slices.Contains(have, doc) {
			continue
		}
		text, err := git.Bounded(repo, 256*1024, "show", snapshot.Tree+":"+doc)
		if err != nil {
			continue
		}
		if part := conventionPart(text); part != "" {
			source, note = doc, part
			break
		}
	}
	if source == "" {
		for _, name := range conventionConfigs {
			if slices.Contains(have, name) {
				source = name
				break
			}
		}
	}
	if source == "" && slices.Contains(have, "package.json") {
		if text, err := git.Bounded(repo, 256*1024, "show", snapshot.Tree+":package.json"); err == nil &&
			(strings.Contains(text, `"commitlint"`) || strings.Contains(text, "@commitlint/") || strings.Contains(text, "cz-conventional-changelog")) {
			source = "package.json"
		}
	}
	if source == "" && snapshot.Head != "" {
		if log, err := git.Bounded(repo, 8192, "log", "-10", "--no-merges", "--format=%s", snapshot.Head); err == nil {
			subjects, n := 0, 0
			for s := range strings.Lines(log) {
				subjects++
				if conventionalSubject.MatchString(s) {
					n++
				}
			}
			if subjects >= 3 && 2*n > subjects {
				source = "its recent subjects"
			}
		}
	}
	if source == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThis repository writes its commit messages as Conventional Commits (found in " + source + "), " +
		"and the subject must follow them even where some recent subjects do not: `type(scope): summary`, " +
		"the type one such as feat, fix, perf, refactor, docs, test, build, ci, chore, style or revert, " +
		"the scope optional and as the recent subjects name their parts, the summary in the imperative, " +
		"and `!` after the type or scope for a breaking change.\n")
	if note != "" {
		b.WriteString("What " + source + " says of commit messages, to follow for the message alone:\n" + note + "\n")
	}
	return b.String()
}

// conventionPart is the part of a Markdown note that names the
// Conventional Commits, "" where none does: its section, under its
// heading, or else its paragraph, cut to conventionNoteLimit.
func conventionPart(text string) string {
	lower := strings.ToLower(text)
	i := strings.Index(lower, "conventional commits")
	if j := strings.Index(lower, "conventionalcommits.org"); i < 0 || (j >= 0 && j < i) {
		i = j
	}
	if i < 0 {
		return ""
	}
	lines := strings.SplitAfter(text, "\n")
	at, off := 0, 0 // the line of the match
	for k, line := range lines {
		if off+len(line) > i {
			at = k
			break
		}
		off += len(line)
	}
	level := func(line string) int {
		n := len(line) - len(strings.TrimLeft(line, "#"))
		if n == 0 || n > 6 || !strings.HasPrefix(line[n:], " ") {
			return 0
		}
		return n
	}
	start, end := at, at+1
	head := 0
	for start >= 0 && head == 0 {
		head = level(lines[start])
		if head == 0 {
			start--
		}
	}
	if head == 0 {
		// No heading over it: its paragraph.
		for start = at; start > 0 && strings.TrimSpace(lines[start-1]) != ""; start-- {
		}
		for ; end < len(lines) && strings.TrimSpace(lines[end]) != ""; end++ {
		}
	} else {
		for ; end < len(lines); end++ {
			if l := level(lines[end]); l > 0 && l <= head {
				break
			}
		}
	}
	part := strings.TrimSpace(strings.Join(lines[start:end], ""))
	if len(part) > conventionNoteLimit {
		part = strings.ToValidUTF8(part[:conventionNoteLimit], "") + "\n[cut]"
	}
	return part
}
