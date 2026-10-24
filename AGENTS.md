# Agent guide

## After every change

Once the tests pass, build the app again, put it in place of the installed
one and restart it, so the change can be tried in the app in use:

```bash
go tool mygo build -skip-dmg &&
  osascript -e 'tell application "/Applications/peek.app" to quit' &&
  while pgrep -qf /Applications/peek.app/Contents/MacOS/peek; do sleep 0.2; done &&
  rm -rf /Applications/peek.app &&
  ditto build/darwin-arm64/peek.app /Applications/peek.app &&
  open /Applications/peek.app
```

The app quits through AppleScript to keep what it saves on quitting.
