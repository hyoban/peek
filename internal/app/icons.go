package app

import "github.com/egoist/mygo/ui"

func icon(shapes string) *ui.SVG {
	return ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round">` + shapes + `</svg>`))
}

var (
	historyIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M10 2.5v11"/>`)
	terminalIcon = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M4.5 6l2 2-2 2M8 10.5h3.5"/>`)
	sidebarIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M6 2.5v11"/>`)
	folderIcon   = icon(`<path d="M1.5 4.5a1 1 0 0 1 1-1h3.6l1.4 1.5h5.9a1 1 0 0 1 1 1V12a1 1 0 0 1-1 1h-10.9a1 1 0 0 1-1-1z"/>`)
	worktreeIcon = icon(`<path d="M10.5 2.5l3 3-7.5 7.5H3v-3z"/><path d="M8.5 4.5l3 3"/>`)
	chevronIcon  = icon(`<path d="M4 6l4 4 4-4" stroke-width="1.8"/>`)
	backIcon     = icon(`<path d="M10 3L5 8l5 5" stroke-width="1.8"/>`)
	forwardIcon  = icon(`<path d="M6 3l5 5-5 5" stroke-width="1.8"/>`)
	branchIcon   = icon(`<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="5" r="1.5"/><path d="M4.5 5v6M11.5 6.5c0 2.5-3 2.5-5.5 4.6"/>`)
	tagIcon      = icon(`<path d="M2.5 3.5a1 1 0 0 1 1-1h4l6 6-5 5-6-6z"/><circle cx="5.5" cy="5.5" r="1"/>`)
	pullIcon     = icon(`<circle cx="4.5" cy="3.5" r="1.5"/><circle cx="4.5" cy="12.5" r="1.5"/><circle cx="11.5" cy="12.5" r="1.5"/><path d="M4.5 5v6M11.5 11V6.5a2 2 0 0 0-2-2H7.5"/><path d="M9 3l-1.5 1.5L9 6"/>`)
	checkIcon    = icon(`<path d="M3.5 8.5l3 3 6-7" stroke-width="1.6"/>`)
	plusIcon     = icon(`<path d="M8 3v10M3 8h10" stroke-width="1.6"/>`)
	minusIcon    = icon(`<path d="M3 8h10" stroke-width="1.6"/>`)
	pullArrow    = icon(`<path d="M8 2.5v10M4 8.5l4 4 4-4" stroke-width="1.6"/>`)
	stopIcon     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><rect x="3.5" y="3.5" width="9" height="9" rx="1.5"/></svg>`))
	repoIcon     = icon(`<path d="M3 2.5h8.5a1 1 0 0 1 1 1v9a1 1 0 0 1-1 1H4.5A1.5 1.5 0 0 1 3 12V2.5z"/><path d="M3 11.5a1.5 1.5 0 0 1 1.5-1.5h8"/><path d="M6 13.5v2l1-.7 1 .7v-2"/>`)
	previewIcon  = icon(`<path d="M1.5 8s2.4-4.5 6.5-4.5S14.5 8 14.5 8 12.1 12.5 8 12.5 1.5 8 1.5 8z"/><circle cx="8" cy="8" r="2"/>`)
	splitIcon    = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M8 2.5v11M3.5 6h2.5M10 6h2.5M3.5 9h2.5M10 9h2.5"/>`)
	unifiedIcon  = icon(`<rect x="1.5" y="2.5" width="13" height="11" rx="2"/><path d="M4 5.5h8M4 8h8M4 10.5h5"/>`)
	// shortcutsIcon is the shortcuts' to a request, a bolt, as quick
	// actions'; sendIcon sends it.
	shortcutsIcon   = icon(`<path d="M9 1.8L3.6 9h4.1l-.9 5.2L12.4 7H8.2z" fill="currentColor" stroke-width="1"/>`)
	copyIcon        = icon(`<rect x="5.5" y="5.5" width="8" height="8" rx="1.5"/><path d="M10.5 5.5V3.5a1 1 0 0 0-1-1h-6a1 1 0 0 0-1 1v6a1 1 0 0 0 1 1h2"/>`)
	checkCircleIcon = icon(`<circle cx="8" cy="8" r="6.5"/><path d="M5.2 8.2l2 2 3.8-4.2" stroke-width="1.6"/>`)
	crossCircleIcon = icon(`<circle cx="8" cy="8" r="6.5"/><path d="M5.8 5.8l4.4 4.4M10.2 5.8l-4.4 4.4" stroke-width="1.6"/>`)
	skipCircleIcon  = icon(`<circle cx="8" cy="8" r="6.5"/><path d="M5.5 8h5" stroke-width="1.6"/>`)
	warningIcon     = icon(`<path d="M8 2.2L14.3 13a.6.6 0 0 1-.5.9H2.2a.6.6 0 0 1-.5-.9z"/><path d="M8 6.5v3" stroke-width="1.6"/><circle cx="8" cy="11.6" r=".4" fill="currentColor"/>`)
	sendIcon        = icon(`<path d="M8 13.5v-11M3.5 7L8 2.5 12.5 7" stroke-width="1.8"/>`)
	commentIcon     = icon(`<path d="M2.5 3.5a1 1 0 0 1 1-1h9a1 1 0 0 1 1 1v6.5a1 1 0 0 1-1 1H7l-3 2.5v-2.5h-.5a1 1 0 0 1-1-1z"/>`)
)

// githubIcon is GitHub's mark, from Primer's Octicons (MIT), filled.
var githubIcon = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 16 16" fill="currentColor"><path d="M8 0c4.42 0 8 3.58 8 8a8.013 8.013 0 0 1-5.45 7.59c-.4.08-.55-.17-.55-.38 0-.27.01-1.13.01-2.2 0-.75-.25-1.23-.54-1.48 1.78-.2 3.65-.88 3.65-3.95 0-.88-.31-1.59-.82-2.15.08-.2.36-1.02-.08-2.12 0 0-.67-.22-2.2.82-.64-.18-1.32-.27-2-.27-.68 0-1.36.09-2 .27-1.53-1.03-2.2-.82-2.2-.82-.44 1.1-.16 1.92-.08 2.12-.51.56-.82 1.28-.82 2.15 0 3.06 1.86 3.75 3.64 3.95-.23.2-.44.55-.51 1.07-.46.21-1.61.55-2.33-.66-.15-.24-.6-.83-1.23-.82-.67.01-.27.38.01.53.34.19.73.9.82 1.13.16.45.68 1.31 2.69.94 0 .67.01 1.3.01 1.49 0 .21-.15.45-.55.38A7.995 7.995 0 0 1 0 8c0-4.42 3.58-8 8-8Z"/></svg>`))
