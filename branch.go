package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// branches are what the branch switcher lists: the local branches, the
// latest committed to first, and the remote ones no local branch of the
// same name tracks yet, as remote/name; and where the branches checked
// out in the repository's other work trees are, which git keeps from
// being checked out here too.
type branches struct {
	local, remote []string
	worktrees     map[string]string // by branch
}

func listBranches(ctx context.Context, repo string) (branches, error) {
	var b branches
	// lstrip=2 is the branch's name itself, where :short makes heads/v1 of
	// one a tag shares its name with, which git switch takes for no branch.
	out, err := pushGit(ctx, repo, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)%09%(worktreepath)", "refs/heads")
	if err != nil {
		return b, err
	}
	have := map[string]bool{}
	b.worktrees = map[string]string{}
	for line := range strings.Lines(out) {
		name, dir, _ := strings.Cut(strings.TrimRight(line, "\n"), "\t")
		if name == "" {
			continue
		}
		b.local = append(b.local, name)
		have[name] = true
		if dir != "" && !samePath(dir, repo) {
			b.worktrees[name] = dir
		}
	}
	// %(refname:lstrip=2) is the name itself, as above, and keeps the
	// remote's name, which :short drops from origin/HEAD.
	out, err = pushGit(ctx, repo, "for-each-ref", "--sort=-committerdate", "--format=%(refname:lstrip=2)", "refs/remotes")
	if err != nil {
		return b, err
	}
	for _, name := range strings.Fields(out) {
		_, branch, ok := strings.Cut(name, "/")
		if !ok || branch == "HEAD" || have[branch] {
			continue
		}
		have[branch] = true // the first remote's, where several have it
		b.remote = append(b.remote, name)
	}
	return b, nil
}

// samePath reports whether the paths name one folder, through symbolic
// links, as macOS's /tmp is /private/tmp.
func samePath(a, b string) bool {
	if ra, err := filepath.EvalSymlinks(a); err == nil {
		a = ra
	}
	if rb, err := filepath.EvalSymlinks(b); err == nil {
		b = rb
	}
	return filepath.Clean(a) == filepath.Clean(b)
}

// switchBranch checks out a local branch, or a remote one as a new local
// branch tracking it, carrying the work tree's changes along as git does,
// and refusing where they would be lost.
func switchBranch(ctx context.Context, repo, name string, remote bool) error {
	args := []string{"switch", name}
	if remote {
		// The ref in full, which no tag of the same name shadows.
		args = []string{"switch", "--track", "refs/remotes/" + name}
	}
	_, err := pushGit(ctx, repo, args...)
	return gitFailure(err)
}

// gitFailure is what git said of a failure, less the command and its
// exit status, for a note to read.
func gitFailure(err error) error {
	if err == nil {
		return nil
	}
	_, said, ok := strings.Cut(err.Error(), "\n")
	if said = strings.TrimSpace(said); !ok || said == "" {
		return err
	}
	return fmt.Errorf("%s", said)
}

// pullRequest is an open pull request of the repository, as gh lists it.
type pullRequest struct {
	Number int    `json:"number"`
	Title  string `json:"title"`
	Branch string `json:"headRefName"`
	Draft  bool   `json:"isDraft"`
	Author struct {
		Login string `json:"login"`
	} `json:"author"`
}

// Finder-launched apps do not inherit the terminal's PATH, as with Codex;
// extendPath adds the usual places, and these are looked in still.
func ghExecutable() (string, error) {
	if p, err := exec.LookPath("gh"); err == nil {
		return p, nil
	}
	for _, p := range []string{"/opt/homebrew/bin/gh", "/usr/local/bin/gh"} {
		if info, err := os.Stat(p); err == nil && !info.IsDir() && info.Mode()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("GitHub CLI was not found; install gh and run gh auth login")
}

func gh(ctx context.Context, repo string, args ...string) (string, error) {
	bin, err := ghExecutable()
	if err != nil {
		return "", err
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "GH_PROMPT_DISABLED=1", "GIT_TERMINAL_PROMPT=0", "NO_COLOR=1")
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		if said := strings.TrimSpace(stderr.String()); said != "" {
			return "", fmt.Errorf("%s", said)
		}
		return "", fmt.Errorf("gh %s: %w", strings.Join(args, " "), err)
	}
	return out.String(), nil
}

// pullRepository is the page of the repository on GitHub whose pull
// requests the switcher lists, takes links to and checks out: gh's for
// the work tree, a fork's upstream where gh repo set-default chose it,
// else fallback, the page of origin.
func pullRepository(ctx context.Context, repo, fallback string) string {
	out, err := gh(ctx, repo, "repo", "view", "--json", "url", "--jq", ".url")
	if page := githubPage(strings.TrimSpace(out)); err == nil && page != "" {
		return page
	}
	return fallback
}

// ghRepo is gh's --repo for the page of a repository, owner/name.
func ghRepo(page string) string {
	return strings.TrimPrefix(page, "https://github.com/")
}

func listPullRequests(ctx context.Context, repo, page string) ([]pullRequest, error) {
	out, err := gh(ctx, repo, "pr", "list", "--repo", ghRepo(page), "--state", "open", "--limit", "50",
		"--json", "number,title,headRefName,isDraft,author")
	if err != nil {
		return nil, err
	}
	var prs []pullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("read gh's pull requests: %w", err)
	}
	return prs, nil
}

// checkoutPullRequest checks out the pull request of the repository whose
// page this is, as gh does, from a fork's too, as a local branch tracking
// it.
func checkoutPullRequest(ctx context.Context, repo, page string, number int) error {
	_, err := gh(ctx, repo, "pr", "checkout", strconv.Itoa(number), "--repo", ghRepo(page))
	return err
}

var pullURL = regexp.MustCompile(`^(https?://github\.com/[^/]+/[^/]+)/pull/(\d+)(?:[/?#].*)?$`)

// parsePullRequest reads a pull request's number, as 12 or #12, or its
// page of the repository on GitHub, github, as the browser shows it.
func parsePullRequest(s, github string) (int, bool) {
	s = strings.TrimSpace(s)
	if m := pullURL.FindStringSubmatch(s); m != nil {
		if github != "" && !strings.EqualFold(strings.Replace(m[1], "http://", "https://", 1), github) {
			return 0, false
		}
		s = m[2]
	}
	n, err := strconv.Atoi(strings.TrimPrefix(s, "#"))
	return n, err == nil && n > 0
}
