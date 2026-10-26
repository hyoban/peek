package testutil

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Scenarios are the repositories the performance tests run over, Peek's
// and those it is compared with, each a commit and changes on top of it,
// made alike wherever they are made:
//
//   - large: a file of code of 20,000 lines, a line changed every 97;
//   - many: 200 files of 200 lines, each changed in 5 places;
//   - untracked: 20,000 small files git does not track, in 200 folders;
//   - memory: 300 files of 5,000 lines, a line added every 200.
var Scenarios = []string{"large", "many", "untracked", "memory"}

// MakeScenario makes the scenario's repository in dir, which it creates.
func MakeScenario(dir, name string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	g := func(args ...string) error {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=Ada", "GIT_AUTHOR_EMAIL=ada@example.com",
			"GIT_COMMITTER_NAME=Ada", "GIT_COMMITTER_EMAIL=ada@example.com",
			"GIT_AUTHOR_DATE=2026-01-01T00:00:00Z", "GIT_COMMITTER_DATE=2026-01-01T00:00:00Z")
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("git %s: %v: %s", strings.Join(args, " "), err, out)
		}
		return nil
	}
	write := func(name, text string) error {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		return os.WriteFile(p, []byte(text), 0o644)
	}
	if err := g("init", "-q", "-b", "main"); err != nil {
		return err
	}
	// code is a Go file of n lines, each changed one marked so.
	code := func(file, n int, changed func(line int) bool) string {
		var b strings.Builder
		b.WriteString("package main\n\nfunc f() {\n")
		for i := range n {
			if changed != nil && changed(i) {
				fmt.Fprintf(&b, "\t// changed %d\n", i)
			}
			fmt.Fprintf(&b, "\tvalue%d_%d := compute(%d, \"some text to make the line longer\") // note %d\n", file, i, i, i)
		}
		b.WriteString("}\n")
		return b.String()
	}
	var files, lines, every int
	switch name {
	case "large":
		files, lines, every = 1, 20000, 97
	case "many":
		files, lines, every = 200, 200, 40
	case "memory":
		files, lines, every = 300, 5000, 200
	case "untracked":
		if err := write("main.go", "package main\n"); err != nil {
			return err
		}
	default:
		return fmt.Errorf("no scenario %q", name)
	}
	for i := range files {
		if err := write(fmt.Sprintf("f%03d.go", i), code(i, lines, nil)); err != nil {
			return err
		}
	}
	if err := g("add", "."); err != nil {
		return err
	}
	if err := g("commit", "-q", "-m", "Add files"); err != nil {
		return err
	}
	for i := range files {
		if err := write(fmt.Sprintf("f%03d.go", i), code(i, lines, func(l int) bool { return l%every == every/2 })); err != nil {
			return err
		}
	}
	if name == "untracked" {
		// An hour old, as files a project has long had are.
		old := time.Now().Add(-time.Hour)
		for i := range 20000 {
			p := fmt.Sprintf("gen/d%03d/f%05d.ts", i/100, i)
			if err := write(p, "export const x = 1\nexport const y = 2\n"); err != nil {
				return err
			}
			os.Chtimes(filepath.Join(dir, p), old, old)
		}
	}
	return nil
}
