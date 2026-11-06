package app

import (
	"github.com/egoist/mygo/ui"
	"testing"
)

func TestEditorGroupKeyboardNavigation(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first", "b.txt": "second"}))
	a := h.open("a.txt")
	left := h.a.focus
	left.eds[a.path].Select(2, 2)
	h.a.splitActive(sideRight)
	right := h.a.focus
	b := h.open("b.txt")
	right.eds[b.path].Select(3, 3)
	h.tt.Frame()
	h.tt.Key(0, ui.KeyF6)
	h.tt.Frame()
	if h.a.focus != left || h.a.activeEditor().Cursor() != 2 {
		t.Fatal("next editor did not wrap preserving cursor")
	}
	h.tt.Key(ui.Shift, ui.KeyF6)
	h.tt.Frame()
	if h.a.focus != right || h.a.activeEditor().Cursor() != 3 {
		t.Fatal("previous editor did not wrap preserving cursor")
	}
	b.buf.Set("edited second")
	h.a.openPalette()
	h.a.quick.query = ">Move Tab to Next Editor"
	h.a.rankQuick()
	h.a.chooseQuick(0)
	h.tt.Frame()
	if h.a.focus != left || !left.has(b.path) || right.has(b.path) || h.a.docs[b.path] != b || h.a.confirm != nil {
		t.Fatal("move lost shared buffer or did not focus destination")
	}
	h.a.moveTabToNextEditor()
	h.tt.Frame()
	if !right.has(b.path) || h.a.focus != right {
		t.Fatal("move did not advance")
	}
}

func TestEditorGroupMoveRemovesEmptySource(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first"}))
	d := h.open("a.txt")
	original := h.a.focus
	h.a.splitActive(sideRight)
	source := h.a.focus
	h.a.moveTabToNextEditor()
	h.tt.Frame()
	if len(h.a.groups()) != 1 || h.a.focus != original || source.has(d.path) || h.a.docs[d.path] != d {
		t.Fatal("empty source not removed safely")
	}
	h.tt.Key(0, ui.KeyF6)
	h.tt.Key(ui.Shift, ui.KeyF6)
	h.a.moveTabToNextEditor()
	if h.a.focus != original || !original.has(d.path) {
		t.Fatal("single-group no-op changed tabs")
	}
}
