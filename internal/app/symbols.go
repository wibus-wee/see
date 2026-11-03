package app

import (
	"sync"

	"github.com/egoist/mygo/ui"
)

// The SVGs are independent fallbacks for platforms without SF Symbols.
// macOS obtains the symbols from AppKit at runtime, not from bundled artwork.
var symbolCache sync.Map // map[*ui.SVG]*symbolImage

type symbolImage struct {
	bitmap        *ui.Bitmap
	width, height float32
}

func symbolName(icon *ui.SVG) string {
	switch icon {
	case icAdd:
		return "document.badge.plus"
	case icPlus:
		return "plus"
	case icFolder:
		return "folder.fill"
	case icNewFolder:
		return "folder.badge.plus"
	case folderIcon:
		return "folder"
	case openIcon:
		return "folder"
	case reopenIcon:
		return "clock.arrow.circlepath"
	case icSearch:
		return "magnifyingglass"
	case icSidebar:
		return "sidebar.left"
	case icSplitH:
		return "rectangle.split.2x1"
	case icSplitV:
		return "rectangle.split.1x2"
	case icBack:
		return "chevron.left"
	case icForward, icChevron:
		return "chevron.right"
	case icClose:
		return "xmark"
	case icMinus:
		return "minus"
	case icRefresh:
		return "arrow.clockwise"
	case icGitOpen:
		return "doc"
	case icGitDiscard:
		return "arrow.uturn.backward"
	case icGit:
		return "arrow.triangle.branch"
	case icCollapse:
		return "rectangle.compress.vertical"
	case icMarkdown:
		return "doc.richtext"
	case icRelated:
		return "link"
	case icPanel:
		return "rectangle.bottomthird.inset.filled"
	case icHammer:
		return "hammer"
	case icGear:
		return "gearshape"
	case icLines:
		return "text.alignleft"
	case icSparkle:
		return "sparkles"
	case icCommand:
		return "command"
	case icTrash:
		return "trash"
	case icMaximize:
		return "chevron.up"
	case icRestore:
		return "chevron.down"
	case icPrompt:
		return "terminal"
	case icLink:
		return "arrow.up.right"
	case icCheck:
		return "checkmark"
	case icCopy:
		return "doc.on.doc"
	case icDiffSplit:
		return "rectangle.split.2x1"
	case icDiffInline:
		return "rectangle.grid.1x2"
	case icChangeUp:
		return "chevron.up"
	case icChangeDown:
		return "chevron.down"
	case icBranches:
		return "point.3.connected.trianglepath.dotted"
	case icCommitArrow:
		return "arrow.up"
	}
	return ""
}

// symbolIcon keeps the existing square icon boxes and their explicit tint.
func symbolIcon(c *ui.Context, icon *ui.SVG, size float32, color ui.Color) ui.Element {
	entry, ok := symbolCache.Load(icon)
	if !ok {
		image := loadSystemSymbol(symbolName(icon))
		entry, _ = symbolCache.LoadOrStore(icon, image)
	}
	image := entry.(*symbolImage)
	return ui.Box(c).Size(size, size).Shrink(0).Draw(func(p *ui.Painter, r ui.Rect) {
		if color.A == 0 {
			return
		}
		if image.bitmap == nil {
			p.Icon(icon, r, color)
			return
		}
		// Contain the native image in the icon box without distorting it.
		scale := min(r.W/image.width, r.H/image.height)
		w, h := image.width*scale, image.height*scale
		dst := ui.Rect{X: r.X + (r.W-w)/2, Y: r.Y + (r.H-h)/2, W: w, H: h}
		p.ImageRegion(image.bitmap, ui.Rect{}, dst, ui.ImageOptions{Tint: color})
	})
}

var (
	icNewFolder   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 10v7m-3.5-3.5h7"/></svg>`))
	icLink        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7M8 7h9v9"/></svg>`))
	icCopy        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="8" y="8" width="12" height="12" rx="2"/><path d="M16 8V6a2 2 0 0 0-2-2H6a2 2 0 0 0-2 2v8a2 2 0 0 0 2 2h2"/></svg>`))
	icBranches    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><circle cx="6" cy="5" r="2"/><circle cx="6" cy="19" r="2"/><circle cx="18" cy="5" r="2"/><circle cx="18" cy="12" r="2"/><path d="M6 7v10M18 7v3M18 14c0 3-5 2-10 4"/></svg>`))
	icCommitArrow = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.6" stroke-linecap="round" stroke-linejoin="round"><path d="M12 19V5M6 11l6-6 6 6"/></svg>`))
	icDiffSplit   = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M12 4v16M6 9h3M6 13h3M15 11h3M15 15h3"/></svg>`))
	icDiffInline  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><rect x="3" y="4" width="18" height="16" rx="2"/><path d="M7 9h10M7 12.5h7M7 16h10"/></svg>`))
	icChangeUp    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 15l6-6 6 6"/></svg>`))
	icChangeDown  = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 9l6 6 6-6"/></svg>`))
	icCheck       = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L19 7"/></svg>`))
)
