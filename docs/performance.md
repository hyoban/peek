# Performance

How fast Peek opens a project, scrolls its diffs and follows its files,
what slowed it, what was done about it, and how it compares with
[Godiff](https://github.com/egoist/godiff), another Mac app for reviewing
Git changes built on [MyGo](https://mygo.egoist.dev/).

## Measuring

The performance tests in [`internal/app/perf_test.go`](../internal/app/perf_test.go)
open the app over a scenario's repository and draw its frames without a
window, as the other tests do, timing them. They print a line a measure:

```bash
PEEK_PERF=1 go test ./internal/app -run '^TestPerf' -v
```

```
PERF peek large scroll-unified 1.6 ms/frame
```

The scenarios ([`internal/testutil/scenarios.go`](../internal/testutil/scenarios.go))
are repositories of a commit and changes on top of it, the same wherever
they are made:

| Scenario | What it changes |
|---|---|
| large | a file of code of 20,000 lines (1.7 MB), a line changed every 97 |
| many | 200 files of 200 lines, each changed in 5 places |
| untracked | 20,000 small files git does not track, in 200 folders |
| memory | 300 files of 5,000 lines, a line added every 200 |

And the measures:

| Measure | What it times |
|---|---|
| open | from the app opening the project to its first frame, the diffs in view shown |
| scroll | a frame, scrolling down 120 points a frame, unified and side by side |
| resize | a frame, the window widening 4 points a frame over the large diff |
| refresh | reading the project again, as a change of its files asks, after the first time |
| load-all | every diff loaded, as scrolling through them all loads them |
| heap | the memory held once they are, after a collection |

A frame here is built, laid out and painted, its pixels drawn by the CPU:
on the screen the GPU draws them, and frames take less. The measures are
for comparing, one with another, more than as the times a user sees.
Measures taken again vary by about a fifth.

All the numbers below were taken on an M4 Pro with 12 cores and 24 GB
running macOS 27.0.1, with Go 1.27.3.

## What slowed Peek, and what was done

Measured by the same tests at `4a58eb4`, before, and now:

| Scenario | Measure | Before | Now | |
|---|---|--:|--:|---|
| large | open | 222 | 156 | ms |
| large | scroll, unified | 5.8 | 1.6 | ms/frame |
| large | scroll, side by side | 5.8 | 1.6 | ms/frame |
| large | resize, unified | 31.7 | 4.4 | ms/frame |
| large | resize, side by side | 46.3 | 6.0 | ms/frame |
| untracked | open | 1,143 | 410 | ms |
| untracked | refresh | 1,166 | 106 | ms |
| memory | load-all | 9,220 | 6,133 | ms |
| memory | heap | 311 | 136 | MB |

### Diffs of long files

Each file's diff is as high as its lines, in the list of the commit's
diffs, and the editor took all its lines for in view: every frame walked
every line of a long file's diff, and built every separator of the
unchanged lines it folds. It now builds those near what it last painted
showed, a view's height to either side.

A side-by-side diff just out of view built its sides at one width and its
row drew them at another, so each frame measured every line's wraps
again: a 40,000-line file's took 12 s of the CPU over a test's frames.
They now build at the width they were laid out at.

At each width the window is resized through, a diff measured the wraps of
all its lines, those folded away too. It measures those it shows, and the
others once they show. And to align its sides it laid out every changed
line with the system's text layout, most of them out of view: it counts
their rows as the layout would, with the same widths of characters, and
lays out only the lines it draws.

These were in MyGo's editor, in the fork Peek builds with
(`github.com/hyoban/mygo`, branch `native-source-list`).

### Opening a project

On a clone of [Dify](https://github.com/langgenius/dify), reading the
history, the changes and the review's snapshot took 300 ms, and takes 170.
Asking whether the branch has commits not pushed, and finding the branch
it is to merge into, ran a git process for each key of the configuration
and each ref asked about, one after another: they read the configuration
and the refs once, and what does not depend on another runs at once,
which took finding the branch from 60 ms to 10. The review's snapshot
asked again, after `git status`, and now shares the answer. The file
icons, 75 ms, load while the app starts rather than before.

### Following the files

A build writing into a folder git ignores read the whole history again
every two seconds: changes only in such folders are left. Each refresh
read every file changed again, to fingerprint the changes for the review,
and added every untracked file to an index, to count its lines: a file
that has not changed since keeps its hash and its counts, unless it
changed within the last second, as git takes its own index's entries.

The first time, the lines of untracked files no attribute applies to are
counted here, as git counts a new file's, rather than by adding the files
to an index; those an attribute may filter or convert, and what is not a
file, still go to git. And the counts and the review's fingerprint read
each file once between them, a few at once: opened by many at once,
each file took several times as long.

### Large files and many diffs

A file too large to show, over 8 MB, is no longer read whole only to be
left. A file's two sides read at once. Buffers are made, and the texts
shown compared with those read again, off the main thread. Past a million
lines loaded, the diffs farthest from the view let theirs go, and load
again as they come back into view.

## Peek and Godiff

[`scripts/perf/compare.sh`](../scripts/perf/compare.sh) makes the
scenarios' repositories, clones Godiff at `da91882` (October 15, 2026),
copies [Godiff's side of the tests](../scripts/perf/godiff/perf_test.go)
into it, times both apps over the same repositories, and prints this
table:

| Scenario | Measure | Peek | Godiff | |
|---|---|--:|--:|---|
| large | open | 156 | 121 | ms |
| large | scroll, unified | 1.6 | 2.4 | ms/frame |
| large | scroll, side by side | 1.6 | 2.5 | ms/frame |
| large | resize, unified | 4.4 | 1.9 | ms/frame |
| large | resize, side by side | 6.0 | 1.9 | ms/frame |
| many | open | 233 | 1,424 | ms |
| many | scroll, unified | 2.7 | 2.8 | ms/frame |
| many | scroll, side by side | 2.0 | 2.9 | ms/frame |
| untracked | open | 410 | 259 | ms |
| untracked | refresh | 106 | 176 | ms |
| memory | open | 576 | 46,954 | ms |
| memory | load-all | 6,133 | 46,955 | ms |
| memory | heap | 136 | 645 | MB |

Opening the untracked scenario took a second, now and then, in either
app, right after other work had filled the system's caches; the table
has the time it took most often.

Godiff's lines wrap here, as Peek's always do; it does not wrap them by
default, which costs it less. It is built on MyGo 0.4.9, an earlier
version than Peek's fork, and its frames are drawn by its own rows of
text, not MyGo's editor.

The two load diffs differently. Godiff reads every file's contents as it
opens, and colors them with [Chroma](https://github.com/alecthomas/chroma),
two files at a time: once it has, scrolling never waits, but opening a
change waits for all of it, a large change's for long, and holds all of
it. Peek reads the diffs in view, and those that come into view as it
scrolls, colored by MyGo's highlighter as they show; its list builds the
rows just past the view too, so a diff starts loading before it comes
into view, but a project's every diff is not ready at once. Godiff shows the
whole text of files of 2 MB at most, and of larger ones the patch's
lines; Peek of files of 8 MB at most.

Where Godiff is faster:

- **Resizing**, two to three times as fast. At each width Peek wraps the
  lines it shows again, and lays out and draws them anew, a frame's work
  that a resize cannot spare; a tenth of it is drawing the pixels on the
  CPU, which the GPU does on the screen.
- **Opening a project with many untracked files**, the first time, though
  it does less: it lists 1,000 untracked files at most, the others as a
  row telling how many more, where Peek lists all 20,000, reads each, and
  asks git which an attribute applies to. Every refresh after reuses the
  counts, and is faster than Godiff's.
- **Opening a long file's diff**, a little: Peek diffs the two texts
  itself, off the main thread, 35 ms of it, where Godiff takes git's
  patch.

Where Peek is faster: opening a change of many files, six times as fast;
of many long files, 80 times; scrolling, its frames as long as Godiff's
or shorter, down to two thirds of them; refreshing a project with many untracked files; and
it holds a fifth of the memory once every diff is loaded.

## Measuring again

Run the tests after a change that might slow the app, and compare with
the tables above:

```bash
PEEK_PERF=1 go test ./internal/app -run '^TestPerf' -v
scripts/perf/compare.sh
```

A measure that grew is worth a profile, `-cpuprofile`, before a guess:

```bash
PEEK_PERF=1 go test ./internal/app -run '^TestPerfLarge$' -cpuprofile cpu.out
go tool pprof -top -cum app.test cpu.out
```
