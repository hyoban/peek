package git

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestBoundedStopsOversizedOutput(t *testing.T) {
	dir := t.TempDir()
	// Keep running after producing the output: merely stopping the reader
	// without terminating its producer would wait for the command timeout.
	script := `#!/bin/sh
i=0
while [ "$i" -lt 4096 ]; do
  printf '%01024d' 0
  i=$((i+1))
done
while :; do :; done
`
	if err := os.WriteFile(filepath.Join(dir, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	start := time.Now()
	out, err := Bounded(dir, 1024, "diff")
	if !errors.Is(err, ErrTooLarge) || out != "" {
		t.Fatalf("returned truncated output as a review: %q, %v", out, err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("did not terminate the oversized producer promptly: %v", elapsed)
	}
}
