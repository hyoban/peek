package main

import "github.com/egoist/mygo/ui"

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	historyIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M6 2.5v11"/>`)
	filesIcon    = icon(`<path d="M2.5 2.5v9.5a1 1 0 0 0 1 1H6"/><path d="M2.5 6H6"/><rect x="7.5" y="4" width="6" height="4" rx="1"/><rect x="7.5" y="10" width="6" height="4" rx="1"/>`)
	folderIcon   = icon(`<path d="M1.5 4.5a1 1 0 0 1 1-1h3.6l1.4 1.5h5.9a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1h-10.9a1 1 0 0 1-1-1z"/>`)
	worktreeIcon = icon(`<path d="M10.5 2.5l3 3-7.5 7.5H3v-3z"/><path d="M8.5 4.5l3 3"/>`)
	changesIcon  = icon(`<path d="M9.5 1.5h-5a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1V4.5z"/><path d="M8 5.5v4M6 7.5h4M6 11.5h4"/>`)
	browseIcon   = icon(`<path d="M5.5 4.5L2 8l3.5 3.5M10.5 4.5L14 8l-3.5 3.5"/>`)
)
