package app

import (
	"path/filepath"
	"testing"
)

// The rename input of a row chosen in the tree reads in the theme's text
// color: the row's selection paints its text white on the accent, which
// must not reach the input's own face, or the name being edited is white
// on white.
func TestRenameInputKeepsReadableText(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"rename-me.txt": "x\n"}))
	a := h.a
	a.sidebarW = 260
	p := filepath.Join(a.root, "rename-me.txt")
	h.settleFrames(func() bool { return h.treeHas(p) })
	for i := range a.tree.Rows() {
		if a.tree.Item(i) == p {
			a.treeRow = i
		}
	}
	a.beginRename(p)
	h.tt.Frame()
	h.tt.Frame()
	if a.renameEntry == nil {
		t.Fatal("rename did not begin")
	}
	img := h.tt.Image()
	b := img.Bounds()
	// The input's face is the widest run of white in the sidebar, broken
	// at its glyphs. Find it and count the dark glyph pixels inside: the
	// name's suffix after the stem is not selected, so it must read on
	// the face.
	bestX0, bestX1, bestY, bestRun := 0, 0, -1, 0
	for y := 0; y < b.Dy(); y++ {
		x0, run := -1, 0
		for x := 0; x < 240 && x < b.Dx(); x++ {
			r, g, bl, _ := img.At(x, y).RGBA()
			if white := r>>8 > 245 && g>>8 > 245 && bl>>8 > 245; white {
				if x0 < 0 {
					x0 = x
				}
				run++
			} else {
				if run > bestRun {
					bestX0, bestX1, bestY, bestRun = x0, x-1, y, run
				}
				x0, run = -1, 0
			}
		}
		if run > bestRun {
			bestX0, bestX1, bestY, bestRun = x0, 239, y, run
		}
	}
	if bestY < 0 {
		t.Fatal("no white surface found in the sidebar")
	}
	// The glyphs of the unselected suffix sit inside the input's box,
	// before the white face's unbroken run begins. Scan the rows of the
	// input's band from the box's left edge, where other rows' glyphs
	// cannot interfere.
	dark := 0
	for y := bestY; y <= bestY+13; y++ {
		for x := bestX0 - 60; x <= 180; x++ {
			if x < 0 {
				continue
			}
			r, g, bl, _ := img.At(x, y).RGBA()
			rs, gs, bs := int(r>>8), int(g>>8), int(bl>>8)
			// Dark text on white: low sum without the blue selection cast.
			if rs+gs+bs < 300 {
				dark++
			}
		}
	}
	_ = bestX1
	if dark < 10 {
		t.Fatalf("the rename input shows %d dark glyph pixels inside its face: white text on white", dark)
	}
}
