package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"testing"
)

func TestGoToLineThroughQuickOpen(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first\nsecond\nthird"}))
	d := h.open("a.txt")
	st := h.a.activeEditor()
	d.buf.Set("first\nsecond\nthird\nunsaved")
	st.Select(2, 2)
	h.tt.Frame()
	h.tt.Key(ui.Ctrl, ui.KeyG)
	h.tt.Frame()
	if !h.a.quick.open || h.a.quick.query != ":" {
		t.Fatal("Ctrl+G did not open line navigation")
	}
	h.tt.Type("2:3")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if h.a.quick.open || st.CursorPos() != (editor.Pos{Line: 1, Col: 2}) {
		t.Fatalf("jump: open=%v pos=%+v", h.a.quick.open, st.CursorPos())
	}
	h.a.goBack()
	if st.Cursor() != 2 {
		t.Fatalf("Go Back: %v", st.CursorPos())
	}
	h.a.openQuickPanel(":4:99")
	h.a.chooseQuick(0)
	if st.Cursor() != d.buf.Len() {
		t.Fatalf("unsaved bounds: %v", st.CursorPos())
	}
	for _, query := range []string{":", ":0", ":-1", ":bad", ":2:no", ":2:3:4"} {
		h.a.openQuickPanel(query)
		h.a.chooseQuick(0)
		if !h.a.quick.open || st.Cursor() != d.buf.Len() {
			t.Fatalf("invalid %q navigated", query)
		}
	}
	h.a.quick.open = false
	h.a.focus.active = ""
	h.a.openQuickPanel(":2")
	h.a.chooseQuick(0)
	if !h.a.quick.open {
		t.Fatal("navigated without text editor")
	}
}
