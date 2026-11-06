package app

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestTabContextCloseRightAndSaved(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a", "b.txt": "b", "c.txt": "c"}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	h.open("c.txt")
	g := h.a.focus
	h.a.sidebarOff = true
	h.tt.Frame()
	if err := h.tt.RightClick("Close b.txt"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Close Tabs to the Right"); err != nil {
		t.Fatalf("%v; menu=%v", err, h.tt.Menu())
	}
	h.tt.Frame()
	if !slices.Equal(g.order, []string{a.path, b.path}) {
		t.Fatalf("tabs: %v", g.order)
	}
	b.buf.Set("edited")
	h.a.openPalette()
	h.a.quick.query = ">Close Saved Tabs"
	h.a.rankQuick()
	h.a.chooseQuick(0)
	h.tt.Frame()
	if !slices.Equal(g.order, []string{b.path}) || h.a.confirm != nil {
		t.Fatalf("saved action: tabs %v confirmation %v", g.order, h.a.confirm)
	}
}

func TestBulkCloseCancelKeepsEveryTab(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.md": "a", "b.txt": "b", "c.txt": "c"}))
	a := h.open("a.md")
	b := h.open("b.txt")
	c := h.open("c.txt")
	g := h.a.focus
	b.buf.Set("edited")
	before := slices.Clone(g.order)
	h.host.confirm = func(string) (int, error) { return 2, nil }
	h.a.closeTabs(g, a.path, closeOtherTabs)
	h.settle(func() bool { return h.a.confirm == nil })
	if !slices.Equal(g.order, before) || b.buf.String() != "edited" {
		t.Fatalf("cancel removed tabs: %v", g.order)
	}
	h.host.confirm = func(string) (int, error) { return 1, nil }
	h.a.closeTabs(g, a.path, closeOtherTabs)
	h.settle(func() bool { return h.a.confirm == nil })
	if !slices.Equal(g.order, []string{a.path}) || h.a.docs[b.path] != nil || h.a.docs[c.path] != nil {
		t.Fatalf("discard tabs: %v", g.order)
	}
}

func TestBulkClosePreservesSharedDirtyBuffers(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.md": "# a", "b.txt": "b"}))
	a := h.open("a.md")
	g := h.a.focus
	h.a.splitActive(sideRight)
	other := h.a.focus
	h.a.focus = g
	b := h.open("b.txt")
	a.buf.Set("# edited")
	h.a.closeTabs(g, b.path, closeOtherTabs)
	h.tt.Frame()
	if h.a.confirm != nil || !other.has(a.path) || h.a.docs[a.path] == nil || a.buf.String() != "# edited" {
		t.Fatal("shared buffer was discarded")
	}
	h.a.focus = other
	h.a.openMarkdownTab(other)
	h.tt.Frame()
	other.addTab(markdownTab(a.path), len(other.order))
	h.a.closeTabs(other, a.path, closeOtherTabs)
	if h.a.confirm != nil || !other.has(a.path) || other.has(markdownTab(a.path)) {
		t.Fatalf("preview close: %v", other.order)
	}
	if !g.has(filepath.Join(h.a.root, "b.txt")) {
		t.Fatal("other group changed")
	}
}
