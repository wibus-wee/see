//go:build darwin

package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestSystemSymbolsLoad(t *testing.T) {
	icons := []*ui.SVG{icAdd, icFolder, icFolderOpen, icFolderClosed, icNewFolder, folderIcon, openIcon, reopenIcon,
		icSearch, icSidebar, icSplitH, icSplitV, icBack, icForward, icClose, icMinus,
		icRefresh, icGit, icCollapse, icMarkdown, icRelated, icChevron, icPanel,
		icHammer, icGear, icLines, icSparkle, icCommand, icPlus, icTrash,
		icMaximize, icRestore, icPrompt, icLink, icCheck}
	for _, icon := range icons {
		name := symbolName(icon)
		if name == "" {
			t.Fatal("missing system symbol name")
		}
		image := loadSystemSymbol(name)
		if image.bitmap == nil || image.width < 32 || image.height < 4 {
			t.Errorf("system symbol %q failed to load at useful resolution (%v x %v)", name, image.width, image.height)
		}
	}
}

func TestSystemSymbolRendersWithTint(t *testing.T) {
	view := ui.NewTester(func(c *ui.Context) {
		ui.Box(c).Fill().Background(ui.RGB(255, 255, 255)).Children(func() {
			symbolIcon(c, icSearch, 32, ui.RGB(200, 20, 20))
		})
	}, 40, 40)
	img := view.Image()
	colored := 0
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			if r > g*2 && r > b*2 {
				colored++
			}
		}
	}
	if colored == 0 {
		t.Fatal("system symbol did not render in the requested color")
	}
}
