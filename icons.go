package main

import "github.com/egoist/mygo/ui"

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	historyIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M10 2.5v11"/>`)
	filesIcon    = icon(`<path d="M2.5 2.5v9.5a1 1 0 0 0 1 1H6"/><path d="M2.5 6H6"/><rect x="7.5" y="4" width="6" height="4" rx="1"/><rect x="7.5" y="10" width="6" height="4" rx="1"/>`)
	folderIcon   = icon(`<path d="M1.5 4.5a1 1 0 0 1 1-1h3.6l1.4 1.5h5.9a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1h-10.9a1 1 0 0 1-1-1z"/>`)
	worktreeIcon = icon(`<path d="M10.5 2.5l3 3-7.5 7.5H3v-3z"/><path d="M8.5 4.5l3 3"/>`)
	changesIcon  = icon(`<path d="M9.5 1.5h-5a1 1 0 0 0-1 1v11a1 1 0 0 0 1 1h7a1 1 0 0 0 1-1V4.5z"/><path d="M8 5.5v4M6 7.5h4M6 11.5h4"/>`)
	browseIcon   = icon(`<path d="M5.5 4.5L2 8l3.5 3.5M10.5 4.5L14 8l-3.5 3.5"/>`)
	chevronIcon  = icon(`<path d="M4 6l4 4 4-4" stroke-width="1.8"/>`)
)

// githubIcon is GitHub's mark, from Primer's Octicons (MIT), filled.
var githubIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"/></svg>`))
