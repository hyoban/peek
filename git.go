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
	Body    string // the message after the subject
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
	// Added and Deleted are the lines the change adds and deletes, as
	// git's --numstat counts them, none for a binary file.
	Added, Deleted int
}

// shape is the change less its line counts: the file and how it changed,
// which an edit to an already changed file keeps.
func (ch change) shape() change {
	ch.Added, ch.Deleted = 0, 0
	return ch
}

// gitSlots limits the git processes running at once, as when a scroll
// brings the diffs of many files into view.
var gitSlots = make(chan struct{}, 8)

func git(repo string, args ...string) (string, error) {
	return gitWith(repo, nil, "", args...)
}

// gitWith runs git with these variables more in its environment and this
// input on its standard input.
func gitWith(repo string, env []string, input string, args ...string) (string, error) {
	gitSlots <- struct{}{}
	defer func() { <-gitSlots }()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	cmd.Stdin = strings.NewReader(input)
	// Reads alone, which leave the index as it is for the watch.
	cmd.Env = append(append(os.Environ(), "GIT_OPTIONAL_LOCKS=0"), env...)
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
	// The ref in full, less refs/heads/: --abbrev-ref and --short make
	// heads/v1 of a branch a tag shares its name with.
	if out, err := git(repo, "symbolic-ref", "--quiet", "HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/")
	}
	out, _ := git(repo, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(out)
}

func loadCommits(repo string, limit int) ([]commit, error) {
	// The body spans lines, so the records end with a separator of
	// their own.
	const sep, end = "\x1f", "\x1e"
	out, err := git(repo, "log", fmt.Sprintf("-n%d", limit),
		"--format=%H"+sep+"%h"+sep+"%s"+sep+"%an"+sep+"%ae"+sep+"%at"+sep+"%b"+end)
	if err != nil {
		return nil, err
	}
	var commits []commit
	for record := range strings.SplitSeq(out, end) {
		f := strings.Split(strings.TrimLeft(record, "\n"), sep)
		if len(f) != 7 {
			continue
		}
		unix, _ := strconv.ParseInt(f[5], 10, 64)
		commits = append(commits, commit{Hash: f[0], Short: f[1], Subject: f[2], Body: strings.TrimSpace(f[6]),
			Author: f[3], Email: f[4], Time: time.Unix(unix, 0)})
	}
	return commits, nil
}

func loadChanges(repo, hash string) ([]change, error) {
	if hash == worktree {
		return loadWorktreeChanges(repo)
	}
	out, err := git(repo, "show", "--format=", "--name-status", "-z", "-M", "--root", hash)
	if err != nil {
		return nil, err
	}
	changes := parseNameStatus(out)
	stats, err := git(repo, "show", "--format=", "--numstat", "-z", "-M", "--root", hash)
	if err != nil {
		return nil, err
	}
	addCounts(changes, parseNumstat(stats))
	return changes, nil
}

// loadWorktreeChanges returns the files that differ from HEAD, those git
// does not track included.
func loadWorktreeChanges(repo string) ([]change, error) {
	out, err := git(repo, "diff", "HEAD", "--name-status", "-z", "-M")
	if err != nil {
		return nil, err
	}
	changes := parseNameStatus(out)
	stats, err := git(repo, "diff", "HEAD", "--numstat", "-z", "-M")
	if err != nil {
		return nil, err
	}
	addCounts(changes, parseNumstat(stats))
	// All of git's lists here end their paths with NULs, which leave
	// them as they are rather than quoted, for the counts to match them.
	untracked, err := git(repo, "ls-files", "-z", "--others", "--exclude-standard")
	if err != nil {
		return nil, err
	}
	paths := nulSeparated(untracked)
	// Counts that fail leave the files listed, with none.
	added, _ := untrackedCounts(repo, paths)
	for _, p := range paths {
		changes = append(changes, change{Status: "A", Old: p, Path: p, Added: added[p][0]})
	}
	return changes, nil
}

// untrackedCounts counts the lines of files git does not track as git
// would once they were added, its attributes, filters, encodings and
// big files' threshold all as they are: in an index of their own, made
// for this and thrown away, they are only to be added, which leaves the
// repository's index and its objects as they are, and git diffs them.
func untrackedCounts(repo string, paths []string) (map[string][2]int, error) {
	if len(paths) == 0 {
		return nil, nil
	}
	dir, err := os.MkdirTemp("", "peek-index")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)
	// The paths as they are, not patterns nor :magic, and those outside
	// a sparse checkout's too.
	env := []string{"GIT_INDEX_FILE=" + filepath.Join(dir, "index"), "GIT_LITERAL_PATHSPECS=1"}
	// Nothing of the repository's own runs or is written: no hook, as
	// post-index-change, is told of this index, and none of it is split
	// into the shared index files git would keep in the repository.
	isolated := []string{"-c", "core.hooksPath=" + os.DevNull, "-c", "core.splitIndex=false"}
	input := strings.Join(paths, "\x00") + "\x00"
	add := append(isolated, "add", "--intent-to-add", "--sparse", "--pathspec-from-file=-", "--pathspec-file-nul")
	if _, err := gitWith(repo, env, input, add...); err != nil {
		return nil, err
	}
	out, err := gitWith(repo, env, "", append(isolated, "diff", "--numstat", "-z")...)
	if err != nil {
		return nil, err
	}
	return parseNumstat(out), nil
}

// parseNumstat reads git's --numstat -z: the lines added and deleted by
// new path, a rename's paths each ended by a NUL of their own.
func parseNumstat(out string) map[string][2]int {
	counts := map[string][2]int{}
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		f := strings.SplitN(fields[i], "\t", 3)
		if len(f) != 3 {
			continue
		}
		p := f[2]
		if p == "" && i+2 < len(fields) { // a rename or copy: old, then new
			p = fields[i+2]
			i += 2
		}
		added, _ := strconv.Atoi(f[0]) // "-" for a binary file
		deleted, _ := strconv.Atoi(f[1])
		counts[p] = [2]int{added, deleted}
	}
	return counts
}

func addCounts(changes []change, counts map[string][2]int) {
	for i := range changes {
		n := counts[changes[i].Path]
		changes[i].Added, changes[i].Deleted = n[0], n[1]
	}
}

// parseNameStatus reads git's --name-status -z: a status, then its path,
// or a rename's or a copy's old path and new.
func parseNameStatus(out string) []change {
	var changes []change
	f := strings.Split(out, "\x00")
	for i := 0; i+1 < len(f); i++ {
		status := f[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R', 'C': // the old path, then the new
			if i+2 < len(f) {
				changes = append(changes, change{Status: status[:1], Old: f[i+1], Path: f[i+2]})
			}
			i += 2
		default:
			changes = append(changes, change{Status: status[:1], Old: f[i+1], Path: f[i+1]})
			i++
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

// githubURL returns the GitHub page of the repository, from its origin's
// URL or else another remote's, "" where none is on GitHub.
func githubURL(repo string) string {
	out, err := git(repo, "config", "--get-regexp", `^remote\..*\.url$`)
	if err != nil {
		return ""
	}
	var found string
	for line := range strings.Lines(out) {
		key, url, _ := strings.Cut(strings.TrimSpace(line), " ")
		page := githubPage(url)
		if page == "" {
			continue
		}
		if key == "remote.origin.url" {
			return page
		}
		if found == "" {
			found = page
		}
	}
	return found
}

// githubPage returns the page of a GitHub remote's URL, as SSH, scp-like
// and HTTPS URLs name it, "" for another host's.
func githubPage(url string) string {
	rest, ok := "", false
	for _, prefix := range []string{"git@github.com:", "ssh://git@github.com/", "https://github.com/", "http://github.com/", "git://github.com/"} {
		if rest, ok = strings.CutPrefix(url, prefix); ok {
			break
		}
	}
	rest = strings.TrimSuffix(strings.TrimSuffix(rest, "/"), ".git")
	if !ok || strings.Count(rest, "/") != 1 || strings.HasPrefix(rest, "/") || strings.HasSuffix(rest, "/") {
		return ""
	}
	return "https://github.com/" + rest
}
