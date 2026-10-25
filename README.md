# Peek

A native git diff viewer for macOS, built in Go with [MyGo](https://github.com/egoist/mygo).

Open a repository and peek at its history: what each commit changed, the uncommitted changes in the work tree, and every file as the commit has it. The window refreshes as the repository changes.

## Changes

The files the chosen commit changes, with their diffs stacked one after another, and the history at the right.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/changes-dark.png">
  <img alt="Changes mode: the changed files, their diffs and the history" src="docs/changes-light.png">
</picture>

## Files

All of the commit's files, to read any of them as it was then.

<picture>
  <source media="(prefers-color-scheme: dark)" srcset="docs/files-dark.png">
  <img alt="Files mode: the commit's files and the text of one of them" src="docs/files-light.png">
</picture>

## Features

- The uncommitted changes always at the top of the history
- Syntax-highlighted diffs, with unmodified lines folded away
- File icons from [file-icons](https://github.com/file-icons/atom)
- Authors' avatars, and a link to the repository on GitHub
- Switch between projects in one window
- Commit all changes with a Codex-generated message and push from the toolbar
- Follows the system's light and dark appearance

## Shortcuts

| Shortcut | Action |
| --- | --- |
| <kbd>⌘</kbd> <kbd>O</kbd> | Open a folder |
| <kbd>⌘</kbd> <kbd>1</kbd> | Changes mode |
| <kbd>⌘</kbd> <kbd>2</kbd> | Files mode |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>S</kbd> | Show or hide the sidebar |
| <kbd>⌃</kbd> <kbd>⌘</kbd> <kbd>I</kbd> | Show or hide the history |

## Usage

Requires Go 1.27 or later, and `git` on your `PATH`.

```bash
go run . path/to/repository
```

Without a path, Peek opens the repository you are in, or the last one it opened.

### Push with Codex

Install the Codex CLI and sign in with `codex login`. Click **Push** in the
toolbar to stage all changes, including untracked files (Git ignores still
apply). Peek supplies the repository, HEAD and fixed staged tree IDs. Codex
reads the staged changes and recent commit subjects itself in a read-only sandbox,
following the repository's commit style. This uses your configured Codex provider
and existing settings; file contents are not embedded in the prompt. Before
committing, Peek verifies that the branch, HEAD and staged tree are unchanged.

Peek fetches the remote, commits, then pushes the current branch. If the remote
explicitly rejects it because the branch is protected, Peek creates and switches
to a `peek/push-…` branch and pushes that instead. A detached HEAD also gets a new
branch. With no new changes, Push sends existing commits without calling Codex.
Git's push-remote settings are respected; otherwise Peek uses `origin` or the
only configured remote. No pull request is created.

The Push button shows progress and the result; hover it for details. Click it
while running to cancel, or after a failure to retry. Authentication, network and
non-fast-forward failures stop the operation; Peek does not force-push or
automatically merge. Failed attempts retain staged changes and any local commit
for a retry. Commit message generation has no assembled-diff size limit.

### Review with Codex

Click **Review** to review staged, unstaged and untracked changes. When the
working tree is clean, it reviews commits not yet pushed to the configured
push destination, using locally known remote refs. A new branch is compared
with the remote's default branch (or `main` / `master` when needed).

While running, the button shows the current stage (starting, inspecting,
reviewing or checking) and elapsed time. Hover for the current operation.
Stages follow live Codex events rather than an estimated percentage.
When finished, it shows **Passed**, **Not passed**, or **Review error**.
Click a completed result to read the summary and findings, or to review again.
A failed review automatically copies its summary and findings to the clipboard.
Click while running to cancel. Changing the reviewed code makes the result
out of date. With no changes to review, the button is disabled.

Review uses your existing Codex login and settings. Peek supplies the repository,
review scope and fixed commit IDs; Codex reads the Git diffs and relevant files
itself in a read-only sandbox, using your configured provider. File contents
and binary assets are not embedded in the prompt. Peek fingerprints changed
files with streaming hashes to detect outdated reviews.
It leaves the index and working tree unchanged. Review is independent of Push;
a failed review does not prevent a later push. The two operations cannot run
at the same time. Review has no assembled-diff size limit; Git metadata output
is still bounded. Dirty submodules must be committed or cleaned before
reviewing, including untracked files inside them.

To build `Peek.app` and a `.dmg` into `build/`:

```bash
go tool mygo build
```

## Development

```bash
CGO_ENABLED=0 go test ./...
```

The screenshots above are drawn by `TestSnapshot` over this repository:

```bash
PEEK_REPO=. PEEK_OUT=docs/changes PEEK_COMMIT=3 go test -run 'Snapshot$'
```

```bash
PEEK_REPO=. PEEK_OUT=docs/files PEEK_MODE=files PEEK_COMMIT=1 PEEK_FILE=main.go go test -run 'Snapshot$'
```

## License

[MIT](LICENSE). The icon fonts in [fileicons/fonts](fileicons/fonts) keep their own licenses, listed in its [NOTICE](fileicons/fonts/NOTICE).
