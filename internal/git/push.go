package git

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type Snapshot struct {
	Head, Tree string
}

type MessageGenerator func(context.Context, string, Snapshot) (string, error)

// Use the same destination as Ship, rather than the pull upstream, which
// may be different for a fork. Read cached remote refs without network I/O.
func HasUnpushedCommits(repo string) bool {
	g := func(args ...string) (string, error) {
		out, err := git(repo, args...)
		return strings.TrimSpace(out), err
	}
	// What does not depend on another reads at once, its processes else
	// adding up.
	var (
		wg      sync.WaitGroup
		headErr error
		branch  string
	)
	wg.Go(func() { _, headErr = g("rev-parse", "--verify", "HEAD") })
	wg.Go(func() { branch, _ = g("symbolic-ref", "--quiet", "--short", "HEAD") })
	wg.Wait()
	if headErr != nil {
		return false
	}
	remote, target, err := PushDestination(g, branch)
	if err != nil {
		return false
	}
	var fetchURL, pushURL string
	wg.Go(func() { fetchURL, _ = g("remote", "get-url", remote) })
	wg.Go(func() { pushURL, _ = g("remote", "get-url", "--push", remote) })
	wg.Wait()
	if fetchURL != pushURL {
		return true // fetched refs cannot prove a different push destination is up to date
	}
	args := []string{"rev-list", "--max-count=1", "HEAD", "--not"}
	if branch == "" {
		args = append(args, "--remotes="+remote)
	} else {
		// A branch not published yet has no ref, which fails the list.
		args = append(args, "refs/remotes/"+remote+"/"+target)
	}
	out, err := g(args...)
	return err != nil || out != ""
}

// Push commits the staged snapshot, then pushes exactly one branch.
// Failures leave the index and any new commit available for a retry.
func Push(ctx context.Context, repo string, generate MessageGenerator, progress func(string)) (string, error) {
	g := func(args ...string) (string, error) { return Run(ctx, repo, args...) }
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
	remote, target, err := PushDestination(g, branch)
	if err != nil {
		return "", err
	}
	// Fetch before committing so offline and authentication failures do not
	// create a commit, but while Codex writes the message rather than before.
	// Git still checks for concurrent remote updates on push.
	progress("Fetching " + remote + "…")
	fetched := make(chan error, 1)
	go func() {
		_, err := g("fetch", "--no-tags", "--", remote)
		fetched <- err
	}()
	waitFetch := func() error {
		if fetched == nil {
			return nil
		}
		err := <-fetched
		fetched = nil
		if err != nil {
			return err
		}
		return samePushHead(g, branch, head)
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
		progress("Generating commit message with Codex…")
		message, err := generate(ctx, repo, Snapshot{Head: head, Tree: tree})
		if err != nil {
			return "", fmt.Errorf("generate commit message: %w", err)
		}
		if err := waitFetch(); err != nil {
			return "", err
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
			return "", fmt.Errorf("the index or branch changed while Codex was working; retry Ship")
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
	if err := waitFetch(); err != nil {
		return "", err
	}
	if err := samePushHead(g, branch, head); err != nil {
		return "", err
	}
	if branch == "" {
		return pushNewBranch(g, remote, progress)
	}
	progress("Shipping " + remote + "/" + target + "…")
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
	return "Shipped to " + remote + "/" + target, nil
}

func samePushHead(g func(...string) (string, error), branch, head string) error {
	currentHead, _ := g("rev-parse", "--verify", "HEAD")
	currentBranch, _ := g("symbolic-ref", "--quiet", "--short", "HEAD")
	if currentHead != head || currentBranch != branch {
		return fmt.Errorf("the branch changed during Ship; retry Ship")
	}
	return nil
}

// Honor Git's push-remote settings, including triangular fork workflows.
// With no configured destination, origin or a sole remote is unambiguous.
func PushDestination(g func(...string) (string, error), branch string) (remote, target string, err error) {
	config := configOf(g)
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
		return "", "", fmt.Errorf("Ship requires a single destination without remote mirroring")
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

// configOf returns what git config --get prints of a key, read from the
// whole config at once: a process a key added up to most of the wait.
func configOf(g func(...string) (string, error)) func(key string) string {
	var values map[string]string
	listed := false
	return func(key string) string {
		if !listed {
			listed = true
			if out, err := g("config", "--list", "-z"); err == nil {
				values = parseConfig(out)
			}
		}
		if values == nil {
			value, _ := g("config", "--get", key)
			return value
		}
		return values[configKey(key)]
	}
}

// parseConfig reads git config --list -z's entries, the last of a key's
// values winning, as --get's does.
func parseConfig(out string) map[string]string {
	values := map[string]string{}
	for _, entry := range strings.Split(out, "\x00") {
		if entry == "" {
			continue
		}
		key, value, _ := strings.Cut(entry, "\n")
		values[configKey(key)] = strings.TrimSpace(value)
	}
	return values
}

// configKey is a key as git compares them: its section and its name in any
// case, a subsection, as a branch's name, in its own.
func configKey(key string) string {
	first, last := strings.Index(key, "."), strings.LastIndex(key, ".")
	if first < 0 {
		return strings.ToLower(key)
	}
	return strings.ToLower(key[:first]) + key[first:last] + strings.ToLower(key[last:])
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
	progress("Shipping " + remote + "/" + branch + "…")
	if _, err := g("-c", "push.followTags=false", "push", "--porcelain", "--set-upstream", "--", remote, "HEAD:refs/heads/"+branch); err != nil {
		return "", fmt.Errorf("push failed; the commit is kept on %s: %w", branch, err)
	}
	return "Shipped to new branch " + remote + "/" + branch, nil
}

func Run(ctx context.Context, repo string, args ...string) (string, error) {
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

// Published is how far the branch's commits have reached the place Ship
// pushes to, as last fetched: Ref names the remote's branch, as
// "origin/main", "" for one not published yet or a detached HEAD, Hash
// is the commit it is at, and Unpushed holds those of HEAD's commits,
// among the limit newest, it does not have. It reads cached remote refs
// without network I/O.
type Published struct {
	Ref, Hash string
	Unpushed  map[string]bool
}

func ReadPublished(repo string, limit int) Published {
	g := func(args ...string) (string, error) {
		out, err := git(repo, args...)
		return strings.TrimSpace(out), err
	}
	branch, _ := g("symbolic-ref", "--quiet", "--short", "HEAD")
	remote, target, err := PushDestination(g, branch)
	if err != nil {
		return Published{} // nowhere to push, so nothing to mark
	}
	var p Published
	not := "--remotes=" + remote
	if branch != "" {
		ref := "refs/remotes/" + remote + "/" + target
		if hash, err := g("rev-parse", "--verify", "--quiet", ref+"^{commit}"); err == nil {
			p.Ref, p.Hash, not = remote+"/"+target, hash, ref
		}
	}
	out, err := g("rev-list", fmt.Sprintf("--max-count=%d", limit), "HEAD", "--not", not)
	if err != nil {
		return p
	}
	for hash := range strings.FieldsSeq(out) {
		if p.Unpushed == nil {
			p.Unpushed = map[string]bool{}
		}
		p.Unpushed[hash] = true
	}
	return p
}
