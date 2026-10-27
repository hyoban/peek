package codex

import (
	"context"
	"os"
	"path/filepath"
	"peek/internal/git"
	"peek/internal/testutil"
	"strings"
	"testing"
)

func TestCommitConvention(t *testing.T) {
	agents := "# Guide\n\nIntro.\n\n## Commits\n\nWrite them as [Conventional Commits](https://www.conventionalcommits.org):\n`feat` under Features.\n\n## Other\n\nUnrelated.\n"
	for _, tc := range []struct {
		name     string
		files    map[string]string
		subjects []string
		source   string // "" for no convention
		note     string
	}{
		{name: "none", subjects: []string{"Add a thing", "Fix a thing", "feat: one"}},
		{name: "agents note", files: map[string]string{"AGENTS.md": agents}, source: "AGENTS.md", note: "## Commits\n\nWrite them"},
		{name: "commitlint", files: map[string]string{"commitlint.config.js": "export default {}\n"}, source: "commitlint.config.js"},
		{name: "package.json", files: map[string]string{"package.json": `{"devDependencies":{"@commitlint/cli":"1"}}`}, source: "package.json"},
		{name: "history", subjects: []string{"feat(ui): add a thing", "fix: a thing", "Something else", "chore!: drop it"}, source: "its recent subjects"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := testutil.Repo(t)
			for _, s := range tc.subjects {
				testutil.Git(t, repo, "commit", "-q", "--allow-empty", "-m", s)
			}
			for name, text := range tc.files {
				if err := os.WriteFile(filepath.Join(repo, name), []byte(text), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			testutil.Git(t, repo, "add", "-A")
			snapshot := git.Snapshot{Head: testutil.Git(t, repo, "rev-parse", "HEAD"), Tree: testutil.Git(t, repo, "write-tree")}
			got := commitConvention(context.Background(), repo, snapshot)
			if tc.source == "" {
				if got != "" {
					t.Fatalf("convention found in %q", got)
				}
				return
			}
			if !strings.Contains(got, "Conventional Commits (found in "+tc.source+")") || !strings.Contains(got, tc.note) {
				t.Fatalf("convention %q", got)
			}
			if strings.Contains(got, "Unrelated") || strings.Contains(got, "Intro") {
				t.Fatalf("the note quotes more than its section: %q", got)
			}
		})
	}
}

func TestConventionPart(t *testing.T) {
	if got := conventionPart("One.\n\nUse Conventional Commits,\nalways.\n\nTwo.\n"); got != "Use Conventional Commits,\nalways." {
		t.Errorf("paragraph %q", got)
	}
	if got := conventionPart("# Notes\n\nNothing of commits.\n"); got != "" {
		t.Errorf("part %q of a note without the convention", got)
	}
}
