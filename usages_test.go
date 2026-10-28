package main

import (
	"context"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

type usageClient struct {
	*fakeClient
	target string
}

func (c *usageClient) References(context.Context, string, lsp.Position) ([]lsp.Location, error) {
	return []lsp.Location{{URI: uriOf(c.target), Range: lsp.Range{Start: lsp.Position{Line: 0}}}}, nil
}
func (c *usageClient) Callers(context.Context, string, lsp.Position) ([]lsp.Location, error) {
	return []lsp.Location{{URI: uriOf(c.target), Range: lsp.Range{Start: lsp.Position{Line: 0}}}}, nil
}

func TestFindUsagesShowsResultsAndNavigates(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "package a\nfunc G() { F() }"})
	h := newHarness(t, root)
	h.a.cfg.trust(root)
	old := h.a.lsp
	old.stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := newGoLSP(root, func(context.Context, string, func(error)) (langClient, error) { return client, nil }, nil)
	h.a.lsp = m
	defer m.stopWait(time.Second)
	var menu []*mygo.MenuItem
	h.a.popupMenu = func(x, y int, items []*mygo.MenuItem) { menu = items }
	d := h.open("a.go")
	h.ed(d).GoTo(d.buf.Pos(at(d, "F()")))
	h.a.findUsages(false)
	h.settle(func() bool { return h.a.choices != nil })
	if h.a.choices.title != "references" || len(menu) != 3 {
		t.Fatalf("results %v", h.tt.Texts())
	}
	menu[2].Click(menu[2], nil)
	h.settle(func() bool { return h.a.activePath() == filepath.Join(root, "b.go") })
	h.a.activate(d.path)
	h.a.findUsages(true)
	h.settle(func() bool { return h.a.choices != nil })
	if h.a.choices.title != "callers" || len(menu) != 3 {
		t.Fatalf("callers %v", h.tt.Texts())
	}
}

func TestEditorContextMenuFindsReferencesAndCallers(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc F() {}", "b.go": "package a\nfunc G() { F() }"})
	h := newHarness(t, root)
	h.a.cfg.trust(root)
	h.a.lsp.stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := newGoLSP(root, func(context.Context, string, func(error)) (langClient, error) { return client, nil }, nil)
	h.a.lsp = m
	defer m.stopWait(time.Second)
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
	h.a.cfg.trust(root)
	h.a.lsp.stop()
	client := &usageClient{fakeClient: &fakeClient{enc: lsp.UTF16}, target: filepath.Join(root, "b.go")}
	m := newGoLSP(root, func(context.Context, string, func(error)) (langClient, error) { return client, nil }, nil)
	h.a.lsp = m
	defer m.stopWait(time.Second)
	var x, y int
	h.a.popupMenu = func(px, py int, _ []*mygo.MenuItem) { x, y = px, py }
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
	if absInt(x-int(clickX)) > 2 || absInt(y-int(clickY)) > 2 {
		t.Fatalf("menu at (%d,%d), right-click at (%v,%v)", x, y, clickX, clickY)
	}
}

func absInt(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
