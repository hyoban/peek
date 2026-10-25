// Package review has Codex review a repository's uncommitted changes, or
// its commits not pushed yet, and tells whether a result is out of date.
package review

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os/exec"
	"peek/internal/git"
	"strings"
)

// Git metadata is bounded; file contents are hashed as streams, never sent
// through the prompt or retained in the snapshot.
const maxMetadata = 1024 * 1024

type Snapshot struct {
	ID, Scope, Base, Head string
}

func ReadSnapshot(repo string) (Snapshot, error) {
	g := func(args ...string) (string, error) { return git.Bounded(repo, maxMetadata, args...) }
	status, err := g("status", "--porcelain=v2", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return Snapshot{}, err
	}
	if err := rejectDirtySubmodules(status); err != nil {
		return Snapshot{}, err
	}
	head, _ := g("rev-parse", "--verify", "HEAD")
	branch, _ := g("symbolic-ref", "--quiet", "HEAD")
	snapshot := Snapshot{Scope: "Uncommitted changes", Head: strings.TrimSpace(head)}
	fingerprint := sha256.New()
	fmt.Fprintf(fingerprint, "%s\x00%s\x00%s\x00", head, branch, status)
	if status != "" {
		if err := hashFiles(repo, status, fingerprint); err != nil {
			return Snapshot{}, err
		}
	} else {
		if snapshot.Head == "" || !git.HasUnpushedCommits(repo) {
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

// blockingPriority is the lowest priority a finding fails the review at:
// those of P3, minor, show with a passed one.
const blockingPriority = 2

func (r Result) Text() string {
	var text strings.Builder
	text.WriteString(r.Summary)
	for _, f := range r.Findings {
		fmt.Fprintf(&text, "\n\n[P%d] %s\n%s:%d\n%s", f.Priority, f.Title, f.Path, f.Line, f.Body)
	}
	return text.String()
}

// validate checks the review Codex returned and judges its verdict: failed
// where a finding is of P0 to P2, else passed.
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
	r.Verdict = "passed"
	for _, f := range r.Findings {
		if f.Priority < 0 || f.Priority > 3 || f.Line < 1 || strings.TrimSpace(f.Title) == "" || strings.TrimSpace(f.Body) == "" || strings.TrimSpace(f.Path) == "" {
			return fmt.Errorf("Codex returned an incomplete review finding")
		}
		if f.Priority <= blockingPriority {
			r.Verdict = "failed"
		}
	}
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
	current, err := ReadSnapshot(repo)
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
	current, err = ReadSnapshot(repo)
	if err != nil {
		return Result{}, err
	}
	if current.ID != snapshot.ID {
		return Result{}, ErrChanged
	}
	err = result.validate()
	return result, err
}
