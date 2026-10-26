module peek

go 1.27.3

tool github.com/egoist/mygo/cmd/mygo

require (
	github.com/egoist/mygo v0.6.4
	golang.org/x/net v0.59.0
)

require (
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// The fork's native-source-list branch, on its native-sidebar-material
// branch, on MyGo's main, leaves Tooltip's tips to AppKit, which shows
// them as its own; scrolls as AppKit's scroll views do: overlay scroll
// bars, and content a touchpad pulls past its ends springing back; gives
// macOS's Services and Writing Tools the selected text, in AppKit's
// context menus of text; shows AppKit's toolbar over the content's
// columns, the sidebars on its glass; and shows AppKit's outline views
// (SourceList), search field (NativeSearchField) and split view, whose
// dividers drag and whose sidebars slide, under the content. Until MyGo
// does.
replace github.com/egoist/mygo => github.com/hyoban/mygo v0.6.4-0.20261026035041-c137120d3e6c
