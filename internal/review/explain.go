package review

import (
	"context"
	"fmt"
	"peek/internal/git"
	"strings"
)

// CommitScope is the scope of an explanation of one commit's changes.
const CommitScope = "Commit"

// CommitSnapshot is the snapshot of the commit's changes, from its first
// parent, or the empty tree for a first commit.
func CommitSnapshot(repo, hash string) (Snapshot, error) {
	g := func(args ...string) (string, error) {
		out, err := git.Bounded(repo, maxMetadata, args...)
		return strings.TrimSpace(out), err
	}
	base, err := g("rev-parse", "--verify", "--quiet", hash+"^1")
	if err != nil || base == "" {
		if base, err = g("hash-object", "-t", "tree", "/dev/null"); err != nil {
			return Snapshot{}, err
		}
	}
	return Snapshot{ID: CommitScope + "\x00" + hash, Scope: CommitScope, Base: base, Head: hash}, nil
}

// Explain has Codex explain the changes of the snapshot, in Markdown. As
// it does to review, Codex reads the changes and the code around them
// itself, in a read-only sandbox, with the user's settings.
func Explain(ctx context.Context, repo string, snapshot Snapshot, progress func(string, string)) (string, error) {
	const schema = `{"type":"object","properties":{"explanation":{"type":"string"}},"required":["explanation"],"additionalProperties":false}`
	prompt := "Explain the changes in the scope below to someone about to read them: what they do and why, " +
		"and how their parts fit together. Explore the repository as you need. Do not modify files. " +
		"Answer briefly, in Markdown. Return only the required JSON object.\n\n" + snapshot.explainScope()
	var result struct {
		Explanation string `json:"explanation"`
	}
	events := progressTo(progress, "Explaining…", "Codex is explaining the changes")
	if err := runCodex(ctx, repo, schema, prompt, events, "explanation", &result); err != nil {
		return "", err
	}
	if text := strings.TrimSpace(result.Explanation); text != "" {
		return text, nil
	}
	return "", fmt.Errorf("Codex returned no explanation")
}

func (s Snapshot) explainScope() string {
	if s.Base != "" {
		return "Scope: " + s.Scope + ", the changes from " + s.Base + " to " + s.Head + ".\n" +
			"git diff --no-ext-diff --no-textconv " + s.Base + " " + s.Head + " --"
	}
	return "Scope: Uncommitted changes, staged, unstaged and untracked, against HEAD " + s.Head + ".\n" +
		"git status, git diff --cached, git diff, and the untracked files themselves."
}
