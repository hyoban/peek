// Package review has Codex review a repository's uncommitted changes, its
// commits not pushed yet, or its branch's changes, and tells whether a
// result is out of date; and has it explain those changes or a commit's.
package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"peek/internal/git"
	"strings"
	"sync"
)

// Git metadata is bounded; file contents are hashed as streams, never sent
// through the prompt or retained in the snapshot.
const maxMetadata = 1024 * 1024

type Snapshot struct {
	ID, Scope, Base, Head string
}

func ReadSnapshot(repo string) (Snapshot, error) {
	return ReadSnapshotWith(repo, func() bool { return git.HasUnpushedCommits(repo) })
}

// ReadSnapshotWith is ReadSnapshot, asking unpushed whether the branch has
// commits it has not pushed, as one read of it for others too answers.
func ReadSnapshotWith(repo string, unpushed func() bool) (Snapshot, error) {
	g := func(args ...string) (string, error) { return git.Bounded(repo, maxMetadata, args...) }
	// Read at once: one after another, the processes added up.
	var (
		wg           sync.WaitGroup
		status, head string
		branch       string
		err          error
	)
	wg.Go(func() {
		status, err = g("status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	})
	wg.Go(func() { head, _ = g("rev-parse", "--verify", "HEAD") })
	wg.Go(func() { branch, _ = g("symbolic-ref", "--quiet", "HEAD") })
	wg.Wait()
	if err != nil {
		return Snapshot{}, err
	}
	if err := rejectDirtySubmodules(status); err != nil {
		return Snapshot{}, err
	}
	snapshot := Snapshot{Scope: "Uncommitted changes", Head: strings.TrimSpace(head)}
	fingerprint := sha256.New()
	fmt.Fprintf(fingerprint, "%s\x00%s\x00%s\x00", head, branch, status)
	if status != "" {
		if err := hashFiles(repo, status, fingerprint); err != nil {
			return Snapshot{}, err
		}
	} else {
		if snapshot.Head == "" || !unpushed() {
			return Snapshot{}, nil
		}
		trimGit := func(args ...string) (string, error) {
			out, err := g(args...)
			return strings.TrimSpace(out), err
		}
		remote, target, err := git.PushDestination(trimGit, strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/"))
		if err != nil {
			return Snapshot{}, err
		}
		fetchURL, _ := trimGit("remote", "get-url", remote)
		pushURL, _ := trimGit("remote", "get-url", "--push", remote)
		if fetchURL != pushURL {
			return Snapshot{}, fmt.Errorf("cannot determine pending commits when fetch and push URLs differ")
		}
		for _, ref := range []string{"refs/remotes/" + remote + "/" + target, "refs/remotes/" + remote + "/HEAD", "refs/remotes/" + remote + "/main", "refs/remotes/" + remote + "/master"} {
			if oid, err := trimGit("rev-parse", "--verify", ref+"^{commit}"); err == nil {
				snapshot.Base, err = trimGit("merge-base", oid, snapshot.Head)
				if err != nil {
					return Snapshot{}, fmt.Errorf("cannot find a common ancestor for the review: %w", err)
				}
				break
			}
		}
		if snapshot.Base == "" {
			snapshot.Base, err = trimGit("hash-object", "-t", "tree", "/dev/null")
			if err != nil {
				return Snapshot{}, err
			}
		}
		// Exit 1 means differences. No patch is generated or buffered here.
		_, err = g("diff", "--quiet", "--no-ext-diff", "--no-textconv", "--ignore-submodules=none", snapshot.Base, snapshot.Head, "--")
		if err == nil {
			return Snapshot{}, nil
		}
		var exit *exec.ExitError
		if !errors.As(err, &exit) || exit.ExitCode() != 1 {
			return Snapshot{}, err
		}
		snapshot.Scope = "Unpushed commits"
		fmt.Fprintf(fingerprint, "%s\x00%s", snapshot.Base, snapshot.Head)
	}
	snapshot.ID = fmt.Sprintf("%x", fingerprint.Sum(nil))
	return snapshot, nil
}

// BranchScope is the scope of a review of the branch's changes since it
// left the branch it is to merge into, as its pull request's diff has them.
const BranchScope = "Branch changes"

// BranchSnapshot is the snapshot of the branch's changes in the range,
// which only its commits move: uncommitted changes leave it be.
func BranchSnapshot(branch string, r git.BranchRange) Snapshot {
	id := sha256.Sum256([]byte(BranchScope + "\x00" + branch + "\x00" + r.Base + "\x00" + r.Head))
	return Snapshot{ID: fmt.Sprintf("%x", id), Scope: BranchScope, Base: r.Base, Head: r.Head}
}

// ReadBranchSnapshot reads the snapshot of the branch's changes, none
// where the branch has no commits of its own.
func ReadBranchSnapshot(repo string) (Snapshot, error) {
	r, ok := git.BranchBase(repo)
	if !ok {
		return Snapshot{}, nil
	}
	return BranchSnapshot(git.CurrentBranch(repo), r), nil
}

type Finding struct {
	Priority int    `json:"priority"`
	Title    string `json:"title"`
	Body     string `json:"body"`
	Path     string `json:"path"`
	Line     int    `json:"line"`
}

type Result struct {
	// Status is what Codex says of its review: complete, or inconclusive
	// where it could not finish it.
	Status   string    `json:"status"`
	Summary  string    `json:"summary"`
	Findings []Finding `json:"findings"`
	// Verdict is passed or failed, as validate judges by the findings'
	// priorities rather than as Codex would.
	Verdict string `json:"-"`
}

// DefaultBlocking is the lowest priority a finding fails the review at
// unless the user chooses another (Judge): those of P3, minor, show with
// a passed one.
const DefaultBlocking = 2

// Judge gives the review its verdict: failed where a finding is of P0 to
// P<blocking>, else passed.
func (r *Result) Judge(blocking int) {
	r.Verdict = "passed"
	for _, f := range r.Findings {
		if f.Priority <= blocking {
			r.Verdict = "failed"
		}
	}
}

func (r Result) Text() string {
	var text strings.Builder
	text.WriteString(r.Summary)
	for _, f := range r.Findings {
		fmt.Fprintf(&text, "\n\n[P%d] %s\n%s:%d\n%s", f.Priority, f.Title, f.Path, f.Line, f.Body)
	}
	return text.String()
}

// validate checks the review Codex returned and judges its verdict at
// DefaultBlocking.
func (r *Result) validate() error {
	if strings.TrimSpace(r.Summary) == "" || r.Findings == nil {
		return fmt.Errorf("Codex returned an incomplete review")
	}
	switch r.Status {
	case "complete":
	case "inconclusive":
		return fmt.Errorf("review inconclusive: %s", r.Summary)
	default:
		return fmt.Errorf("Codex returned an invalid review status")
	}
	for _, f := range r.Findings {
		if f.Priority < 0 || f.Priority > 3 || f.Line < 1 || strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" || strings.TrimSpace(f.Path) == "" {
			return fmt.Errorf("Codex returned an incomplete review finding")
		}
	}
	r.Judge(DefaultBlocking)
	return nil
}

var ErrChanged = errors.New("changes changed during review; review again")

type Runner func(context.Context, string, Snapshot) (Result, error)

func Run(ctx context.Context, repo string, snapshot Snapshot, run Runner) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if snapshot.ID == "" {
		return Result{}, errors.New("nothing to review")
	}
	read := ReadSnapshot
	if snapshot.Scope == BranchScope {
		read = ReadBranchSnapshot
	}
	current, err := read(repo)
	if err != nil {
		return Result{}, err
	}
	if current.ID != snapshot.ID {
		return Result{}, ErrChanged
	}
	result, err := run(ctx, repo, snapshot)
	if err != nil {
		return Result{}, err
	}
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	current, err = read(repo)
	if err != nil {
		return Result{}, err
	}
	if current.ID != snapshot.ID {
		return Result{}, ErrChanged
	}
	err = result.validate()
	return result, err
}
