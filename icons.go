package main

import (
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
)

// Icons drawn after SF Symbols' shapes (not copies of them), in the
// color of the text around them.
var (
	icAdd        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 5v14M5 12h14"/></svg>`))
	icFolder     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 6.5A2.5 2.5 0 0 1 5.5 4h4.1c.7 0 1.3.3 1.8.8L12.8 6.3h5.7A2.5 2.5 0 0 1 21 8.8v8.7a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/></svg>`))
	icFolderOpen = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 6.5A2.5 2.5 0 0 1 5.5 4h4.1c.7 0 1.3.3 1.8.8L12.8 6.3h5.2A2.5 2.5 0 0 1 20.5 8.8V10H8.3a2.5 2.5 0 0 0-2.4 1.8L3.6 19.2A2.5 2.5 0 0 1 3 17.5z"/><path d="M8.3 11.5h13.1c.8 0 1.4.8 1.1 1.6l-1.8 5.4A2.2 2.2 0 0 1 18.6 20H5.2c-.8 0-1.4-.8-1.1-1.6l1.9-5.4a2.4 2.4 0 0 1 2.3-1.5z"/></svg>`))
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
	icGit        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="4" r="2"/><circle cx="6" cy="20" r="2"/><circle cx="18" cy="6" r="2"/><path d="M6 6v12M18 8c0 6-12 4-12 10"/></svg>`))
	icCollapse   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="4.5" width="17" height="15" rx="2"/><path d="M8 12h8"/></svg>`))
	icMarkdown   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="5" width="19" height="14" rx="2"/><path d="M6 15V9l2.5 3L11 9v6M15.5 9v6m-2-2 2 2 2-2"/></svg>`))
	icRelated    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="7" height="7" rx="1.5"/><rect x="14" y="13" width="7" height="7" rx="1.5"/><path d="M10 7.5h4a3 3 0 0 1 3 3V13"/></svg>`))
	icChevron    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M9 6l6 6-6 6"/></svg>`))
	icPanel      = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7"><rect x="2.5" y="4" width="19" height="16" rx="3"/><path d="M2.5 14h19"/></svg>`))
	icHammer     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 20l9-9"/><path d="M11 6.5 14.5 3l6.5 6.5L17.5 13z"/></svg>`))
)

// fileKind is how a file's icon looks: the letters on its page and
// their color, after Xcode's pages with a letter on them.
type fileKind struct {
	glyph string
	color ui.Color
}

var (
	kGo    = fileKind{"go", ui.RGB(0x3d, 0xb8, 0xd8)}
	kJS    = fileKind{"js", ui.RGB(0xd9, 0xb3, 0x1a)}
	kTS    = fileKind{"ts", ui.RGB(0x35, 0x78, 0xc6)}
	kJSON  = fileKind{"{}", ui.RGB(0xb0, 0x8a, 0x16)}
	kMD    = fileKind{"M", ui.RGB(0x5c, 0x7c, 0xa8)}
	kYAML  = fileKind{"y", ui.RGB(0xa0, 0x5c, 0xc8)}
	kTOML  = fileKind{"t", ui.RGB(0x9c, 0x6a, 0x3a)}
	kPy    = fileKind{"py", ui.RGB(0x35, 0x70, 0xa8)}
	kRust  = fileKind{"rs", ui.RGB(0xc8, 0x5a, 0x2a)}
	kC     = fileKind{"c", ui.RGB(0x4a, 0x7f, 0xd0)}
	kH     = fileKind{"h", ui.RGB(0xc8, 0x3a, 0x48)}
	kCPP   = fileKind{"c+", ui.RGB(0x5a, 0x55, 0xc8)}
	kSwift = fileKind{"sw", ui.RGB(0xf0, 0x6a, 0x2c)}
	kJava  = fileKind{"J", ui.RGB(0xc8, 0x6a, 0x2a)}
	kKt    = fileKind{"kt", ui.RGB(0x8a, 0x5a, 0xd8)}
	kHTML  = fileKind{"<>", ui.RGB(0xd8, 0x5a, 0x32)}
	kCSS   = fileKind{"#", ui.RGB(0x35, 0x7a, 0xc8)}
	kSh    = fileKind{"$", ui.RGB(0x4a, 0x9a, 0x4a)}
	kSQL   = fileKind{"db", ui.RGB(0x5a, 0x8a, 0xa8)}
	kProto = fileKind{"pb", ui.RGB(0x4a, 0x9a, 0x8a)}
	kMod   = fileKind{"m", ui.RGB(0x3d, 0xb8, 0xd8)}
	kLock  = fileKind{"•", ui.RGB(0x8a, 0x8a, 0x8a)}
	kText  = fileKind{"", ui.RGB(0x8a, 0x8a, 0x8a)}
)

// kindOf chooses a file's icon by its name.
func kindOf(name string) fileKind {
	lower := strings.ToLower(name)
	switch lower {
	case "go.mod", "go.sum", "go.work":
		return kMod
	case "dockerfile", "makefile":
		return kSh
	}
	switch strings.TrimPrefix(filepath.Ext(lower), ".") {
	case "go":
		return kGo
	case "js", "mjs", "cjs", "jsx":
		return kJS
	case "ts", "tsx", "mts":
		return kTS
	case "json", "jsonc":
		return kJSON
	case "md", "markdown", "mdx":
		return kMD
	case "yml", "yaml":
		return kYAML
	case "toml", "ini", "cfg":
		return kTOML
	case "py":
		return kPy
	case "rs":
		return kRust
	case "c", "m":
		return kC
	case "h", "hpp":
		return kH
	case "cc", "cpp", "cxx", "mm":
		return kCPP
	case "swift":
		return kSwift
	case "java":
		return kJava
	case "kt", "kts", "scala":
		return kKt
	case "html", "htm", "xml", "svg", "vue":
		return kHTML
	case "css", "scss", "less":
		return kCSS
	case "sh", "bash", "zsh", "fish", "ps1":
		return kSh
	case "sql":
		return kSQL
	case "proto", "graphql":
		return kProto
	case "lock", "sum":
		return kLock
	}
	return kText
}

// fileIcon draws a file's icon: a light page with its letters on it.
func fileIcon(c *ui.Context, name string, size float32) {
	k := kindOf(name)
	ui.Box(c).Size(size*0.84, size).Shrink(0).Children(func() {
		ui.Icon(c, icPage).Size(size*0.84, size).TextColor(colPage)
		if k.glyph != "" {
			fs := size * 0.46
			if len(k.glyph) > 1 {
				fs = size * 0.4
			}
			ui.Text(c, k.glyph).Absolute().Left(0).Right(0).Bottom(size * 0.1).TextAlign(ui.Center).
				FontSize(fs).FontWeight(800).TextColor(k.color).FixedLineHeight(fs)
		} else {
			// Lines of text on a plain page.
			ui.Column(c).Absolute().Left(size * 0.2).Right(size * 0.24).Top(size * 0.38).Gap(size * 0.1).Children(func() {
				for range 3 {
					ui.Box(c).Height(max(1, size*0.06)).FillWidth().Background(k.color).Radius(1)
				}
			})
		}
	})
}

// folderGlyph draws a folder, open or closed, in Xcode's blue.
func folderGlyph(c *ui.Context, open bool, size float32) {
	ic := icFolderClosed
	if open {
		ic = icFolderOpen
	}
	symbolIcon(c, ic, size, colFolder).Shrink(0)
}
