# Peek

A native git diff viewer for macOS, built in Go with [MyGo](https://github.com/egoist/mygo).

Browse uncommitted changes, compare a branch, or read any commit's files. Follow definitions in the working tree, ask Codex about the code, and commit and push from the same window. Peek refreshes as repositories change and remembers where you left each one.

## Changes

Read the selected commit, uncommitted changes, or branch comparison as a stack of diffs, with changed files on the left and history on the right. Switch between unified and side-by-side views, mark files as viewed, and step through changes across files.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/changes-dark.png">
  <img alt="Changes mode: the changed files, their diffs and the history" src="docs/changes-light.png">
</picture>

## Files

Browse every file at the selected commit or in the working tree. Related files are nested together; images and Markdown have previews, and Go, TypeScript and Kotlin files in the working tree support definition lookup.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/files-dark.png">
  <img alt="Files mode: the commit's files and the text of one of them" src="docs/files-light.png">
</picture>

## Install

Peek runs on Macs with Apple silicon or Intel, on macOS 13 or later.
Install the latest release into `/Applications`:

```bash
curl -fsSL https://github.com/hyoban/peek/releases/latest/download/install.sh | sh
```

Run the same command again to reinstall. Once installed, Peek checks for new
versions in the background and offers to install them; use
**Peek → Check for Updates…** to check now. Updates are signed, and Peek
installs only those signed with the project's key.

Releases are not signed with an Apple Developer ID or notarized. The install
script avoids Gatekeeper's warning, because files downloaded with `curl` are
not quarantined. If you download the `.dmg` from
[Releases](https://github.com/hyoban/peek/releases) in a browser instead,
macOS blocks Peek the first time it opens. After moving it to Applications,
allow it in **System Settings → Privacy & Security → Open Anyway**, or run:

```bash
xattr -d com.apple.quarantine /Applications/Peek.app
```

## Get started

To run from source, use macOS, Go 1.27.3 or later (as specified in
[go.mod](go.mod)), and `git` on your `PATH`. From this checkout:

```bash
go run . path/to/repository
```

Without a path, Peek opens the repository you are in, or restores the
repositories open when it last quit. Use **File → Open Folder** (⌘O) to open
another repository, and the project name in the toolbar to switch between them.

### Command line

**Peek → Install Command Line Tool…** links `peek` into `/usr/local/bin`,
asking for an administrator's password if needed. Then open a repository
from a terminal in the running app:

```bash
peek path/to/repository
```

Without a path, `peek` opens the repository you are in. Peek Dev installs
`peek-dev`.

Build `Peek.app` and a `.dmg` into `build/`:

```bash
go tool mygo build
```

Use `go tool mygo build -skip-dmg` to build just the app.

`PEEK_DEV=1 go tool mygo build -skip-dmg` builds Peek Dev instead: an app
with an orange icon that installs beside Peek, keeps its own state, and does
not update itself.

Optional tools enable additional features:

| Tool | Used for |
| --- | --- |
| Codex CLI, signed in with `codex login` | Conversations, reviews, explanations and commit messages |
| [GitHub CLI](https://cli.github.com), authenticated for the repository | Checking out pull requests by number or URL |
| Local language servers | [Go to definition](#go-to-definition) in Go, TypeScript and Kotlin |

## Features

### Read and navigate

- Uncommitted changes at the top of the history, alongside commits and branch comparisons.
- Changed files grouped into code, tests, docs and generated files, with added and deleted line counts. Tests, docs and generated files start folded as viewed; selecting one opens its diff.
- Syntax highlighting, folded unchanged lines and imports, unified or side-by-side diffs, and a patch fallback for files too large to display whole.
- File-path filtering, in-file search, Next / Previous Change, and Back / Forward navigation between definitions.
- Related files nested in **Files** using bundled [antfu/vscode-file-nesting-config](https://github.com/antfu/vscode-file-nesting-config) rules. Expand a file's arrow for its companions, or select its name to read it. Nesting works offline, within each directory.
- Image and Markdown previews, with a source toggle for Markdown and support for repository images.
- File and folder icons from the active VS Code icon theme when it provides SVGs, with [file-icons](https://github.com/file-icons/atom) as a fallback.

### Discuss and ship

- Select line numbers to copy code references or GitHub links. Add comments from the gutter and copy them as an agent prompt.
- **Ask Codex** about the selected changes, starting from Explain, Review, Summarize or Suggest Tests, or your own question. Conversations are saved per repository and support follow-up questions.
- Review findings appear under the relevant diff lines, with the full result in the conversation.
- **Ship** stages all changes, asks Codex for a commit message following the repository's style, commits and pushes.
- Switch branches or check out a pull request from the branch name in the toolbar.
- Browse author and co-author avatars, commit details, linked issue and pull request references, and the branch's GitHub page or pull request.

### Keep repositories close

- One shared window with per-repository state, restored on the next launch.
- Background fetches, incoming commit counts, and one-click fast-forward pulls. Fetches pause while offline or on a metered connection.
- **Show in Menu Bar** replaces the Dock icon with a menu of recent repositories, changed-file counts and commits to pull; right-click its icon to bring Peek forward.
- **Hide When Inactive** hides Peek when another app comes forward. Incoming commits are also counted on the Dock icon when it is shown.
- **Peek → Settings…** (⌘,) holds these choices and more, in panes as Xcode's: opening at login, what opens at launch and notifications (General), file nesting and folding tests, docs and generated files (Navigation), the code's font and size, the first diff layout, ignoring whitespace in diffs, and line wrapping and import folding in Files (Text Editing), whether and how often remotes are fetched, metered connections included (Source Control), and the model Ship writes commit messages with and the lowest priority that fails a review (Intelligence).
- Notifications when shipping or a review finishes in the background, native macOS menus and controls, and system light and dark appearance.

## Shortcuts

| Shortcut | Action |
| --- | --- |
| <kbd>⌘</kbd> <kbd>,</kbd> | Settings |
| <kbd>⌘</kbd> <kbd>O</kbd> | Open a folder |
| <kbd>⌘</kbd> <kbd>1</kbd> | Changes mode |
| <kbd>⌘</kbd> <kbd>2</kbd> | Files mode |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>S</kbd> | Show or hide the sidebar |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>I</kbd> | Show or hide the history |
| <kbd>⌥</kbd> <kbd>⌘</kbd> <kbd>V</kbd> | Mark the chosen file's diff as viewed |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>D</kbd> | Show the diffs side by side or unified |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>↓</kbd> / <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>↑</kbd> | Next / previous change |
| <kbd>⌘</kbd> <kbd>F</kbd> | Find in the file |
| <kbd>⌘</kbd> click / <kbd>F12</kbd> | Go to definition (working tree, Go, TypeScript and Kotlin) |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>←</kbd> / <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>→</kbd> | Go back / forward |
| <kbd>⌘</kbd> <kbd>G</kbd> / <kbd>⇧</kbd> <kbd>⌘</kbd> <kbd>G</kbd> | Find next / previous |
| <kbd>⌘</kbd> <kbd>E</kbd> | Use the selection for Find |
| <kbd>⌥</kbd> <kbd>⌘</kbd> <kbd>J</kbd> | Filter the sidebar's files |
| <kbd>⌘</kbd> <kbd>R</kbd> | Refresh |
| <kbd>⌘</kbd> <kbd>B</kbd> | Switch branch or check out a pull request |
| <kbd>⇧</kbd> <kbd>⌘</kbd> <kbd>P</kbd> | Pull |
| <kbd>⌘</kbd> <kbd>P</kbd> | Ship |
| <kbd>⇧</kbd> <kbd>⌘</kbd> <kbd>A</kbd> | New Codex conversation |
| <kbd>⇧</kbd> <kbd>⌘</kbd> <kbd>R</kbd> | Review changes |
| <kbd>⇧</kbd> <kbd>⌘</kbd> <kbd>E</kbd> | Explain changes |
| <kbd>⌥</kbd> <kbd>⌘</kbd> <kbd>R</kbd> | Show the repository in Finder |

## Usage

### Go to definition

In **Files**, choose **Uncommitted Changes** in the history, then ⌘-click a
function, type or variable, or use **Navigate → Go to Definition** (F12).
Hold ⌘ over an identifier to underline it and show a pointing hand before
clicking. This offers a lookup; the language server resolves its definition.
Go, TypeScript (`.ts`, `.tsx`, `.mts`, `.cts`) and Kotlin (`.kt`, `.kts`)
use local language servers:

```bash
go install golang.org/x/tools/gopls@latest
npm install -g typescript-language-server typescript@6
brew install JetBrains/utils/kotlin-lsp
```

The TypeScript language server currently uses TypeScript 6's `tsserver`;
TypeScript 7 does not provide that server. Peek prefers a language server in
the repository's `node_modules/.bin`, then searches `PATH`. It also looks in
`~/.local/bin` and `~/go/bin`. Node.js and Go must be available to the servers.
Missing servers are reported when a definition is requested; Peek does not
download them automatically.

Kotlin uses [JetBrains' Kotlin Language Server](https://github.com/Kotlin/kotlin-lsp)
over stdio. Peek finds the enclosing Gradle or Maven project, including
builds in repository subdirectories. The first lookup waits for the server
to import and index it. The project's JDK and Gradle/Maven wrapper (or
installed build tool) must be available. Kotlin targets must be files on disk;
virtual or JAR source locations are not supported.
The server's Android support is experimental and Kotlin Multiplatform
support is still under development.

A single definition opens directly; multiple definitions offer a choice.
Standard libraries and dependency files open read-only, even outside the
repository. The arrows in the file bar and **Navigate → Go Back / Go Forward**
return to earlier positions. Historical commits and the branch comparison
do not offer definition lookup. Servers start on demand, restart after a
repository refresh to discard stale project information, and stop when the
project closes.

### Ask Codex

Click **Ask Codex** in the toolbar to enter a question. The **Shortcuts** menu
offers **Explain**, **Review**, **Summarize** and **Suggest Tests**; choose one
and edit its request before sending. The scope shown beside Send tells you
which changes the request concerns. Return sends the request; Shift-Return
adds a line break.

Answers stream into a conversation. Use the **Conversation Navigator** to
reopen saved conversations, continue with follow-up questions, copy a response
or a whole conversation, or delete a conversation. **Repository → New
Conversation** (⇧⌘A) starts a new one. Conversations are kept per repository
across launches.

Questions and explanations use the selected history entry: uncommitted
changes, a commit, or the branch comparison. **Repository → Explain Changes**
(⇧⌘E) starts an explanation directly. Codex reads the diffs and surrounding
code in a read-only sandbox using your existing login and settings. The toolbar
shows progress and lets you stop the current request.

### Review changes

Choose the **Review** template in Ask Codex, or use **Repository → Review
Changes** (⇧⌘R). When the branch comparison is selected, Peek reviews that
comparison. Otherwise it reviews staged, unstaged and untracked changes;
with a clean working tree, it reviews commits not yet pushed to the configured
push destination. A new branch is compared with the remote's default branch,
falling back to `main` or `master` when needed. Remote refs are those known locally.

The result appears in the conversation, and the toolbar shows **Passed**,
**Not Passed** or **Review Error**. By default any P0–P2 finding fails the review and P3
findings alone still pass; **Settings → Intelligence** changes where it fails. Findings also appear beside the affected diff lines.
You can copy the result or ask follow-up questions in the conversation.

Peek checks that the reviewed changes still match the snapshot. Changing them
makes the result stale and stops a review still running. Run Review Changes
again for the new version. Dirty submodules, including untracked files inside
them, must be committed or cleaned before reviewing.

Review leaves the index and working tree unchanged. It is independent of
Ship: a failed review does not prevent a later push, but review and shipping
cannot run at the same time.

### Ship changes

Click **Ship** in the toolbar (⌘P) to stage all changes, including untracked
files that Git does not ignore, write a commit message with Codex, commit and
push. Peek supplies the staged diff and recent commit subjects so the message
follows the repository's style. Commit messages use a separate small-model
configuration, whose model **Settings → Intelligence** can change; reviews
and conversations use your Codex settings.

Before committing, Peek verifies that the branch, HEAD and staged tree have
not changed. It fetches the remote before committing and pushing. If the remote
explicitly rejects the push because the branch is protected, Peek creates and
switches to a `peek/push-…` branch and pushes that instead. A detached HEAD also
gets a new branch. With no new changes, Ship pushes existing commits without
calling Codex. Git's push-remote settings are respected; otherwise Peek uses
`origin` or the only configured remote. Ship does not create a pull request.

The button shows progress and can cancel an operation in progress. Failures
show an alert; staged changes and any local commit are retained for a retry.
Authentication, network and non-fast-forward failures stop the operation.
Peek does not force-push or automatically merge.

## Development

```bash
go tool mygo vet .
CGO_ENABLED=0 go test ./...
```

With `gopls` and `typescript-language-server` installed, `PEEK_LSP_TEST=1 go test ./internal/definition`
also checks real cross-file, TypeScript path-alias and standard-library definitions.
With `kotlin-lsp`, Maven and a JDK installed,
`PEEK_KOTLIN_LSP_TEST=1 go test ./internal/definition` checks real Kotlin
cross-file function and type definitions, including UTF-16 and CRLF positions.

### Releases

Releases are published to GitHub, and installed copies of Peek update
themselves from the latest one. To publish one:

1. Raise `version` in [mygo.config.ts](mygo.config.ts) and commit it,
   as `chore: release v0.2.0`.
2. Push a tag of the version:

   ```bash
   git tag v0.2.0
   git push origin main v0.2.0
   ```

The [release workflow](.github/workflows/release.yml) checks that the tag
matches `version`, writes the release notes with
[changelogithub](https://github.com/antfu-collective/changelogithub) from
the [Conventional Commits](https://www.conventionalcommits.org) since the
last tag, which Peek's update window shows too, builds Peek for Apple
silicon and Intel, signs its updates, uploads the disk images, the updates
and the install script to the
release of the tag, and publishes it.

The workflow signs updates with the `MYGO_UPDATER_PRIVATE_KEY` secret of the
repository, the key that `go tool mygo keygen` wrote. Installed copies
accept only updates signed with it: keep a copy outside this Mac, and never
commit it. To release from this Mac instead, give the build the key and
publish the draft it uploads:

```bash
MYGO_UPDATER_PRIVATE_KEY="$(cat ~/Library/Application\ Support/mygo/update-keys/mygo-update.key)" go tool mygo build -platform darwin/arm64,darwin/amd64 -upload
```

### Screenshots

The screenshots show Peek browsing [Dify](https://github.com/langgenius/dify)
in light and dark appearance. Regenerate them in `docs/` using the installed
app and a local Dify clone:

```bash
scripts/screenshots.sh path/to/dify
```

The script creates a temporary clone at commit `9dc53da`, with `origin/main`
58 commits ahead, and captures Changes and Files (with
`dify-agent-runtime/internal/agentcli/config.go` selected). It requires Screen
Recording permission and control of System Events to switch appearance.
Peek's saved projects, window state and the system appearance are restored afterward.

### Performance

The performance tests time opening a project, scrolling its diffs, resizing the window and following its files, over repositories made for them, and `scripts/perf/compare.sh` times [Godiff](https://github.com/egoist/godiff) and [Codiff](https://github.com/nkzw-tech/codiff) over the same ones; [docs/performance.md](docs/performance.md) tells what they found:

```bash
PEEK_PERF=1 go test ./internal/app -run '^TestPerf' -v
scripts/perf/compare.sh
```

## License

[MIT](LICENSE). The icon fonts in [internal/fileicons/fonts](internal/fileicons/fonts) keep their own licenses, listed in its [NOTICE](internal/fileicons/fonts/NOTICE).
