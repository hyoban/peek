package main

import (
	"context"
	"os"
	"strings"
	"testing"
)

// Opt in to a tiny real CLI request; the regular tests use fakeCodex and
// local bare remotes, so they need neither a login nor network access.
func TestCodexSmoke(t *testing.T) {
	if os.Getenv("PEEK_CODEX_SMOKE") != "1" {
		t.Skip("set PEEK_CODEX_SMOKE=1 to check the installed Codex CLI")
	}
	message, err := codexCommitMessage(context.Background(),
		"diff --git a/hello.go b/hello.go\n--- a/hello.go\n+++ b/hello.go\n@@ -1 +1 @@\n-fmt.Println(\"helo\")\n+fmt.Println(\"hello\")\n",
		"Fix the welcome message\nShow the current branch")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(message) == "" {
		t.Fatal("empty message")
	}
	t.Log(message)
}
