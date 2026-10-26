#!/bin/bash
# Times Peek, Godiff (https://github.com/egoist/godiff) and Codiff
# (https://github.com/nkzw-tech/codiff) over the same repositories, the
# performance scenarios', and prints their measures side by side, as
# docs/performance.md shows them: Peek's and Godiff's frames drawn
# without a window, by their tests, and Codiff's in its window, by
# Electron, at the commits below, all their lines wrapped as Peek's are.
#
#   scripts/perf/compare.sh
#
# It clones Godiff and Codiff, and makes the repositories, in a folder of
# its own, which it removes. Codiff takes Node and pnpm to build.
set -euo pipefail

godiff_commit=da918821a29bce3cc250df0598b6e2eccc141243
codiff_commit=ab0653cfd0c1f44bc3ddd2cd749a6706aac09c75
root=$(cd "$(dirname "$0")/../.." && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

echo "Making the scenarios' repositories…" >&2
(cd "$root" && GOWORK=off go run ./scripts/perf/repos "$work/repos")

echo "Cloning Godiff…" >&2
git clone -q https://github.com/egoist/godiff "$work/godiff"
git -C "$work/godiff" checkout -q "$godiff_commit"
# Without the constraint that keeps it out of Peek's module.
grep -v '^//go:build ignore$' "$root/scripts/perf/godiff/perf_test.go" >"$work/godiff/peekperf_test.go"

echo "Cloning and building Codiff…" >&2
git clone -q https://github.com/nkzw-tech/codiff "$work/codiff"
git -C "$work/codiff" checkout -q "$codiff_commit"
(cd "$work/codiff" && pnpm install --frozen-lockfile >/dev/null && pnpm run build >/dev/null)

# gotest runs a Go app's performance tests in dir: the memory scenario's
# in a process of its own, for the memory it reports to be its own.
gotest() {
	local dir=$1
	shift
	(cd "$dir" && env "$@" GOWORK=off go test ${pkg:-.} -run '^TestPerf(Large|Many|Untracked)$' -v -count=1 -timeout 30m)
	(cd "$dir" && env "$@" GOWORK=off go test ${pkg:-.} -run '^TestPerfMemory$' -v -count=1 -timeout 30m)
}

echo "Timing Peek…" >&2
pkg=./internal/app gotest "$root" PEEK_PERF=1 PEEK_PERF_REPOS="$work/repos" | grep '^PERF' >"$work/peek.txt"

echo "Timing Godiff…" >&2
gotest "$work/godiff" GODIFF_PERF_REPOS="$work/repos" | grep '^PERF' >"$work/godiff.txt"

echo "Timing Codiff…" >&2
node "$root/scripts/perf/codiff/run.mjs" "$work/codiff" "$work/repos" | grep '^PERF' >"$work/codiff.txt"

# A measure taken twice, as refresh, keeps the second: the one every
# refresh after the first takes. One an app does not take shows as –.
awk '
	{ key = $3 " " $4; if (!(key in unit)) order[n++] = key; value[$2, key] = $5; unit[key] = $6 }
	END {
		print "| Scenario | Measure | Peek | Godiff | Codiff | Unit |"
		print "|---|---|--:|--:|--:|---|"
		for (i = 0; i < n; i++) {
			split(order[i], k, " ")
			printf "| %s | %s", k[1], k[2]
			for (a = 1; a <= 3; a++) {
				app = a == 1 ? "peek" : a == 2 ? "godiff" : "codiff"
				v = ((app, order[i]) in value) ? value[app, order[i]] : "–"
				printf " | %s", v
			}
			printf " | %s |\n", unit[order[i]]
		}
	}
' "$work/peek.txt" "$work/godiff.txt" "$work/codiff.txt"
