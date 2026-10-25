package main

import (
	"context"
	"strconv"
	"strings"
)

// upstreamRemote is the remote the branch checked out tracks, and its
// upstream's name there, as remote/branch; "" where it tracks none, or
// a local branch.
func upstreamRemote(ctx context.Context, repo string) (remote, upstream string) {
	branch, err := pushGit(ctx, repo, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || branch == "" {
		return "", ""
	}
	remote, _ = pushGit(ctx, repo, "config", "--get", "branch."+branch+".remote")
	if remote == "" || remote == "." {
		return "", ""
	}
	upstream, err = pushGit(ctx, repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	if err != nil {
		return "", ""
	}
	return remote, upstream
}

// behindUpstream counts the commits of the branch's upstream, as last
// fetched, that the branch has not, and names the upstream; 0 and "" for
// a branch that tracks none. It reads cached refs, without network I/O.
func behindUpstream(repo string) (int, string) {
	out, err := git(repo, "rev-list", "--count", "HEAD..@{upstream}")
	if err != nil {
		return 0, ""
	}
	n, _ := strconv.Atoi(strings.TrimSpace(out))
	if n == 0 {
		return 0, ""
	}
	name, _ := git(repo, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}")
	return n, strings.TrimSpace(name)
}

// fetchUpstream fetches the remote the branch checked out tracks, quietly
// and without asking for credentials, and reports whether the upstream
// moved, for the history to be read again.
func fetchUpstream(ctx context.Context, repo string) (bool, error) {
	remote, upstream := upstreamRemote(ctx, repo)
	if remote == "" {
		return false, nil
	}
	ref := "refs/remotes/" + upstream
	before, _ := pushGit(ctx, repo, "rev-parse", "--verify", "--quiet", ref)
	if _, err := pushGit(ctx, repo, "fetch", "--quiet", "--no-tags", "--no-write-fetch-head", "--", remote); err != nil {
		return false, err
	}
	after, _ := pushGit(ctx, repo, "rev-parse", "--verify", "--quiet", ref)
	return before != after, nil
}

// pullUpstream brings the branch up to its upstream as last fetched, by
// fast-forward alone, carrying the work tree's changes along as git does
// and refusing where they would be lost or the branches diverged.
func pullUpstream(ctx context.Context, repo string) error {
	_, err := pushGit(ctx, repo, "merge", "--ff-only", "--quiet", "@{upstream}")
	return gitFailure(err)
}
