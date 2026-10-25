package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

var ErrTooLarge = errors.New("Git metadata exceeds the review snapshot limit")

// boundedOutput never retains more than limit bytes. Stdout cancels the
// command on overflow; diagnostics are truncated without growing the buffer.
type boundedOutput struct {
	strings.Builder
	limit    int
	overflow bool
	cancel   context.CancelFunc
}

func (w *boundedOutput) Write(p []byte) (int, error) {
	n := len(p)
	keep := min(n, w.limit-w.Len())
	w.Builder.Write(p[:keep])
	if keep < n {
		w.overflow = true
		if w.cancel != nil {
			w.cancel()
			return keep, ErrTooLarge
		}
	}
	return n, nil
}

// Bound output while Git produces it, rather than after buffering it.
// Cancel closes the producer even if it keeps computing after a pipe closes.
func Bounded(repo string, limit int, args ...string) (string, error) {
	if limit < 0 {
		return "", ErrTooLarge
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
	out := boundedOutput{limit: limit, cancel: cancel}
	stderr := boundedOutput{limit: 32 * 1024}
	cmd.Stdout, cmd.Stderr = &out, &stderr
	err := cmd.Run()
	if out.overflow {
		return "", ErrTooLarge
	}
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	if err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}
