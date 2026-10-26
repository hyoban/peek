# Performance

How fast Peek opens a project, scrolls its diffs and follows its files,
what slowed it, what was done about it, and how it compares with two
other apps for reviewing Git changes: [Godiff](https://github.com/egoist/godiff),
built on [MyGo](https://mygo.egoist.dev/) as Peek is, and
[Codiff](https://github.com/nkzw-tech/codiff), which Godiff remakes, built
on Electron.

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
| load-all | scrolling through the change to its end, 700 points every two frames, which shows every diff |
| heap | Go's heap once it has, after a collection |
| rss | the memory the process holds then, as `ps` tells it; the memory scenario runs in a process of its own |

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
| large | open | 222 | 145 | ms |
| large | scroll, unified | 5.8 | 1.6 | ms/frame |
| large | scroll, side by side | 5.8 | 1.6 | ms/frame |
| large | resize, unified | 31.7 | 4.4 | ms/frame |
| large | resize, side by side | 46.3 | 6.0 | ms/frame |
| many | open | 213 | 90 | ms |
| untracked | open | 1,143 | 400 | ms |
| untracked | refresh | 1,166 | 96 | ms |
| memory | open | 612 | 519 | ms |
| memory | load-all | 28,572 | 15,365 | ms |
| memory | heap | 609 | 208 | MB |
| memory | rss | 1,132 | 473 | MB |

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
left. A file's two sides read at once, and those of commits through a few
`git cat-file` processes kept running, rather than a `git show` each: the
processes they took were most of the time a diff took to come, and
opening a change of many files takes less than half as long. Buffers are made, and the texts
shown compared with those read again, off the main thread. Past a million
lines loaded, the diffs farthest from the view let theirs go, and load
again as they come back into view.

## Peek, Godiff and Codiff

[`scripts/perf/compare.sh`](../scripts/perf/compare.sh) makes the
scenarios' repositories, clones Godiff at `da91882` (October 15, 2026) and
Codiff at `ab0653c` (October 23, 2026, version 1.18.2), times the three
over the same repositories, and prints this table:

| Scenario | Measure | Peek | Godiff | Codiff | |
|---|---|--:|--:|--:|---|
| large | open | 145 | 121 | 170 | ms |
| large | scroll, unified | 1.7 | 2.4 | 7.5 | ms/frame |
| large | scroll, side by side | 1.5 | 2.5 | 4.4 | ms/frame |
| large | resize, unified | 4.3 | 2.0 | 8.4 | ms/frame |
| large | resize, side by side | 5.6 | 1.9 | 8.1 | ms/frame |
| many | open | 90 | 1,399 | 268 | ms |
| many | scroll, unified | 1.9 | 2.7 | 5.3 | ms/frame |
| many | scroll, side by side | 1.9 | 2.8 | 4.4 | ms/frame |
| untracked | open | 400 | 239 | 497 | ms |
| untracked | refresh | 96 | 166 | – | ms |
| memory | open | 519 | 46,755 | 1,042 | ms |
| memory | load-all | 15,365 | 7,944 | 29,720 | ms |
| memory | heap | 208 | 772 | – | MB |
| memory | rss | 473 | 3,256 | 2,621 | MB |

All three wrap their lines here, as Peek always does; Godiff and Codiff
do not by default, which costs them less. Opening the untracked scenario
took a second, now and then, in any of the apps, right after other work
had filled the system's caches; the table has the time it took most
often.

### How each is measured

Godiff's side of the tests ([`godiff/perf_test.go`](../scripts/perf/godiff/perf_test.go))
is Peek's, copied into its checkout: its frames are drawn without a
window, as Peek's are. It is built on MyGo 0.4.9, an earlier version than
Peek's fork, and draws its own rows of text, not MyGo's editor.

Codiff is a page Chromium draws, which no test draws without a window.
[`codiff/run.mjs`](../scripts/perf/codiff/run.mjs) opens its window over
each repository, as Codiff's own startup benchmark does, its settings and
home its own, and [`codiff/harness.cjs`](../scripts/perf/codiff/harness.cjs)
takes the same measures in it: open from the window made to the diffs in
view drawn, or folded; a frame as the time between two of Chromium's,
which draws them as fast as it can rather than at the display's rate;
resizing as the window is made wider; and rss as the memory all of
Electron's processes hold. Its frames are drawn on the GPU, which makes
its times not quite the others'. It has no measure of a refresh: it tells
of files changed, and reads them again when asked.

### What they do differently

Godiff reads every file's contents as it opens, and colors them with
[Chroma](https://github.com/alecthomas/chroma), two files at a time: once
it has, scrolling never waits, but opening a change waits for all of it,
a large change's for long, and holds all of it, more than 3 GB of the
memory scenario's. Codiff reads the change as it opens too, and draws
only the files in view, in a list as high as the whole change. Peek reads
the diffs in view, and those its list builds just past it, as it
scrolls, and lets the farthest go past a million lines: it holds less
than a sixth of Godiff's memory and a fifth of Codiff's once every
diff has shown, and opens a large change 90 times as fast as Godiff and
twice as fast as Codiff, and a change of many files 15 times and 3 times
as fast. Its load-all reads and diffs
each file as it comes, which the app does off the main thread and the
test in its frames; Godiff's did as it opened.

Godiff and Codiff list 1,000 untracked files at most, the others as a row
telling how many more, where Peek lists all 20,000. Godiff shows the
whole text of files of 2 MB at most, and of larger ones the patch's lines;
Peek of files of 8 MB at most.

Where Godiff is faster: **resizing**, two to three times as fast, where
Peek wraps the lines it shows again at each width, and lays them out and
draws them anew; **opening a project with many untracked files** the
first time, though it lists a twentieth of them; **opening a long file's
diff**, a little, as Peek diffs the two texts itself, off the main thread,
35 ms of it, where Godiff takes git's patch; and **scrolling through a
change already loaded**, as its loading was done as it opened.

Codiff is slower than Peek at every measure here, its frames from 1.4 to
4.4 times as long, opening the project of many untracked files too,
though it lists a twentieth of them.

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
