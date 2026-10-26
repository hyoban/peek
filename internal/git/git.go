// Package git reads a repository's history, changes and files through the
// git command, and switches, fetches, pulls and pushes its branches, with
// gh for its pull requests.
package git

import (
	"bytes"
	"cmp"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Commit is one entry of the history column.
type Commit struct {
	Hash    string
	Short   string
	Subject string
	Body    string // the message after the subject
	Author  string
	Email   string
	// CoAuthors are the people the message's Co-authored-by trailers
	// name, as GitHub credits them, less the author.
	CoAuthors []Person
	Time      time.Time
	Summary   string // what the work tree's entry says of its changes
}

// Worktree is the hash of the entry for the changes not committed yet:
// those of the index and the work tree, against HEAD.
const Worktree = ""

// Range is the hash of the entry for a branch's changes since it left the
// branch it is to merge into: those from base, where it left, to head, as
// a pull request's diff has them.
func Range(base, head string) string { return base + ".." + head }

// SplitRange returns the base and head of a Range's hash.
func SplitRange(hash string) (base, head string, ok bool) { return strings.Cut(hash, "..") }

// IsRange reports whether the hash is a Range's.
func IsRange(hash string) bool {
	_, _, ok := SplitRange(hash)
	return ok
}

// Tip is the commit whose files the entry has: a Range's head, else the
// hash itself.
func Tip(hash string) string {
	if _, head, ok := SplitRange(hash); ok {
		return head
	}
	return hash
}

// Change is a file a commit touches, as git's --name-status prints it.
type Change struct {
	Status string // A, M, D, R, C, T
	Old    string // the path before a rename or copy, else Path
	Path   string
	// Added and Deleted are the lines the change adds and deletes, as
	// git's --numstat counts them, none for a binary file.
	Added, Deleted int
}

// Shape is the change less its line counts: the file and how it changed,
// which an edit to an already changed file keeps.
func (ch Change) Shape() Change {
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

// RepoRoot returns the top of the work tree that holds dir.
func RepoRoot(dir string) (string, error) {
	out, err := git(dir, "rev-parse", "--show-toplevel")
	return strings.TrimSpace(out), err
}

// CurrentBranch returns the branch checked out, or the short hash of a
// detached HEAD.
func CurrentBranch(repo string) string {
	// The ref in full, less refs/heads/: --abbrev-ref and --short make
	// heads/v1 of a branch a tag shares its name with.
	if out, err := git(repo, "symbolic-ref", "--quiet", "HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "refs/heads/")
	}
	out, _ := git(repo, "rev-parse", "--short", "HEAD")
	return strings.TrimSpace(out)
}

func LoadCommits(repo string, limit int) ([]Commit, error) {
	// The body spans lines, so the records end with a separator of
	// their own.
	const sep, end, each = "\x1f", "\x1e", "\x1d"
	out, err := git(repo, "log", fmt.Sprintf("-n%d", limit),
		"--format=%H"+sep+"%h"+sep+"%s"+sep+"%an"+sep+"%ae"+sep+"%at"+sep+
			"%(trailers:key=Co-authored-by,valueonly,unfold,separator=%x1d)"+sep+"%b"+end)
	if err != nil {
		return nil, err
	}
	var commits []Commit
	for record := range strings.SplitSeq(out, end) {
		f := strings.Split(strings.TrimLeft(record, "\n"), sep)
		if len(f) != 8 {
			continue
		}
		unix, _ := strconv.ParseInt(f[5], 10, 64)
		cm := Commit{Hash: f[0], Short: f[1], Subject: f[2], Author: f[3], Email: f[4], Time: time.Unix(unix, 0)}
		var credited []string // the emails of the trailers read
		for v := range strings.SplitSeq(f[6], each) {
			p, ok := parsePerson(v)
			if !ok {
				continue
			}
			credited = append(credited, p.Email)
			if !strings.EqualFold(p.Email, cm.Email) &&
				!slices.ContainsFunc(cm.CoAuthors, func(q Person) bool { return strings.EqualFold(q.Email, p.Email) }) {
				cm.CoAuthors = append(cm.CoAuthors, p)
			}
		}
		cm.Body = withoutCoAuthors(f[7], credited)
		commits = append(commits, cm)
	}
	return commits, nil
}

// Person is someone a commit credits, by name and email.
type Person struct{ Name, Email string }

// parsePerson reads "Name <email>", as trailers name people.
func parsePerson(s string) (Person, bool) {
	s = strings.TrimSpace(s)
	lt, gt := strings.LastIndex(s, "<"), strings.LastIndex(s, ">")
	if lt < 0 || gt < lt {
		return Person{}, false
	}
	p := Person{Name: strings.TrimSpace(s[:lt]), Email: strings.TrimSpace(s[lt+1 : gt])}
	if p.Email == "" {
		return Person{}, false
	}
	if p.Name == "" {
		p.Name = p.Email
	}
	return p, true
}

// withoutCoAuthors returns the body less the Co-authored-by trailers git
// read, which show as people instead. Only the last paragraph holds
// trailers: a line like them further up, as a squash's messages keep,
// is text, and stays.
func withoutCoAuthors(body string, credited []string) string {
	body = strings.TrimSpace(body)
	i := strings.LastIndex(body, "\n\n") // -1 for one paragraph
	head, last := body[:i+1], body[i+1:]
	var b strings.Builder
	for line := range strings.Lines(last) {
		if k, v, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(k), "Co-authored-by") {
			if p, ok := parsePerson(v); ok && slices.ContainsFunc(credited, func(e string) bool { return strings.EqualFold(e, p.Email) }) {
				continue
			}
		}
		b.WriteString(line)
	}
	return strings.TrimSpace(head + b.String())
}

func LoadChanges(repo, hash string) ([]Change, error) {
	if hash == Worktree {
		return LoadWorktreeChanges(repo)
	}
	if base, head, ok := SplitRange(hash); ok {
		return countedChanges(repo, "diff", "-z", "-M", base, head)
	}
	return countedChanges(repo, "show", "--format=", "-z", "-M", "--root", hash)
}

// countedChanges runs the git command, as show or diff, for the files it
// lists and again, at once, for their line counts.
func countedChanges(repo string, cmd ...string) ([]Change, error) {
	with := func(flag string) (string, error) {
		return git(repo, append([]string{cmd[0], flag}, cmd[1:]...)...)
	}
	var stats string
	var statsErr error
	var wg sync.WaitGroup
	wg.Go(func() { stats, statsErr = with("--numstat") })
	out, err := with("--name-status")
	wg.Wait()
	if err := cmp.Or(err, statsErr); err != nil {
		return nil, err
	}
	changes := parseNameStatus(out)
	addCounts(changes, parseNumstat(stats))
	return changes, nil
}

// LoadWorktreeChanges returns the files that differ from HEAD, those git
// does not track included.
func LoadWorktreeChanges(repo string) ([]Change, error) {
	// All of git's lists here end their paths with NULs, which leave
	// them as they are rather than quoted, for the counts to match them.
	var untracked string
	var untrackedErr error
	var wg sync.WaitGroup
	wg.Go(func() { untracked, untrackedErr = git(repo, "ls-files", "-z", "--others", "--exclude-standard") })
	changes, err := countedChanges(repo, "diff", "HEAD", "-z", "-M")
	wg.Wait()
	if err := cmp.Or(err, untrackedErr); err != nil {
		return nil, err
	}
	paths := nulSeparated(untracked)
	// Counts that fail leave the files listed, with none.
	added, _ := untrackedCounts(repo, paths)
	for _, p := range paths {
		changes = append(changes, Change{Status: "A", Old: p, Path: p, Added: added[p][0]})
	}
	return changes, nil
}

// CountChanges counts the files that differ from HEAD, those git does not
// track included, as LoadWorktreeChanges lists them, without their lines.
func CountChanges(repo string) (int, error) {
	out, err := git(repo, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return 0, err
	}
	return len(nulSeparated(out)), nil
}

// untrackedCounts counts the lines of files git does not track as git
// would once they were added, its attributes, filters, encodings and
// big files' threshold all as they are: in an index of their own, made
// for this and thrown away, they are only to be added, which leaves the
// repository's index and its objects as they are, and git diffs them.
// The files go in batches, at once, as git matches each file it adds
// against every path it is given, which slows with thousands of them.
func untrackedCounts(repo string, paths []string) (map[string][2]int, error) {
	const batch = 1000
	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		firstErr error
	)
	counts := map[string][2]int{}
	for chunk := range slices.Chunk(paths, batch) {
		wg.Go(func() {
			n, err := untrackedBatch(repo, chunk)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				firstErr = cmp.Or(firstErr, err)
				return
			}
			maps.Copy(counts, n)
		})
	}
	wg.Wait()
	return counts, firstErr
}

// untrackedBatch counts the lines of a batch of untracked files, as
// untrackedCounts does.
func untrackedBatch(repo string, paths []string) (map[string][2]int, error) {
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

func addCounts(changes []Change, counts map[string][2]int) {
	for i := range changes {
		n := counts[changes[i].Path]
		changes[i].Added, changes[i].Deleted = n[0], n[1]
	}
}

// parseNameStatus reads git's --name-status -z: a status, then its path,
// or a rename's or a copy's old path and new.
func parseNameStatus(out string) []Change {
	var changes []Change
	f := strings.Split(out, "\x00")
	for i := 0; i+1 < len(f); i++ {
		status := f[i]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R', 'C': // the old path, then the new
			if i+2 < len(f) {
				changes = append(changes, Change{Status: status[:1], Old: f[i+1], Path: f[i+2]})
			}
			i += 2
		default:
			changes = append(changes, Change{Status: status[:1], Old: f[i+1], Path: f[i+1]})
			i++
		}
	}
	return changes
}

// FileTexts returns the text of a file a change touches before and after
// the commit, the work tree's on disk for the uncommitted changes, and at
// the base and the head of a Range.
func FileTexts(repo, hash string, ch Change) (before, after string) {
	before, after, _ = FileTextsAtMost(repo, hash, ch, 0)
	return before, after
}

// FileTextsAtMost is FileTexts, both read at once, or ErrTooLarge where
// either is longer than limit bytes; 0 for no limit.
func FileTextsAtMost(repo, hash string, ch Change, limit int) (before, after string, err error) {
	parent := hash + "^"
	if hash == Worktree {
		parent = "HEAD"
	}
	if base, head, ok := SplitRange(hash); ok {
		parent, hash = base, head
	}
	var (
		wg                  sync.WaitGroup
		errBefore, errAfter error
	)
	if ch.Status != "A" {
		wg.Go(func() { before, errBefore = FileTextAtMost(repo, parent, ch.Old, limit) })
	}
	if ch.Status != "D" {
		wg.Go(func() { after, errAfter = FileTextAtMost(repo, hash, ch.Path, limit) })
	}
	wg.Wait()
	if errors.Is(errBefore, ErrTooLarge) || errors.Is(errAfter, ErrTooLarge) {
		return "", "", ErrTooLarge
	}
	return before, after, nil
}

// FileTextAtMost is FileText, or ErrTooLarge where the text is longer than
// limit bytes, not read whole only to be left; 0 for no limit.
func FileTextAtMost(repo, hash, p string, limit int) (string, error) {
	if limit <= 0 {
		return FileText(repo, hash, p)
	}
	if hash == Worktree {
		if fi, err := os.Stat(filepath.Join(repo, p)); err == nil && fi.Size() > int64(limit) {
			return "", ErrTooLarge
		}
		return FileText(repo, hash, p)
	}
	return Bounded(repo, limit, "show", Tip(hash)+":"+p)
}

// FileText returns the file's text in the commit, or on disk.
func FileText(repo, hash, p string) (string, error) {
	if hash == Worktree {
		b, err := os.ReadFile(filepath.Join(repo, p))
		return string(b), err
	}
	return git(repo, "show", Tip(hash)+":"+p)
}

// Patch returns git's patch of the change the commit makes to a file, or
// the work tree's against HEAD, or a Range's, of limit bytes at most: ErrTooLarge where
// it is longer. A file git does not track has none.
func Patch(repo, hash string, ch Change, limit int) (string, error) {
	args := []string{"show", "--format=", "-M", "--root", hash}
	if hash == Worktree {
		args = []string{"diff", "-M", "HEAD"}
	}
	if base, head, ok := SplitRange(hash); ok {
		args = []string{"diff", "-M", base, head}
	}
	paths := []string{ch.Path}
	if ch.Old != ch.Path {
		paths = append(paths, ch.Old)
	}
	// a/ and b/ whatever the user's diff.noprefix or diff.mnemonicPrefix.
	args = append(args, "--no-ext-diff", "--no-color", "--src-prefix=a/", "--dst-prefix=b/", "--")
	return Bounded(repo, limit, append(args, paths...)...)
}

// ListFiles returns every file of the commit, a Range's head's, or of the
// work tree: the files git tracks and those it does not ignore, less those
// deleted.
func ListFiles(repo, hash string) ([]string, error) {
	if hash != Worktree {
		out, err := git(repo, "ls-tree", "-r", "-z", "--name-only", Tip(hash))
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
