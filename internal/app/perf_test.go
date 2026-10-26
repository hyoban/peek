package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"peek/internal/testutil"

	"github.com/egoist/mygo/ui"
)

// The performance tests time the app over the scenarios' repositories
// (testutil.Scenarios), drawing its frames without a window, and print
// one line a measure:
//
//	PERF peek <scenario> <measure> <value> <unit>
//
// They run where PEEK_PERF is set, over the repositories PEEK_PERF_REPOS
// holds, as scripts/perf/compare.sh makes them, else over their own:
//
//	PEEK_PERF=1 go test ./internal/app -run TestPerf -v
//
// docs/performance.md tells what they found.

// perfRepo is the scenario's repository, skipping the test where
// PEEK_PERF is not set.
func perfRepo(t *testing.T, name string) string {
	if os.Getenv("PEEK_PERF") == "" {
		t.Skip("PEEK_PERF not set")
	}
	if dir := os.Getenv("PEEK_PERF_REPOS"); dir != "" {
		return filepath.Join(dir, name)
	}
	dir := filepath.Join(t.TempDir(), name)
	if err := testutil.MakeScenario(dir, name); err != nil {
		t.Fatal(err)
	}
	return dir
}

func report(scenario, measure string, value float64, unit string) {
	fmt.Printf("PERF peek %s %s %.1f %s\n", scenario, measure, value, unit)
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// openPerf opens the app over the repository and draws its first frame,
// the diffs in view loaded, reporting how long it took.
func openPerf(t *testing.T, scenario, repo string) (*app, *ui.Tester) {
	start := time.Now()
	a, tt := testApp(t, repo, nil)
	tt.Frame()
	report(scenario, "open", ms(time.Since(start)), "ms")
	for _, d := range a.diffs {
		d.read = false // every diff shown, none folded as viewed
	}
	settle(tt)
	return a, tt
}

// frames reports the time a frame takes on average as step runs before
// each.
func frames(scenario, measure string, tt *ui.Tester, n int, step func(i int)) {
	start := time.Now()
	for i := range n {
		step(i)
		tt.Frame()
	}
	report(scenario, measure, ms(time.Since(start))/float64(n), "ms/frame")
}

func layouts() []bool { return []bool{false, true} }

func layoutName(split bool) string {
	if split {
		return "split"
	}
	return "unified"
}

// A long file's diff: scrolled through, and the window resized over it.
func TestPerfLarge(t *testing.T) {
	a, tt := openPerf(t, "large", perfRepo(t, "large"))
	for _, split := range layouts() {
		a.split = split
		a.scrollToFile(a.diffs[0].change.Path)
		settle(tt)
		frames("large", "scroll-"+layoutName(split), tt, 150, func(int) { tt.Scroll(800, 400, 0, 120) })
		frames("large", "resize-"+layoutName(split), tt, 30, func(i int) { tt.SetSize(1280+i*4, 860) })
		tt.SetSize(1280, 860)
	}
}

// Many short diffs, scrolled through.
func TestPerfMany(t *testing.T) {
	a, tt := openPerf(t, "many", perfRepo(t, "many"))
	for _, split := range layouts() {
		a.split = split
		a.scrollToFile(a.diffs[0].change.Path)
		settle(tt)
		frames("many", "scroll-"+layoutName(split), tt, 300, func(int) { tt.Scroll(800, 400, 0, 120) })
	}
}

// Thousands of files git does not track: the project opened, then read
// again as a change of its files asks.
func TestPerfUntracked(t *testing.T) {
	a, tt := openPerf(t, "untracked", perfRepo(t, "untracked"))
	for range 2 { // the second as every refresh after the first
		start := time.Now()
		a.refresh()
		tt.Frame()
		report("untracked", "refresh", ms(time.Since(start)), "ms")
	}
}

// A large change scrolled through to its end, about a view's height every
// two frames, which shows every diff: how long that takes, and the memory
// held after, Go's heap and all the process's. Run alone, the memory is
// this test's, not the others' too.
func TestPerfMemory(t *testing.T) {
	a, tt := openPerf(t, "memory", perfRepo(t, "memory"))
	start := time.Now()
	for steps := 0; !a.diffList.AtEnd() && steps < 10_000; steps++ {
		tt.Scroll(800, 400, 0, 700)
		tt.Frame()
		tt.Frame()
	}
	report("memory", "load-all", ms(time.Since(start)), "ms")
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	report("memory", "heap", float64(m.HeapAlloc>>20), "MB")
	report("memory", "rss", rss(), "MB")
	runtime.KeepAlive(a)
}

// rss is the memory the process holds, as ps tells it, in MB.
func rss() float64 {
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	kb, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	return float64(kb) / 1024
}
