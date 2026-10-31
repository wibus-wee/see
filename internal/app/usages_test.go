package app

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
	"see/internal/nav"
)

type usageClient struct {
	*fakeClient
	target string
	line   int
}

func (c *usageClient) References(context.Context, string, lsp.Position) ([]lsp.Location, error) {
	return []lsp.Location{{URI: uriOf(c.target), Range: lsp.Range{Start: lsp.Position{Line: c.line}}}}, nil
}
func (c *usageClient) Callers(context.Context, string, lsp.Position) ([]lsp.Location, error) {
	return []lsp.Location{{URI: uriOf(c.target), Range: lsp.Range{Start: lsp.Position{Line: c.line}}}}, nil
}

func TestReferencesShowSurroundingUnsavedCode(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "old"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.lsp.Stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go"), line: 3}
	m := serverOf(root, client)
	h.a.lsp = m
	defer m.StopWait(time.Second)
	b := h.open("b.go")
	b.buf.Set("zero\none\n  before\n    F()\n  after\nfive\nsix")
	d := h.open("a.go")
	h.ed(d).GoTo(d.buf.Pos(at(d, "F()")))
	h.a.findUsages(false)
	h.settle(func() bool { return h.a.choices != nil && len(h.a.choices.locs[0].preview) > 0 })
	target := h.a.choices.locs[0]
	if target.previewStart != 1 || !slices.Equal(target.preview, []string{"one", "  before", "    F()", "  after", "five"}) {
		t.Fatalf("context at %d: %q", target.previewStart, target.preview)
	}
	if h.a.choices.previewEditor == nil || h.a.choices.previewEditor.Buffer.String() != b.buf.String() {
		t.Fatal("preview does not show the unsaved file")
	}
	if _, ok := h.tt.Find("Reference code preview"); !ok {
		t.Fatal("reference editor is missing")
	}
}

func TestReferenceSelectionChangesPreview(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "first\nsecond", "c.go": "third\nfourth"})
	h := newHarness(t, root)
	d := h.open("a.go")
	b, c := filepath.Join(root, "b.go"), filepath.Join(root, "c.go")
	ch := &defChoices{g: h.a.focus, from: nav.Entry{Path: d.path}, title: "references", enc: lsp.UTF16,
		locs:    []target{{path: b, pos: lsp.Position{Line: 0}, preview: []string{"first"}}, {path: c, pos: lsp.Position{Line: 1}, preview: []string{"third", "fourth"}}},
		buffers: map[string]*editor.Buffer{b: editor.NewBuffer("first\nsecond"), c: editor.NewBuffer("third\nfourth")}}
	h.a.choices = ch
	h.a.selectRelated(ch, 0)
	h.tt.Frame()
	h.click("c.go:2")
	if ch.selected != 1 || ch.previewEditor.Buffer.String() != "third\nfourth" || ch.previewEditor.CursorPos().Line != 1 {
		t.Fatal("selecting another reference did not update the code preview")
	}
	h.click("Toggle references in c.go")
	if _, ok := h.tt.Find("c.go:2"); ok {
		t.Fatal("collapsed file still shows references")
	}
}

func TestFindUsagesShowsResultsAndNavigates(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "package a\nfunc G() { F() }"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	old := h.a.lsp
	old.Stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := serverOf(root, client)
	h.a.lsp = m
	defer m.StopWait(time.Second)
	d := h.open("a.go")
	h.ed(d).GoTo(d.buf.Pos(at(d, "F()")))
	h.a.findUsages(false)
	h.settle(func() bool { return h.a.choices != nil })
	if h.a.choices.title != "references" {
		t.Fatalf("results %v", h.tt.Texts())
	}
	h.settle(func() bool { return len(h.a.choices.locs[0].preview) > 0 })
	if !slices.Equal(h.a.choices.locs[0].preview, []string{"package a", "func G() { F() }"}) {
		t.Fatalf("preview: %q", h.a.choices.locs[0].preview)
	}
	h.click("b.go:1")
	if h.a.choices == nil || h.a.choices.previewEditor == nil {
		t.Fatal("selection did not show the code preview")
	}
	h.click("b.go:1")
	h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "b.go") })
	h.a.activate(d.path)
	h.a.findUsages(true)
	h.settle(func() bool { return h.a.choices != nil })
	if h.a.choices.title != "callers" {
		t.Fatalf("callers %v", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	if h.a.choices != nil {
		t.Fatal("Escape did not close callers")
	}
}

func TestEditorContextMenuFindsReferencesAndCallers(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "package a\nfunc G() { F() }"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.lsp.Stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := serverOf(root, client)
	h.a.lsp = m
	defer m.StopWait(time.Second)
	d := h.open("a.go")
	st := h.ed(d)
	st.GoTo(d.buf.Pos(at(d, "F()")))
	st.Label = "Source Editor"
	tt := ui.NewTester(func(c *ui.Context) { editor.View(c, st).Fill() }, 600, 400)
	if err := tt.RightClick("Source Editor"); err != nil {
		t.Fatal(err)
	}
	items := tt.Menu()
	for _, label := range []string{"Go to Definition", "Copy", "Find References", "Find Callers"} {
		if !slices.Contains(items, label) {
			t.Fatalf("missing %q in %v", label, items)
		}
	}
	if err := tt.ChooseMenuItem("Find References"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.choices != nil })
	if h.a.choices.title != "references" {
		t.Fatalf("choice = %+v", h.a.choices)
	}
	if err := tt.RightClick("Source Editor"); err != nil {
		t.Fatal(err)
	}
	if err := tt.ChooseMenuItem("Find Callers"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.choices != nil && h.a.choices.title == "callers" })
}

func TestReferencesMenuUsesRightClickPosition(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "package a\nfunc G() { F() }"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.lsp.Stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := serverOf(root, client)
	h.a.lsp = m
	defer m.StopWait(time.Second)
	d := h.open("a.go")
	h.ed(d).GoTo(d.buf.Pos(at(d, "F()")))
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("editor caret missing")
	}
	clickX, clickY := caret.X+3, caret.Y+caret.H/2
	h.tt.RightClickAt(clickX, clickY)
	if err := h.tt.ChooseMenuItem("Find References"); err != nil {
		t.Fatalf("menu %v: %v", h.tt.Menu(), err)
	}
	h.settle(func() bool { return h.a.choices != nil })
	if _, ok := h.tt.Find("Close References Preview"); !ok {
		t.Fatal("references preview is missing")
	}
}
