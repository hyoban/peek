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
apply). Codex generates a commit message from the staged diff and recent
commit subjects, following the repository's commit style. This sends that
diff to your configured Codex provider and uses your existing Codex settings.

Peek fetches the remote, commits, then pushes the current branch. If the remote
explicitly rejects it because the branch is protected, Peek creates and switches
to a `peek/push-…` branch and pushes that instead. A detached HEAD also gets a new
branch. With no new changes, Push sends existing commits without calling Codex.
Git's push-remote settings are respected; otherwise Peek uses `origin` or the
only configured remote. No pull request is created.

Progress and errors appear below the toolbar. Authentication, network and
non-fast-forward failures stop the operation; Peek does not force-push or
automatically merge. Failed attempts retain staged changes and any local commit
for a retry. Hover the status to read the full error. Commit message generation
supports staged diffs up to 256 KiB; larger changes can be committed manually
before clicking Push.

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
