package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestResetSettingsReappliesEditorLineHeight(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\npackage b\n"}))
	h.open("a.go")
	st := h.a.activeEditor()
	h.tt.Frame()
	defaultRowHeight := st.LineTop(1) - st.LineTop(0)
	diff := &editor.DiffState{}
	h.a.focus.diffs = map[string]*gitDiffTab{"line-height-reset": {state: diff}}

	h.a.cfg.EditorLineHeight = 2
	h.a.applyEditorPrefsAll()
	h.tt.Frame()

	settings := ui.NewTester(h.a.advancedPane, 900, 600)
	if err := settings.Click("Reset"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()

	if got, want := st.LineTop(1)-st.LineTop(0), defaultRowHeight; got != want {
		t.Fatalf("editor row height after reset = %g, want %g", got, want)
	}
	if diff.LineHeight != 0 {
		t.Fatalf("diff line height after reset = %g, want default", diff.LineHeight)
	}
}
