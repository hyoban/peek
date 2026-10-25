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

// The fork's native-sidebar-material branch, on MyGo's main, leaves
// Tooltip's tips to AppKit, which shows them as its own; scrolls as
// AppKit's scroll views do: overlay scroll bars, and content a touchpad
// pulls past its ends springing back; gives macOS's Services and Writing
// Tools the selected text, in AppKit's context menus of text; and shows
// AppKit's toolbar over the content's columns, the sidebars on its glass.
// Until MyGo does.
replace github.com/egoist/mygo => github.com/hyoban/mygo v0.6.4-0.20261025143937-85f300b38361
