package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// commit is one entry of the history column.
type commit struct {
	Hash    string
	Short   string
	Subject string
	Author  string
	Email   string
	Time    time.Time
	Summary string // what the work tree's entry says of its changes
}

// worktree is the hash of the entry for the changes not committed yet:
// those of the index and the work tree, against HEAD.
const worktree = ""

// change is a file a commit touches, as git's --name-status prints it.
type change struct {
	Status string // A, M, D, R, C, T
	Old    string // the path before a rename or copy, else Path
	Path   string
}

// gitSlots limits the git processes running at once, as when a scroll
// brings the diffs of many files into view.
var gitSlots = make(chan struct{}, 8)

func git(repo string, args ...string) (string, error) {
	gitSlots <- struct{}{}
	defer func() { <-gitSlots }()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	// Reads alone, which leave the index as it is for the watch.
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(errOut.String()))
	}
	return out.String(), nil
}

// repoRoot returns the top of the work tree that holds dir.
func repoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// currentBranch returns the branch checked out, or the short hash of a
// detached HEAD.
func currentBranch(repo string) string {
	out, err := git(repo, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return ""
	}
	if b := strings.TrimSpace(out); b != "HEAD" {
		return b
	}
	out, _ = git(repo, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(out)
}

func loadCommits(repo string, limit int) ([]commit, error) {
	const sep = "\x1f"
	out, err := git(repo, "log", fmt.Sprintf("-n%d", limit),
		"--format=%H"+sep+"%h"+sep+"%s"+sep+"%an"+sep+"%ae"+sep+"%at")
	if err != nil {
		return nil, err
	}
	var commits []commit
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\n"), sep)
		if len(f) != 6 {
			continue
		}
		unix, _ := strconv.ParseInt(f[5], 10, 64)
		commits = append(commits, commit{Hash: f[0], Short: f[1], Subject: f[2], Author: f[3], Email: f[4], Time: time.Unix(unix, 0)})
	}
	return commits, nil
}

func loadChanges(repo, hash string) ([]change, error) {
	if hash == worktree {
		return loadWorktreeChanges(repo)
	}
	out, err := git(repo, "show", "--format=", "--name-status", "-M", "--root", hash)
	if err != nil {
		return nil, err
	}
	return parseNameStatus(out), nil
}

// loadWorktreeChanges returns the files that differ from HEAD, those git
// does not track included.
func loadWorktreeChanges(repo string) ([]change, error) {
	out, err := git(repo, "diff", "HEAD", "--name-status", "-M")
	if err != nil {
		return nil, err
	}
	changes := parseNameStatus(out)
	untracked, err := git(repo, "ls-files", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	for line := range strings.Lines(untracked) {
		p := strings.TrimRight(line, "\n")
		changes = append(changes, change{Status: "A", Old: p, Path: p})
	}
	return changes, nil
}

func parseNameStatus(out string) []change {
	var changes []change
	for line := range strings.Lines(out) {
		f := strings.Split(strings.TrimRight(line, "\n"), "\t")
		switch {
		case len(f) == 2:
			changes = append(changes, change{Status: f[0][:1], Old: f[1], Path: f[1]})
		case len(f) == 3:
			changes = append(changes, change{Status: f[0][:1], Old: f[1], Path: f[2]})
		}
	}
	return changes
}

// fileTexts returns the text of a file a change touches before and after
// the commit, the work tree's on disk for the uncommitted changes.
func fileTexts(repo, hash string, ch change) (before, after string) {
	parent := hash + "^"
	if hash == worktree {
		parent = "HEAD"
	}
	if ch.Status != "A" {
		before, _ = fileText(repo, parent, ch.Old)
	}
	if ch.Status != "D" {
		after, _ = fileText(repo, hash, ch.Path)
	}
	return before, after
}

// fileText returns the file's text in the commit, or on disk.
func fileText(repo, hash, p string) (string, error) {
	if hash == worktree {
		b, err := os.ReadFile(filepath.Join(repo, p))
		return string(b), err
	}
	return git(repo, "show", hash+":"+p)
}

// listFiles returns every file of the commit, or of the work tree: the
// files git tracks and those it does not ignore, less those deleted.
func listFiles(repo, hash string) ([]string, error) {
	if hash != worktree {
		out, err := git(repo, "ls-tree", "-r", "-z", "--name-only", hash)
		return nulSeparated(out), err
	}
	out, err := git(repo, "ls-files", "-z", "--cached", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	deleted, err := git(repo, "ls-files", "-z", "--deleted")
	if err != nil {
		return nil, err
	}
	gone := map[string]bool{}
	for _, p := range nulSeparated(deleted) {
		gone[p] = true
	}
	var files []string
	for _, p := range nulSeparated(out) {
		if !gone[p] {
			files = append(files, p)
		}
	}
	return files, nil
}

func nulSeparated(out string) []string {
	return strings.FieldsFunc(out, func(r rune) bool { return r == 0 })
}
