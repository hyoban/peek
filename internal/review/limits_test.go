package review

import (
	"crypto/rand"
	"os"
	"path/filepath"
	"peek/internal/testutil"
	"strings"
	"testing"
)

func TestReviewLargeFiles(t *testing.T) {
	for _, scenario := range []string{"unstaged", "staged", "binary", "untracked", "combined", "unpushed"} {
		t.Run(scenario, func(t *testing.T) {
			repo, _ := testutil.RemoteRepo(t)
			name := "main.go"
			data := []byte(strings.Repeat("a line\n", maxMetadata/7+1))
			if scenario == "binary" {
				data = make([]byte, 2*maxMetadata)
				if _, err := rand.Read(data); err != nil {
					t.Fatal(err)
				}
				data[0] = 0
			}
			if scenario == "untracked" {
				name = "large.txt"
			}
			if scenario == "combined" {
				data = []byte(strings.Repeat("staged\n", 60000))
			}
			if err := os.WriteFile(filepath.Join(repo, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
			if scenario == "staged" || scenario == "combined" || scenario == "unpushed" {
				testutil.Git(t, repo, "add", "--all")
			}
			if scenario == "combined" {
				if err := os.WriteFile(filepath.Join(repo, name), []byte(strings.Repeat("working\n", 60000)), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "unpushed" {
				testutil.Git(t, repo, "commit", "-m", "Add the large file")
			}
			snapshot, err := ReadSnapshot(repo)
			if err != nil || snapshot.ID == "" {
				t.Fatalf("could not fingerprint large %s changes: id=%q, err=%v", scenario, snapshot.ID, err)
			}
		})
	}
}
