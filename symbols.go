package main

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
	case icFolderOpen:
		return "folder.fill"
	case icFolderClosed:
		return "folder"
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
	icFolderClosed = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24"><path d="M3 6.5A2.5 2.5 0 0 1 5.5 4h4.1c.7 0 1.3.3 1.8.8L12.8 6.3h5.7A2.5 2.5 0 0 1 21 8.8v8.7a2.5 2.5 0 0 1-2.5 2.5h-13A2.5 2.5 0 0 1 3 17.5z"/></svg>`))
	icNewFolder    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3 7a2 2 0 0 1 2-2h4l2 2h8a2 2 0 0 1 2 2v9a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2zM12 10v7m-3.5-3.5h7"/></svg>`))
	icLink         = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M7 17 17 7M8 7h9v9"/></svg>`))
	icCheck        = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m5 12 5 5L19 7"/></svg>`))
)
