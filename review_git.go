package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var errReviewTooLarge = errors.New("Git metadata exceeds the review snapshot limit")

// reviewOutput never retains more than limit bytes. Stdout cancels the
// command on overflow; diagnostics are truncated without growing the buffer.
type reviewOutput struct {
	strings.Builder
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (w *reviewOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, w.limit-w.Len())
	w.Builder.Write(p[:keep])
	if keep < n {
		w.overflow = true
		if w.cancel != nil {
			w.cancel()
			return keep, errReviewTooLarge
		}
	}
	return n, nil
}

// Bound output while Git produces it, rather than after buffering it.
// Cancel closes the producer even if it keeps computing after a pipe closes.
func reviewGit(repo string, limit int, args ...string) (string, error) {
	if limit < 0 {
		return "", errReviewTooLarge
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	select {
	case gitSlots <- struct{}{}:
		defer func() { <-gitSlots }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", repo}, args...)...)
	cmd.Env = append(os.Environ(), "GIT_OPTIONAL_LOCKS=0")
	cmd.WaitDelay = time.Second
	out := reviewOutput{limit: limit, cancel: cancel}
	stderr := reviewOutput{limit: 32 * 1024}
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if out.overflow {
		return "", errReviewTooLarge
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

// Porcelain v2 exposes submodule work-tree and untracked dirt separately
// from a committed gitlink change. NUL records keep unusual paths intact.
func rejectDirtySubmodules(status string) error {
	records := strings.Split(status, "\x00")
	for i := 0; i < len(records); i++ {
		record := records[i]
		if !strings.HasPrefix(record, "1 ") && !strings.HasPrefix(record, "2 ") && !strings.HasPrefix(record, "u ") {
			continue
		}
		fields := strings.SplitN(record, " ", 4)
		if len(fields) >= 3 {
			sub := fields[2]
			if len(sub) == 4 && sub[0] == 'S' && (sub[2] != '.' || sub[3] != '.') {
				return errors.New("cannot review uncommitted changes inside submodules; commit or discard those changes and review again")
			}
		}
		if record[0] == '2' {
			i++ // the original path of a rename is the next NUL record
		}
	}
	return nil
}
