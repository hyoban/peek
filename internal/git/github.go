package git

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// GitHubURL returns the GitHub page of the repository, from its origin's
// URL or else another remote's, "" where none is on GitHub.
func GitHubURL(repo string) string {
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

// BranchGitHubURL returns the GitHub page of the branch checked out, on
// the remote it tracks, "" where it tracks none on GitHub, as a branch
// never pushed or a detached HEAD.
func BranchGitHubURL(repo string) string {
	head, err := git(repo, "symbolic-ref", "--quiet", "HEAD")
	if err != nil {
		return ""
	}
	out, err := git(repo, "for-each-ref", "--format=%(upstream:remotename)%00%(upstream:remoteref)", strings.TrimSpace(head))
	remote, ref, _ := strings.Cut(strings.TrimSpace(out), "\x00")
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	if err != nil || remote == "" || !ok {
		return ""
	}
	url, err := git(repo, "config", "--get", "remote."+remote+".url")
	page := githubPage(strings.TrimSpace(url))
	if err != nil || page == "" {
		return ""
	}
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = neturl.PathEscape(p)
	}
	return page + "/tree/" + strings.Join(parts, "/")
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

// PullRequest is an open pull request of the repository, as gh lists it.
type PullRequest struct {
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

// PullRepository is the page of the repository on GitHub whose pull
// requests the switcher lists, takes links to and checks out: gh's for
// the work tree, a fork's upstream where gh repo set-default chose it,
// else fallback, the page of origin.
func PullRepository(ctx context.Context, repo, fallback string) string {
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

func ListPullRequests(ctx context.Context, repo, page string) ([]PullRequest, error) {
	out, err := gh(ctx, repo, "pr", "list", "--repo", ghRepo(page), "--state", "open", "--limit", "50",
		"--json", "number,title,headRefName,isDraft,author")
	if err != nil {
		return nil, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("read gh's pull requests: %w", err)
	}
	return prs, nil
}

// CheckoutPullRequest checks out the pull request of the repository whose
// page this is, as gh does, from a fork's too, as a local branch tracking
// it.
func CheckoutPullRequest(ctx context.Context, repo, page string, number int) error {
	_, err := gh(ctx, repo, "pr", "checkout", strconv.Itoa(number), "--repo", ghRepo(page))
	return err
}

var pullURL = regexp.MustCompile(`^(https?://github\.com/[^/]+/[^/]+)/pull/(\d+)(?:[/?#].*)?$`)

// ParsePullRequest reads a pull request's number, as 12 or #12, or its
// page of the repository on GitHub, github, as the browser shows it.
func ParsePullRequest(s, github string) (int, bool) {
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
