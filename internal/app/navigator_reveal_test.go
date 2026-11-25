package app

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestRevealInProjectNavigatorShortcut(t *testing.T) {
	files := map[string]string{
		"src/components/Widget.tsx":     "export const Widget = 1\n",
		"src/components/Widget.test.ts": "export const test = 1\n",
	}
	for i := range 80 {
		files[fmt.Sprintf("src/components/A%02d.txt", i)] = "filler\n"
	}
	h := newHarness(t, writeTree(t, files))
	a := h.a
	a.trustDismissed = true
	a.sidebarOff, a.navigator = true, navProblems
	d := h.open("src/components/Widget.test.ts")
	a.tree.Open.Clear()
	a.reveal = ""
	st := a.activeEditor()
	a.focusEditor = st
	h.tt.Frame()

	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyJ)
	if a.sidebarOff || a.navigator != navFiles {
		t.Fatal("shortcut did not show the project navigator")
	}
	h.settleFrames(func() bool { return h.treeHas(d.path) })
	assertNavigatorSelection(t, h, d.path)
	if !a.tree.Open.Has(filepath.Join(a.root, "src", "components", "Widget.tsx")) {
		t.Fatal("the parent of the nested file stayed closed")
	}
	if a.activeEditor() != st {
		t.Fatal("reveal changed the active editor")
	}
	before := st.CursorPos()
	h.tt.Key(0, ui.KeyRight)
	if st.CursorPos() == before {
		t.Fatal("reveal took keyboard focus from the editor")
	}
}

func TestRevealInProjectNavigatorFromPaletteClearsFilter(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"src/current.txt": "current file\n",
		"docs/other.txt":  "other file\n",
	}))
	a := h.a
	d := h.open("src/current.txt")
	h.settleFrames(func() bool { return h.treeHas(d.path) })
	src := filepath.Join(a.root, "src")
	a.quick.files = []string{"src/current.txt", "docs/other.txt"}
	a.filter.query = "other"
	h.tt.Frame()
	if h.treeHas(d.path) || a.tree.Open.Has(src) {
		t.Fatal("test filter did not hide the active file")
	}

	palette := jumpShortcut(t, "Command Palette")
	h.tt.Key(palette.shortcutMods, palette.shortcutKey)
	h.tt.Type("reveal in project navigator")
	if !h.tt.HasText("Reveal in Project Navigator") {
		t.Fatal("reveal command is missing from the palette")
	}
	h.tt.Key(0, ui.KeyEnter)
	if a.filter.query != "" || !a.tree.Open.Has(src) {
		t.Fatal("reveal did not clear the filter and reopen the file's folder")
	}
	assertNavigatorSelection(t, h, d.path)
}

func TestRevealInProjectNavigatorWithNoProjectFile(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"existing.txt": "existing\n"}))
	h.a.newFile()
	h.a.navigator = navProblems
	h.tt.Frame()
	if h.a.commandEnabled("navigator.reveal") {
		t.Fatal("reveal is enabled for an untitled tab")
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyJ)
	if h.a.navigator != navProblems {
		t.Fatal("reveal changed the navigator with no project file to select")
	}
}

func assertNavigatorSelection(t *testing.T, h *harness, path string) {
	t.Helper()
	a := h.a
	if a.treeRow < 0 || a.treeRow >= a.tree.Rows() || a.tree.Item(a.treeRow) != path {
		t.Fatalf("row %d does not select %q", a.treeRow, path)
	}
	first, last := a.tree.List.Visible()
	if a.treeRow < first || a.treeRow > last {
		t.Fatalf("selected row %d is outside the visible range %d–%d", a.treeRow, first, last)
	}
}
