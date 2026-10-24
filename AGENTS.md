# Agent guide

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
