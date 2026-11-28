package app

import (
	"path/filepath"
	"runtime"
	"testing"

	"github.com/egoist/mygo/ui"
)

// A single-click preview in the project navigator leaves the keyboard in
// the tree, as the row says it does: the user keeps walking files with
// the arrows, and on macOS Enter renames. The editor the preview shows
// must not take the focus as it appears, however long its file takes to
// load.
func TestPreviewClickKeepsKeyboardInTree(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}))
	a := h.a
	pa := filepath.Join(a.root, "a.txt")
	h.settleFrames(func() bool { return h.treeHas(pa) })
	if err := h.tt.Click("a.txt"); err != nil {
		t.Fatalf("clicking the file: %v", err)
	}
	h.settleFrames(func() bool { d := a.docs[pa]; return d != nil && !d.loading })
	h.tt.Frame()
	d := a.docs[pa]
	lenBefore := d.buf.Len()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if d.buf.Len() != lenBefore {
		d.buf.Undo()
		t.Fatal("Enter typed a newline into the previewed file: the editor stole the keyboard from the tree")
	}
	if runtime.GOOS == "darwin" && a.renameEntry == nil {
		t.Fatal("Enter did not begin a rename while the tree had the keyboard")
	}
}

// The focus is the tree's again as the preview replaces itself: clicking
// back into the tree and previewing another file keeps it there.
func TestSecondPreviewClickKeepsKeyboardInTree(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}))
	a := h.a
	pa, pb := filepath.Join(a.root, "a.txt"), filepath.Join(a.root, "b.txt")
	h.settleFrames(func() bool { return h.treeHas(pa) && h.treeHas(pb) })
	if err := h.tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	h.settleFrames(func() bool { d := a.docs[pa]; return d != nil && !d.loading })
	// Back into the tree, as the user walks on.
	if err := h.tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if err := h.tt.Click("b.txt"); err != nil {
		t.Fatal(err)
	}
	h.settleFrames(func() bool { d := a.docs[pb]; return d != nil && !d.loading })
	h.tt.Frame()
	d := a.docs[pb]
	lenBefore := d.buf.Len()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if d.buf.Len() != lenBefore {
		d.buf.Undo()
		t.Fatal("Enter typed a newline into the second preview: the editor took the keyboard from the tree")
	}
}

// Open Quickly previews a file for the same look, but the user opened it
// to edit it: the keyboard goes to the editor once its text is there.
func TestFocusedPreviewOpenFocusesTheEditor(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n"}))
	a := h.a
	pb := filepath.Join(a.root, "b.txt")
	h.settleFrames(func() bool { return h.treeHas(pb) })
	a.previewOpen(pb, true)
	h.settleFrames(func() bool { d := a.docs[pb]; return d != nil && !d.loading })
	h.tt.Frame()
	h.tt.Frame()
	d := a.docs[pb]
	lenBefore := d.buf.Len()
	h.tt.Type("bb")
	h.tt.Frame()
	if d.buf.Len() != lenBefore+2 {
		t.Fatal("a focused preview open did not give the editor the keyboard")
	}
	d.buf.Undo()
	d.buf.Undo()
}
