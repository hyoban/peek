# Agent guide

## Layout

`main.go` only starts `internal/app`, the window. Beside it, under
`internal/`: `git` runs git and gh, `codex` runs the Codex CLI, `review`
builds Codex reviews and explanations on both, `fileicons` draws file icons, `filekind`
tells code from tests, docs and generated files, and `testutil`
holds the repositories and fake CLIs the tests share.

## Design

Follow Apple's design for native Mac apps: the window, controls, menus and
behavior should look and work as macOS's own do.

## After every change

Once the tests pass, build Peek Dev, the development build, put it in place
of the installed one and restart it, so the change can be tried in the app
in use. Peek Dev installs beside Peek, the release, which updates itself
and is left alone:

```bash
PEEK_DEV=1 go tool mygo build -skip-dmg &&
  osascript -e 'tell application "/Applications/Peek Dev.app" to quit' &&
  while pgrep -qf "/Applications/Peek Dev.app/Contents/MacOS/"; do sleep 0.2; done &&
  rm -rf "/Applications/Peek Dev.app" &&
  ditto "build/darwin-arm64/Peek Dev.app" "/Applications/Peek Dev.app" &&
  env -i HOME="$HOME" USER="$USER" LOGNAME="$LOGNAME" open "/Applications/Peek Dev.app"
```

The app quits through AppleScript to keep what it saves on quitting, and
opens with the environment of an app opened from the Dock, not the
agent's: its terminal's shells inherit it, and tools that find an agent
there (`AI_AGENT`, `CLAUDECODE`) change how they run, as `astro dev`
detaches.

## Commits

Write commit messages as [Conventional Commits](https://www.conventionalcommits.org):
a type, an optional scope, and a summary in the imperative, such as
`feat(review): explain a change in its file` or `fix: keep the selection
after refreshing`. A commit that fixes an issue ends its summary with the
issue's number and closes it in the body, as `fix(app): refresh cached
avatars once they are old (#4)` with `Closes #4` below: the release notes
link only the numbers in the summary, and a bare `(#4)` doesn't close the
issue. The release notes list the commits since the last tag by
type: `feat` under Features, `fix` under Bug Fixes and `perf` under
Performance, and leave out the others, such as `refactor`, `docs`, `test`,
`ci` and `chore`. Mark a breaking change with `!`, as `feat!:`. A release
is a commit `chore: release v0.2.0` that raises the version in
`mygo.config.ts`, tagged and pushed, which the release workflow builds and
publishes. bumpp makes it, as `bump.config.ts` sets it up, choosing the
version from the commits since the last tag: a breaking change raises the
major, a `feat` the minor, anything else the patch:

```bash
npx bumpp@12
```
