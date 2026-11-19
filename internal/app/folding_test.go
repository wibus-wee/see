package app

import (
	"slices"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

const foldingSource = "package example\n\nimport (\n\t\"fmt\"\n\t\"os\"\n)\n\nfunc first() {\n\tfmt.Println(os.Args)\n}\n\nfunc second() {\n\tfirst()\n}\n"

func runFoldingCommand(t *testing.T, a *App, name string) {
	t.Helper()
	for _, cmd := range commands() {
		if cmd.name == name {
			cmd.run(a)
			return
		}
	}
	t.Fatalf("missing command %q", name)
}

func TestFoldingPaletteCommands(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": foldingSource, "empty.go": ""}))
	for _, cmd := range foldingCommands() {
		cmd.run(h.a)
	}
	h.open("main.go")
	st := h.a.activeEditor()
	runFoldingCommand(t, h.a, "Fold All")
	if len(st.Folded()) < 3 {
		t.Fatalf("Fold All: %v", st.Folded())
	}
	runFoldingCommand(t, h.a, "Unfold All")
	if len(st.Folded()) != 0 {
		t.Fatalf("Unfold All: %v", st.Folded())
	}
	runFoldingCommand(t, h.a, "Fold Imports")
	if !slices.Equal(st.Folded(), []int{2}) {
		t.Fatalf("Fold Imports: %v", st.Folded())
	}
	h.open("empty.go")
	for _, cmd := range foldingCommands() {
		cmd.run(h.a)
	}
	if got := h.a.activeEditor().Folded(); len(got) != 0 {
		t.Fatalf("empty buffer folds: %v", got)
	}
}

func TestFoldImportsOnOpen(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": foldingSource, "other.go": foldingSource}))
	d := h.open("main.go")
	if got := h.a.activeEditor().Folded(); len(got) != 0 {
		t.Fatalf("fold imports defaults on: %v", got)
	}
	h.a.cfg.EditorFoldImports = true
	h.open("other.go")
	st := h.a.activeEditor()
	if !slices.Equal(st.Folded(), []int{2}) {
		t.Fatalf("imports not folded on open: %v", st.Folded())
	}
	st.Run(editor.UnfoldAll)
	h.a.showLoaded(h.a.focus, h.a.docs[h.a.activePath()])
	h.tt.Frame()
	if len(st.Folded()) != 0 {
		t.Fatal("imports folded again")
	}
	h.a.openFile(d.path, nil)
	if len(h.a.activeEditor().Folded()) != 0 {
		t.Fatal("existing tab folded after preference changed")
	}
}

func TestFoldsSurviveSession(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": foldingSource, "unfolded.go": foldingSource}))
	d := h.open("main.go")
	h.a.activeEditor().Fold(7)
	h.open("unfolded.go")
	h.a.captureSession()
	saved := h.a.cfg.Workspaces[h.a.root].Layout
	saved.Tabs[0].Folds = append(saved.Tabs[0].Folds, -1, 99999)
	h.a.cfg.EditorFoldImports = true
	next := newHarness(t, "")
	next.a.cfg = h.a.cfg
	next.a.openWorkspace(h.a.root)
	next.settle(func() bool {
		return next.a.focus.eds[d.path] != nil && next.a.activeEditor() != nil
	})
	if got := next.a.focus.eds[d.path].Folded(); !slices.Equal(got, []int{7}) {
		t.Fatalf("restored folds: %v", got)
	}
	if got := next.a.activeEditor().Folded(); len(got) != 0 {
		t.Fatalf("saved unfolded tab had imports folded: %v", got)
	}
}
