//go:build godiffperf

// Godiff's side of the comparison scripts/perf/compare.sh makes: the
// measures of Peek's performance tests (internal/app/perf_test.go), over
// the same repositories, taken of a checkout of Godiff this file is
// copied into, its lines wrapped as Peek's are. It prints one line a
// measure:
//
//	PERF godiff <scenario> <measure> <value> <unit>
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/godiff/internal/git"
	"github.com/egoist/mygo/ui"
)

func perfRepo(t *testing.T, name string) string {
	dir := os.Getenv("GODIFF_PERF_REPOS")
	if dir == "" {
		t.Skip("GODIFF_PERF_REPOS not set")
	}
	return filepath.Join(dir, name)
}

func report(scenario, measure string, value float64, unit string) {
	fmt.Printf("PERF godiff %s %s %.1f %s\n", scenario, measure, value, unit)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// openPerf opens a window over the repository, its lines wrapped, as
// newTestWindow does, and draws its first frame, reporting how long it
// took where scenario is given.
func openPerf(t *testing.T, scenario, dir, style string) (*window, *ui.Tester) {
	start := time.Now()
	repo, err := git.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	w := newWindow(repo, source{})
	w.settings = defaultSettings()
	w.settings.WordWrap, w.settings.DiffStyle = true, style
	w.sidebarShown, w.sidebarWidth = true, sidebarDefault
	w.load()
	w.loadHistory()
	w.loadUser()
	tt := ui.NewTester(w.view, 1280, 860)
	tt.Frame()
	if scenario != "" {
		report(scenario, "open", ms(time.Since(start)), "ms")
	}
	tt.Frame() // MyGo's Tester of Godiff's version has no Advance
	return w, tt
}

func frames(scenario, measure string, tt *ui.Tester, n int, step func(i int)) {
	start := time.Now()
	for i := range n {
		step(i)
		tt.Frame()
	}
	report(scenario, measure, ms(time.Since(start))/float64(n), "ms/frame")
}

func TestPerfLarge(t *testing.T) {
	repo := perfRepo(t, "large")
	for i, style := range []string{"unified", "split"} {
		scenario := ""
		if i == 0 {
			scenario = "large"
		}
		_, tt := openPerf(t, scenario, repo, style)
		frames("large", "scroll-"+style, tt, 150, func(int) { tt.Scroll(800, 400, 0, 120) })
		frames("large", "resize-"+style, tt, 30, func(i int) { tt.SetSize(1280+i*4, 860) })
	}
}

func TestPerfMany(t *testing.T) {
	repo := perfRepo(t, "many")
	for i, style := range []string{"unified", "split"} {
		scenario := ""
		if i == 0 {
			scenario = "many"
		}
		_, tt := openPerf(t, scenario, repo, style)
		frames("many", "scroll-"+style, tt, 300, func(int) { tt.Scroll(800, 400, 0, 120) })
	}
}

func TestPerfUntracked(t *testing.T) {
	w, tt := openPerf(t, "untracked", perfRepo(t, "untracked"), "unified")
	for range 2 {
		start := time.Now()
		w.refresh()
		tt.Frame()
		report("untracked", "refresh", ms(time.Since(start)), "ms")
	}
}

// Godiff reads every file's contents as it opens: loading them all is its
// opening.
func TestPerfMemory(t *testing.T) {
	start := time.Now()
	w, _ := openPerf(t, "memory", perfRepo(t, "memory"), "unified")
	report("memory", "load-all", ms(time.Since(start)), "ms")
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	report("memory", "heap", float64(m.HeapAlloc>>20), "MB")
	runtime.KeepAlive(w)
}
