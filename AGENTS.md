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

Once the tests pass, build the app again, put it in place of the installed
one and restart it, so the change can be tried in the app in use:

```bash
go tool mygo build -skip-dmg &&
  osascript -e 'tell application "/Applications/Peek.app" to quit' &&
  while pgrep -qf /Applications/Peek.app/Contents/MacOS/Peek; do sleep 0.2; done &&
  rm -rf /Applications/Peek.app &&
  ditto build/darwin-arm64/Peek.app /Applications/Peek.app &&
  open /Applications/Peek.app
```

The app quits through AppleScript to keep what it saves on quitting.
