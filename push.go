package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type messageGenerator func(context.Context, string, string) (string, error)

// Use the same destination as Push, rather than the pull upstream, which
// may be different for a fork. Read cached remote refs without network I/O.
func hasUnpushedCommits(repo string) bool {
	g := func(args ...string) (string, error) {
		out, err := git(repo, args...)
		return strings.TrimSpace(out), err
	}
	if _, err := g("rev-parse", "--verify", "HEAD"); err != nil {
		return false
	}
	branch, _ := g("symbolic-ref", "--quiet", "--short", "HEAD")
	remote, target, err := pushDestination(g, branch)
	if err != nil {
		return false
	}
	fetchURL, _ := g("remote", "get-url", remote)
	pushURL, _ := g("remote", "get-url", "--push", remote)
	if fetchURL != pushURL {
		return true // fetched refs cannot prove a different push destination is up to date
	}
	args := []string{"rev-list", "--max-count=1", "HEAD", "--not"}
	if branch == "" {
		args = append(args, "--remotes="+remote)
	} else {
		ref := "refs/remotes/" + remote + "/" + target
		if _, err := g("rev-parse", "--verify", ref); err != nil {
			return true // a branch that has not been published yet
		}
		args = append(args, ref)
	}
	out, err := g(args...)
	return err != nil || out != ""
}

// pushRepository commits the staged snapshot, then pushes exactly one branch.
// Failures leave the index and any new commit available for a retry.
func pushRepository(ctx context.Context, repo string, generate messageGenerator, progress func(string)) (string, error) {
	g := func(args ...string) (string, error) { return pushGit(ctx, repo, args...) }
	progress("Checking repository…")
	for _, state := range []string{"MERGE_HEAD", "CHERRY_PICK_HEAD", "REVERT_HEAD", "rebase-merge", "rebase-apply", "sequencer"} {
		p, err := g("rev-parse", "--git-path", state)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(repo, p)
		}
		if _, err := os.Stat(p); err == nil {
			return "", fmt.Errorf("finish the merge, rebase or cherry-pick before pushing")
		}
	}
	unmerged, err := g("ls-files", "--unmerged")
	if err != nil {
		return "", err
	}
	if unmerged != "" {
		return "", fmt.Errorf("resolve the conflicted files before pushing")
	}
	branch, _ := g("symbolic-ref", "--quiet", "--short", "HEAD")
	head, _ := g("rev-parse", "--verify", "HEAD") // empty for the first commit
	remote, target, err := pushDestination(g, branch)
	if err != nil {
		return "", err
	}
	// Fetch before committing so offline and authentication failures do not
	// create a commit. Git still checks for concurrent remote updates on push.
	progress("Fetching " + remote + "…")
	if _, err := g("fetch", "--no-tags", "--", remote); err != nil {
		return "", err
	}
	if err := samePushHead(g, branch, head); err != nil {
		return "", err
	}
	progress("Staging all changes…")
	if _, err := g("add", "--all"); err != nil {
		return "", err
	}
	tree, err := g("write-tree")
	if err != nil {
		return "", err
	}
	baseTree, _ := g("rev-parse", "HEAD^{tree}")
	if tree != baseTree {
		diff, err := g("diff", "--cached", "--no-ext-diff", "--no-textconv", "--binary", "--no-color")
		if err != nil {
			return "", err
		}
		if len(diff) > 256*1024 {
			return "", fmt.Errorf("the staged diff is too large for commit message generation (limit 256 KiB); commit it manually, then retry Push")
		}
		history, _ := g("log", "-8", "--format=%s")
		progress("Generating commit message with Codex…")
		message, err := generate(ctx, diff, history)
		if err != nil {
			return "", fmt.Errorf("generate commit message: %w", err)
		}
		message = strings.TrimSpace(message)
		if message == "" || strings.ContainsRune(message, 0) {
			return "", fmt.Errorf("Codex returned an empty or invalid commit message")
		}
		currentTree, err := g("write-tree")
		if err != nil {
			return "", err
		}
		if currentTree != tree {
			return "", fmt.Errorf("the index or branch changed while Codex was working; retry Push")
		}
		if err := samePushHead(g, branch, head); err != nil {
			return "", err
		}
		progress("Committing changes…")
		if _, err := g("commit", "-m", message); err != nil {
			return "", err
		}
		head, err = g("rev-parse", "--verify", "HEAD")
		if err != nil {
			return "", err
		}
	}
	if err := samePushHead(g, branch, head); err != nil {
		return "", err
	}
	if branch == "" {
		return pushNewBranch(g, remote, progress)
	}
	progress("Pushing " + remote + "/" + target + "…")
	args := []string{"-c", "push.followTags=false", "push", "--porcelain"}
	// Keep a fork's pull upstream when pushing to a different remote.
	if upstream, _ := g("config", "--get", "branch."+branch+".merge"); upstream == "" {
		args = append(args, "--set-upstream")
	}
	args = append(args, "--", remote, "HEAD:refs/heads/"+target)
	if _, err := g(args...); err != nil {
		if !protectedBranch(err.Error()) {
			return "", fmt.Errorf("push failed; any local commit is kept: %w", err)
		}
		if err := samePushHead(g, branch, head); err != nil {
			return "", err
		}
		return pushNewBranch(g, remote, progress)
	}
	return "Pushed to " + remote + "/" + target, nil
}

func samePushHead(g func(...string) (string, error), branch, head string) error {
	currentHead, _ := g("rev-parse", "--verify", "HEAD")
	currentBranch, _ := g("symbolic-ref", "--quiet", "--short", "HEAD")
	if currentHead != head || currentBranch != branch {
		return fmt.Errorf("the branch changed during Push; retry Push")
	}
	return nil
}

// Honor Git's push-remote settings, including triangular fork workflows.
// With no configured destination, origin or a sole remote is unambiguous.
func pushDestination(g func(...string) (string, error), branch string) (remote, target string, err error) {
	config := func(key string) string { value, _ := g("config", "--get", key); return value }
	if branch != "" {
		remote = config("branch." + branch + ".pushRemote")
	}
	if remote == "" {
		remote = config("remote.pushDefault")
	}
	tracking := ""
	if branch != "" {
		tracking = config("branch." + branch + ".remote")
	}
	if remote == "" && tracking != "." {
		remote = tracking
	}
	if remote == "" {
		remotes, e := g("remote")
		if e != nil {
			return "", "", e
		}
		for _, r := range strings.Fields(remotes) {
			if r == "origin" {
				remote = r
				break
			}
		}
		if remote == "" && len(strings.Fields(remotes)) == 1 {
			remote = strings.Fields(remotes)[0]
		}
	}
	if remote == "" || remote == "." {
		return "", "", fmt.Errorf("configure a push remote (origin or remote.pushDefault) before pushing")
	}
	urls, e := g("remote", "get-url", "--push", "--all", remote)
	if e != nil {
		return "", "", e
	}
	if len(strings.Split(urls, "\n")) != 1 || config("remote."+remote+".mirror") == "true" {
		return "", "", fmt.Errorf("Push requires a single destination without remote mirroring")
	}
	target = branch
	if branch != "" && remote == tracking {
		merge := config("branch." + branch + ".merge")
		if strings.HasPrefix(merge, "refs/heads/") {
			target = strings.TrimPrefix(merge, "refs/heads/")
		}
	}
	return remote, target, nil
}

func protectedBranch(message string) bool {
	message = strings.ToLower(message)
	return strings.Contains(message, "[remote rejected]") &&
		(strings.Contains(message, "protected branch") ||
			strings.Contains(message, "changes must be made through a pull request") ||
			strings.Contains(message, "not allowed to push code to protected branches"))
}

func pushNewBranch(g func(...string) (string, error), remote string, progress func(string)) (string, error) {
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return "", err
	}
	branch := fmt.Sprintf("peek/push-%s-%x", time.Now().UTC().Format("20060102-150405"), suffix)
	progress("Creating " + branch + "…")
	if _, err := g("switch", "-c", branch); err != nil {
		return "", err
	}
	progress("Pushing " + remote + "/" + branch + "…")
	if _, err := g("-c", "push.followTags=false", "push", "--porcelain", "--set-upstream", "--", remote, "HEAD:refs/heads/"+branch); err != nil {
		return "", fmt.Errorf("push failed; the commit is kept on %s: %w", branch, err)
	}
	return "Pushed to new branch " + remote + "/" + branch, nil
}

func pushGit(ctx context.Context, repo string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "LC_ALL=C")
	cmd.WaitDelay = time.Second
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		return "", fmt.Errorf("git %s: %w\n%s%s", strings.Join(args, " "), err, out.String(), stderr.String())
	}
	return strings.TrimSpace(out.String()), nil
}
