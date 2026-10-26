#!/bin/bash
# Times Peek and Godiff (https://github.com/egoist/godiff) over the same
# repositories, the performance scenarios', and prints their measures side
# by side, as docs/performance.md shows them: each app's frames drawn
# without a window, by its tests, Godiff's at the commit below, its lines
# wrapped as Peek's are.
#
#   scripts/perf/compare.sh
#
# It clones Godiff, and makes the repositories, in a folder of its own,
# which it removes.
set -euo pipefail

godiff_commit=da918821a29bce3cc250df0598b6e2eccc141243
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Making the scenarios' repositories…" >&2
(cd "$root" && GOWORK=off go run ./scripts/perf/repos "$work/repos")

echo "Cloning Godiff…" >&2
git clone -q https://github.com/egoist/godiff "$work/godiff"
git -C "$work/godiff" checkout -q "$godiff_commit"
cp "$root/scripts/perf/godiff/perf_test.go" "$work/godiff/peekperf_test.go"

echo "Timing Peek…" >&2
(cd "$root" && PEEK_PERF=1 PEEK_PERF_REPOS="$work/repos" GOWORK=off \
	go test ./internal/app -run '^TestPerf' -v -count=1 -timeout 30m) | grep '^PERF' >"$work/peek.txt"

echo "Timing Godiff…" >&2
(cd "$work/godiff" && GODIFF_PERF_REPOS="$work/repos" GOWORK=off \
	go test -tags godiffperf -run '^TestPerf' -v -count=1 -timeout 30m .) | grep '^PERF' >"$work/godiff.txt"

# A measure taken twice, as refresh, keeps the second: the one every
# refresh after the first takes.
awk '
	{ key = $3 " " $4; if (!(key in unit)) order[n++] = key; value[$2, key] = $5; unit[key] = $6 }
	END {
		print "| Scenario | Measure | Peek | Godiff | Unit |"
		print "|---|---|--:|--:|---|"
		for (i = 0; i < n; i++) {
			split(order[i], k, " ")
			printf "| %s | %s | %s | %s | %s |\n", k[1], k[2], value["peek", order[i]], value["godiff", order[i]], unit[order[i]]
		}
	}
' "$work/peek.txt" "$work/godiff.txt"
