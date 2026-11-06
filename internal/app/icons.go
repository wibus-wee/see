package app

import (
	"github.com/egoist/mygo/ui"
)

// Icons drawn after SF Symbols' shapes (not copies of them), in the
// color of the text around them.
var (
	icProblems   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3 2 21h20L12 3Z"/><path d="M12 9v5m0 3v.1"/></svg>`))
	icAdd        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`))
	icFolder     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 6.5A2.5 2.5 0 0 1 5.5 4h4.1c.7 0 1.3.3 1.8.8L12.8 6.3h5.7A2.5 2.5 0 0 1 21 8.8v8.7a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/></svg>`))
	icPage       = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 20 24"><path d="M3.5 1h8.4c.5 0 1 .2 1.4.6l4.6 4.6c.4.4.6.9.6 1.4V20.5A2.5 2.5 0 0 1 16 23H3.5A2.5 2.5 0 0 1 1 20.5v-17A2.5 2.5 0 0 1 3.5 1z"/></svg>`))
	icSearch     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/></svg>`))
	icSidebar    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M9 4v16"/><path d="M4.8 8h2M4.8 11h2M4.8 14h2" stroke-linecap="round"/></svg>`))
	icSplitH     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M12 4v16"/></svg>`))
	icSplitV     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M2.5 12h19"/></svg>`))
	icBack       = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M15 5l-7 7 7 7"/></svg>`))
	icForward    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 5l7 7-7 7"/></svg>`))
	icClose      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.2" stroke-linecap="round"><path d="M7 7l10 10M17 7 7 17"/></svg>`))
	icMinus      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M5 12h14"/></svg>`))
	icRefresh    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19.5 12a7.5 7.5 0 1 1-2.2-5.3"/><path d="M19.5 4.5v4h-4"/></svg>`))
	icGitOpen    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M13 3h5a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V9"/><path d="M3 7h10M9 3l4 4-4 4"/></svg>`))
	icGitDiscard = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 14 4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-2"/></svg>`))
	icGit        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="4" r="2"/><circle cx="6" cy="20" r="2"/><circle cx="18" cy="6" r="2"/><path d="M6 6v12M18 8c0 6-12 4-12 10"/></svg>`))
	icCollapse   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M8 12h8"/></svg>`))
	icHex        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 8h2m3 0h2m3 0h.01M7 12h2m3 0h2m3 0h.01M7 16h2m3 0h2m3 0h.01"/></svg>`))
	icMarkdown   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="5" width="19" height="14" rx="2"/><path d="M6 15V9l2.5 3L11 9v6M15.5 9v6m-2-2 2 2 2-2"/></svg>`))
	icRelated    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="7" height="7" rx="1.5"/><rect x="14" y="13" width="7" height="7" rx="1.5"/><path d="M10 7.5h4a3 3 0 0 1 3 3V13"/></svg>`))
	icChevron    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>`))
	icPanel      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M2.5 14h19"/></svg>`))
	icHammer     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20l9-9"/><path d="M11 6.5 14.5 3l6.5 6.5L17.5 13z"/></svg>`))
)

// folderGlyph draws a folder, open or closed, as files are drawn: the
// icon theme's (fileicons.go).
func folderGlyph(c *ui.Context, open bool, size float32) {
	icon := fileIconTheme.folder
	if open {
		icon = fileIconTheme.folderOpen
	}
	ui.Image(c, fileIconSVG(icon, c.Theme().Dark)).Size(size, size).Shrink(0)
}
