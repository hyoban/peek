#!/bin/bash
# Takes the README's screenshots of the installed Peek's window, into docs/:
# Changes and Files, in the light and the dark appearance, over a clone of
# Dify at the commit the README names, the window with its shadow over the
# desktop's wallpaper, as the appearance draws it.
#
#   scripts/screenshots.sh path/to/dify
#
# It needs Screen Recording for the app it runs in, to capture the window,
# and lets it control System Events, to switch the appearance. Peek's
# saved projects and window, and the appearance, are put back as they were.
set -euo pipefail

dify=${1:?usage: scripts/screenshots.sh path/to/dify}
commit=9dc53da
upstream=2d78a78 # 58 commits on, for the toolbar to offer them to pull
file=dify-agent-runtime/internal/agentcli/config.go
app=/Applications/Peek.app
docs=$(cd "$(dirname "$0")/.." && pwd)/docs
state="$HOME/Library/Application Support/Peek"

work=$(mktemp -d)
saved=$work/state
mkdir -p "$saved"

# dark switches the appearance, and waits for it to have.
dark() {
	osascript -e "tell application \"System Events\" to tell appearance preferences to set dark mode to $1"
	until [ "$(osascript -e 'tell application "System Events" to tell appearance preferences to get dark mode')" = "$1" ]; do sleep 0.2; done
	sleep 1
}

quit() {
	osascript -e "tell application \"$app\" to quit" 2>/dev/null || true
	while pgrep -qf "$app/Contents/MacOS/Peek"; do sleep 0.2; done
	sleep 1 # for Launch Services to see it gone
}

# launch opens the app, again where Launch Services is not ready for it.
launch() {
	for _ in 1 2 3 4 5; do
		open -a "$app" "$@" && return
		sleep 1
	done
	return 1
}

# Peek's saved projects and window, and the appearance, as they were.
was_dark=$(osascript -e 'tell application "System Events" to tell appearance preferences to get dark mode')
quit
cp "$state"/window-state.json "$state"/open "$state"/recent "$saved"/ 2>/dev/null || true
restore() {
	set +e
	quit
	cp "$saved"/* "$state"/ 2>/dev/null || true
	dark "$was_dark"
	rm -rf "$work"
	launch
}
trap restore EXIT

# The clone: main at the commit, tracking origin/main at upstream, origin
# the GitHub repository, and fetches failing at once, so that the toolbar
# shows the same commits to pull whenever the screenshots are taken.
git clone -q --no-checkout "$dify" "$work/dify"
git -C "$work/dify" checkout -q -B main "$commit"
git -C "$work/dify" remote set-url origin "$(git -C "$dify" remote get-url origin)"
git -C "$work/dify" update-ref refs/remotes/origin/main "$upstream"
git -C "$work/dify" branch -q -u origin/main
git -C "$work/dify" config core.sshCommand false

# The number of Peek's window on screen, its largest, or with "wallpaper"
# the desktop's wallpaper's.
cat >"$work/window.swift" <<'SWIFT'
import CoreGraphics
let all = CGWindowListCopyWindowInfo([.optionOnScreenOnly], kCGNullWindowID) as! [[String: Any]]
let wallpaper = CommandLine.arguments.count > 1
let peek = all.filter { w in
	wallpaper
		? ["Wallpaper", "Desktop Picture"].contains { (w["kCGWindowName"] as? String ?? "").hasPrefix($0) }
		: (w["kCGWindowOwnerName"] as? String) == "Peek" && (w["kCGWindowLayer"] as? Int) == 0
}
let area = { (w: [String: Any]) -> Double in
	let b = w["kCGWindowBounds"] as! [String: Double]
	return b["Width"]! * b["Height"]!
}
if let w = peek.max(by: { area($0) < area($1) }) { print(w["kCGWindowNumber"]!) }
SWIFT

# compose puts a window's capture, with its shadow, in the middle of the
# wallpaper, filling it, with a margin of points around it.
cat >"$work/compose.swift" <<'SWIFT'
import AppKit
let a = CommandLine.arguments
let window = NSImage(contentsOfFile: a[1])!, wall = NSImage(contentsOfFile: a[2])!
let rep = window.representations[0]
let scale = CGFloat(rep.pixelsWide) / window.size.width
let margin = CGFloat(Double(a[4])!) * scale
let w = CGFloat(rep.pixelsWide) + 2 * margin, h = CGFloat(rep.pixelsHigh) + 2 * margin
let out = NSBitmapImageRep(bitmapDataPlanes: nil, pixelsWide: Int(w), pixelsHigh: Int(h), bitsPerSample: 8,
	samplesPerPixel: 4, hasAlpha: true, isPlanar: false, colorSpaceName: .deviceRGB, bytesPerRow: 0, bitsPerPixel: 0)!
NSGraphicsContext.saveGraphicsState()
NSGraphicsContext.current = NSGraphicsContext(bitmapImageRep: out)
let fill = max(w / wall.size.width, h / wall.size.height)
let ws = NSSize(width: wall.size.width * fill, height: wall.size.height * fill)
wall.draw(in: NSRect(x: (w - ws.width) / 2, y: (h - ws.height) / 2, width: ws.width, height: ws.height))
window.draw(in: NSRect(x: margin, y: margin, width: CGFloat(rep.pixelsWide), height: CGFloat(rep.pixelsHigh)))
NSGraphicsContext.restoreGraphicsState()
try! out.representation(using: .png, properties: [:])!.write(to: URL(fileURLWithPath: a[3]))
SWIFT

shoot() { # mode appearance
	quit
	# The project alone, in a window of 1280 by 800 points, or as wide as
	# Stage Manager leaves room for.
	printf '%s\n' "$work/dify" >"$state/open"
	printf '{"main": {"x": 100, "y": 60, "width": 1280, "height": 800}}\n' >"$state/window-state.json"
	# The commit, and in Files the file, which Changes shows from the top.
	local env=(--env PEEK_SHOT_COMMIT="$commit" --env PEEK_SHOT_MODE="$1")
	if [ "$1" = files ]; then env+=(--env PEEK_SHOT_FILE="$file"); fi
	launch "${env[@]}"
	sleep 10 # the history, the diffs and the avatars
	launch # in front, drawn as active
	sleep 1
	screencapture -x -l "$(swift "$work/window.swift")" "$work/window.png"
	swift "$work/compose.swift" "$work/window.png" "$work/wallpaper.png" "$docs/$1-$2.png" 60
	echo "$docs/$1-$2.png"
}

for look in light dark; do
	if [ "$look" = dark ]; then dark true; else dark false; fi
	screencapture -x -o -l "$(swift "$work/window.swift" wallpaper)" "$work/wallpaper.png"
	shoot changes "$look"
	shoot files "$look"
done
