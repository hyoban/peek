module peek

go 1.27.3

tool github.com/egoist/mygo/cmd/mygo

require (
	github.com/egoist/mygo v0.6.4
	golang.org/x/net v0.59.0
)

require (
	github.com/ProtonMail/go-crypto v1.4.1 // indirect
	github.com/cloudflare/circl v1.6.2 // indirect
	github.com/ebitengine/purego v0.11.1 // indirect
	github.com/go-text/typesetting v0.3.5 // indirect
	github.com/klauspost/compress v1.18.6 // indirect
	github.com/ulikunitz/xz v0.5.15 // indirect
	go.digitalxero.dev/rpm v0.3.4 // indirect
	golang.org/x/crypto v0.57.0 // indirect
	golang.org/x/image v0.46.0 // indirect
	golang.org/x/sys v0.48.0 // indirect
	golang.org/x/text v0.43.0 // indirect
)

// The fork's native-controls branch, on its native-source-list branch,
// on its native-sidebar-material branch, on MyGo's main, shows AppKit's
// check boxes, pop-up buttons, text fields (NativeCheckbox,
// NativePopUpButton, NativeTextField) and preference toolbars, as a
// settings window's; lets diffs ignore whitespace and collapse the
// lines expanded again, the separator clicked staying where it is; leaves Tooltip's tips to AppKit, which shows
// them as its own; scrolls as AppKit's scroll views do: overlay scroll
// bars, and content a touchpad pulls past its ends springing back; gives
// macOS's Services and Writing Tools the selected text, in AppKit's
// context menus of text; shows AppKit's toolbar over the content's
// columns, the sidebars on its glass; and shows AppKit's outline views
// (SourceList), flat or not, their rows' titles after a muted prefix and
// subtitles after a picture, search field (NativeSearchField) and split
// view, whose dividers drag and whose sidebars slide, under the content.
// Until MyGo does.
replace github.com/egoist/mygo => github.com/hyoban/mygo v0.7.2-0.20261112042456-af6fb4c4647a
