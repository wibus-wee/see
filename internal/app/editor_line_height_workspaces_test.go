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
	otherEditor := second.a.activeEditor()
	projects := []*projectWindow{{app: first.a}, {app: second.a}}
	refreshEditorPrefs := func() { applyEditorPrefsToProjects(projects) }
	first.a.editorPrefsChanged = refreshEditorPrefs
	second.a.editorPrefsChanged = refreshEditorPrefs

	first.settle(func() bool { return len(first.q) == 0 })
	second.settle(func() bool { return len(second.q) == 0 })
	if otherEditor.LineHeight != 0 || diff.LineHeight != 0 {
		t.Fatal("other workspace editor preferences changed before the shared line-height update")
	}

	cfg.EditorLineHeight = 2
	first.a.applyEditorPrefsAcrossWindows()
	if otherEditor.LineHeight != 0 || diff.LineHeight != 0 {
		t.Fatal("other workspace editor preferences changed before its scheduled refresh")
	}
	first.settle(func() bool { return len(first.q) == 0 })
	second.settle(func() bool { return len(second.q) == 0 })
	if second.a.activeEditor() != otherEditor {
		t.Fatal("other workspace editor was replaced during the line-height update")
	}
	if otherEditor.LineHeight != 2 {
		t.Fatalf("other workspace editor line height = %g, want 2", otherEditor.LineHeight)
	}

	st := otherEditor
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
