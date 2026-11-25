package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
)

func TestEditorLineHeightPreferenceUpdatesOtherWorkspace(t *testing.T) {
	cfg := &config.Config{}
	first := newHarnessWithConfig(t, writeTree(t, map[string]string{"a.go": "package a\npackage b\n"}), cfg)
	second := newHarnessWithConfig(t, writeTree(t, map[string]string{"b.go": "package b\npackage c\n"}), cfg)
	first.open("a.go")
	second.open("b.go")
	diff := &editor.DiffState{}
	second.a.focus.diffs = map[string]*gitDiffTab{"line-height-other-window": {state: diff}}

	cfg.EditorLineHeight = 2
	first.a.applyEditorPrefsAcrossWindows()
	first.settle(func() bool { return first.a.activeEditor().LineHeight == 2 })
	second.settle(func() bool { return second.a.activeEditor().LineHeight == 2 })

	st := second.a.activeEditor()
	if got, want := st.LineTop(1)-st.LineTop(0), float64(st.Font.Size*2); got != want {
		t.Fatalf("other workspace editor row height = %g, want %g", got, want)
	}
	if got, want := first.a.activeEditor().LineTop(1)-first.a.activeEditor().LineTop(0), float64(first.a.activeEditor().Font.Size*2); got != want {
		t.Fatalf("current workspace editor row height = %g, want %g", got, want)
	}
	if diff.LineHeight != 2 {
		t.Fatalf("other workspace diff line height = %g, want 2", diff.LineHeight)
	}
}
