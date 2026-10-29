package git

import (
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	neturl "net/url"
	"os"
	"os/exec"
	"regexp"
	"slices"
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
// never pushed or a detached HEAD. The remote's default branch has the
// repository's own page. A remote may be named by its URL, as gh pr
// checkout tracks a fork's branch, which git then tells no upstream of.
func BranchGitHubURL(repo string) string {
	head, err := git(repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return ""
	}
	name := strings.TrimSpace(head)
	remote, _ := git(repo, "config", "--get", "branch."+name+".remote")
	ref, _ := git(repo, "config", "--get", "branch."+name+".merge")
	remote = strings.TrimSpace(remote)
	branch, ok := strings.CutPrefix(strings.TrimSpace(ref), "refs/heads/")
	if remote == "" || remote == "." || !ok {
		return ""
	}
	if page := githubPage(remote); page != "" {
		return page + "/tree/" + escapeBranch(branch)
	}
	url, err := git(repo, "config", "--get", "remote."+remote+".url")
	page := githubPage(strings.TrimSpace(url))
	if err != nil || page == "" {
		return ""
	}
	if branch == remoteDefaultBranch(repo, remote) {
		return page
	}
	return page + "/tree/" + escapeBranch(branch)
}

// escapeBranch is the branch's name as the path of its page.
func escapeBranch(branch string) string {
	parts := strings.Split(branch, "/")
	for i, p := range parts {
		parts[i] = neturl.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// remoteDefaultBranch returns the branch the remote's HEAD names, or else
// main or master where the remote has one.
func remoteDefaultBranch(repo, remote string) string {
	if out, err := git(repo, "symbolic-ref", "--quiet", "refs/remotes/"+remote+"/HEAD"); err == nil {
		return strings.TrimPrefix(strings.TrimSpace(out), "refs/remotes/"+remote+"/")
	}
	for _, name := range []string{"main", "master"} {
		if _, err := git(repo, "rev-parse", "--verify", "--quiet", "refs/remotes/"+remote+"/"+name); err == nil {
			return name
		}
	}
	return ""
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

// ListPullRequests lists the open pull requests of the repository whose
// page this is, the last updated first, as those at work on now.
func ListPullRequests(ctx context.Context, repo, page string) ([]PullRequest, error) {
	out, err := gh(ctx, repo, "pr", "list", "--repo", ghRepo(page), "--state", "open", "--limit", "50",
		"--search", "sort:updated-desc", "--json", "number,title,headRefName,isDraft,author")
	if err != nil {
		return nil, err
	}
	var prs []PullRequest
	if err := json.Unmarshal([]byte(out), &prs); err != nil {
		return nil, fmt.Errorf("read gh's pull requests: %w", err)
	}
	return prs, nil
}

// BranchPull is the pull request of the branch checked out, open or
// merged, as gh tells of it: its page, its description and the checks
// run on its last commit.
type BranchPull struct {
	Number int
	Title  string
	Body   string
	URL    string
	State  string // OPEN or MERGED
	Draft  bool
	Author string
	// Review is GitHub's decision of its reviews: APPROVED,
	// CHANGES_REQUESTED or REVIEW_REQUIRED, "" where none is required.
	Review string
	Checks []Check // the failing first, then those running, passing and skipped
	Base   string  // the branch it merges into
	// Mergeable tells whether it merges without conflicts: MERGEABLE,
	// CONFLICTING, or UNKNOWN while GitHub works it out.
	Mergeable string
	// MergeState is where merging it stands, as GitHub's merge box:
	// CLEAN, HAS_HOOKS, UNSTABLE (checks not required failed), BEHIND
	// (the base moved on, and the rules want it up to date), BLOCKED (by
	// the rules: reviews, required checks or conversations), DIRTY
	// (conflicts), DRAFT, or UNKNOWN while GitHub works it out.
	MergeState string
	// Unresolved counts its review conversations not resolved yet, which
	// the rules may want resolved; -1 where GitHub did not say.
	Unresolved int
}

// CheckState is where a check of a pull request stands, in the order the
// checks list.
type CheckState int

const (
	CheckFailed CheckState = iota
	CheckPending
	CheckPassed
	CheckSkipped
)

// Check is a check run, or a commit status, on a pull request's last
// commit.
type Check struct {
	Name     string
	Workflow string // the workflow of a GitHub Actions job's, "" for others
	URL      string // its details
	State    CheckState
	Required bool // the base's rules want it passing to merge
}

// Counts returns how many of the checks stand each way, by CheckState.
func (p *BranchPull) Counts() (n [4]int) {
	for _, ch := range p.Checks {
		n[ch.State]++
	}
	return n
}

// RequiredCounts returns how many of the checks the rules require stand
// each way, by CheckState.
func (p *BranchPull) RequiredCounts() (n [4]int) {
	for _, ch := range p.Checks {
		if ch.Required {
			n[ch.State]++
		}
	}
	return n
}

// ChecksState is where the checks stand as one, as GitHub's mark beside
// a commit: failed where one has, else running where one is, else passed;
// false where there are none but skipped.
func (p *BranchPull) ChecksState() (CheckState, bool) {
	n := p.Counts()
	for _, s := range []CheckState{CheckFailed, CheckPending, CheckPassed} {
		if n[s] > 0 {
			return s, true
		}
	}
	return 0, false
}

type ghCheck struct {
	Type       string    `json:"__typename"`
	Name       string    `json:"name"`
	Workflow   string    `json:"workflowName"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	DetailsURL string    `json:"detailsUrl"`
	StartedAt  time.Time `json:"startedAt"`
	// A commit status's.
	Context   string `json:"context"`
	State     string `json:"state"`
	TargetURL string `json:"targetUrl"`
}

func (g ghCheck) check() Check {
	if g.Type == "StatusContext" {
		ch := Check{Name: g.Context, URL: g.TargetURL, State: CheckFailed}
		switch g.State {
		case "SUCCESS":
			ch.State = CheckPassed
		case "PENDING", "EXPECTED":
			ch.State = CheckPending
		}
		return ch
	}
	ch := Check{Name: g.Name, Workflow: g.Workflow, URL: g.DetailsURL, State: CheckFailed}
	switch {
	case g.Status != "COMPLETED":
		ch.State = CheckPending
	case g.Conclusion == "SUCCESS":
		ch.State = CheckPassed
	case g.Conclusion == "SKIPPED" || g.Conclusion == "NEUTRAL":
		ch.State = CheckSkipped
	}
	return ch
}

// readChecks reads gh's statusCheckRollup: a check run again keeps its
// latest run alone, as GitHub lists them.
func readChecks(rollup []ghCheck) []Check {
	latest := map[[2]string]int{}
	var kept []ghCheck
	for _, g := range rollup {
		key := [2]string{g.Workflow, g.Name + g.Context}
		if i, ok := latest[key]; ok {
			if g.StartedAt.After(kept[i].StartedAt) {
				kept[i] = g
			}
			continue
		}
		latest[key] = len(kept)
		kept = append(kept, g)
	}
	checks := make([]Check, len(kept))
	for i, g := range kept {
		checks[i] = g.check()
	}
	slices.SortStableFunc(checks, func(a, b Check) int {
		return cmp.Or(cmp.Compare(a.State, b.State), cmp.Compare(a.Workflow, b.Workflow), cmp.Compare(a.Name, b.Name))
	})
	return checks
}

// BranchPullRequest returns the pull request of the branch checked out,
// as gh finds it, open or merged, nil where it has none.
func BranchPullRequest(ctx context.Context, repo string) (*BranchPull, error) {
	out, err := gh(ctx, repo, "pr", "view", "--json",
		"number,title,body,url,state,isDraft,author,reviewDecision,statusCheckRollup,baseRefName,mergeable,mergeStateStatus")
	if err != nil {
		if strings.Contains(err.Error(), "no pull requests found") {
			return nil, nil
		}
		return nil, err
	}
	var pr struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		Body   string `json:"body"`
		URL    string `json:"url"`
		State  string `json:"state"`
		Draft  bool   `json:"isDraft"`
		Author struct {
			Login string `json:"login"`
		} `json:"author"`
		Review     string    `json:"reviewDecision"`
		Checks     []ghCheck `json:"statusCheckRollup"`
		Base       string    `json:"baseRefName"`
		Mergeable  string    `json:"mergeable"`
		MergeState string    `json:"mergeStateStatus"`
	}
	if err := json.Unmarshal([]byte(out), &pr); err != nil {
		return nil, fmt.Errorf("read gh's pull request: %w", err)
	}
	if pr.State == "CLOSED" || !pullURL.MatchString(pr.URL) {
		return nil, nil
	}
	p := &BranchPull{
		Number: pr.Number, Title: pr.Title, Body: pr.Body, URL: pr.URL, State: pr.State, Draft: pr.Draft,
		Author: pr.Author.Login, Review: pr.Review, Checks: readChecks(pr.Checks),
		Base: pr.Base, Mergeable: pr.Mergeable, MergeState: pr.MergeState, Unresolved: -1,
	}
	if p.State == "OPEN" {
		readMergeRules(ctx, repo, p)
	}
	return p, nil
}

// readMergeRules asks GitHub what gh pr view does not tell of what holds
// the pull request back: which checks the rules require, and how many
// conversations are not resolved. What GitHub cannot tell is left
// unknown, as the pull request stands without it.
func readMergeRules(ctx context.Context, repo string, p *BranchPull) {
	m := pullURL.FindStringSubmatch(p.URL)
	owner, name, _ := strings.Cut(strings.TrimPrefix(m[1], "https://github.com/"), "/")
	const query = `query($owner: String!, $name: String!, $number: Int!) {
  repository(owner: $owner, name: $name) {
    pullRequest(number: $number) {
      reviewThreads(first: 100) { nodes { isResolved } }
      commits(last: 1) { nodes { commit { statusCheckRollup { contexts(first: 100) { nodes {
        __typename
        ... on CheckRun { name isRequired(pullRequestNumber: $number) checkSuite { workflowRun { workflow { name } } } }
        ... on StatusContext { context isRequired(pullRequestNumber: $number) }
      } } } } } }
    }
  }
}`
	out, err := gh(ctx, repo, "api", "graphql", "-f", "query="+query, "-f", "owner="+owner, "-f", "name="+name,
		"-F", "number="+strconv.Itoa(p.Number))
	if err != nil {
		return
	}
	var r struct {
		Data struct {
			Repository struct {
				PullRequest struct {
					ReviewThreads struct {
						Nodes []struct {
							IsResolved bool `json:"isResolved"`
						} `json:"nodes"`
					} `json:"reviewThreads"`
					Commits struct {
						Nodes []struct {
							Commit struct {
								Rollup *struct {
									Contexts struct {
										Nodes []struct {
											Name       string `json:"name"`
											Context    string `json:"context"`
											IsRequired bool   `json:"isRequired"`
											CheckSuite struct {
												WorkflowRun *struct {
													Workflow struct {
														Name string `json:"name"`
													} `json:"workflow"`
												} `json:"workflowRun"`
											} `json:"checkSuite"`
										} `json:"nodes"`
									} `json:"contexts"`
								} `json:"statusCheckRollup"`
							} `json:"commit"`
						} `json:"nodes"`
					} `json:"commits"`
				} `json:"pullRequest"`
			} `json:"repository"`
		} `json:"data"`
	}
	if json.Unmarshal([]byte(out), &r) != nil {
		return
	}
	pr := r.Data.Repository.PullRequest
	p.Unresolved = 0
	for _, t := range pr.ReviewThreads.Nodes {
		if !t.IsResolved {
			p.Unresolved++
		}
	}
	required := map[[2]string]bool{}
	for _, cm := range pr.Commits.Nodes {
		if cm.Commit.Rollup == nil {
			continue
		}
		for _, n := range cm.Commit.Rollup.Contexts.Nodes {
			if !n.IsRequired {
				continue
			}
			workflow := ""
			if n.CheckSuite.WorkflowRun != nil {
				workflow = n.CheckSuite.WorkflowRun.Workflow.Name
			}
			required[[2]string{workflow, n.Name + n.Context}] = true
		}
	}
	for i, ch := range p.Checks {
		p.Checks[i].Required = required[[2]string{ch.Workflow, ch.Name}]
	}
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
