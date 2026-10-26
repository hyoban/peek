// Command repos makes the repositories of the performance scenarios in
// the folder given, one folder a scenario, for the apps compared to run
// over the same ones (scripts/perf/compare.sh).
//
//	go run ./scripts/perf/repos path/to/folder
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"peek/internal/testutil"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: go run ./scripts/perf/repos path/to/folder")
		os.Exit(2)
	}
	for _, name := range testutil.Scenarios {
		if err := testutil.MakeScenario(filepath.Join(os.Args[1], name), name); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}
