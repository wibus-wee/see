package app

import (
	"fmt"
	"os"
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

func TestBulkCloseSavesUntitledCandidate(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"keep.txt": "keep"}))
	keep := h.open("keep.txt")
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	draft.buf.Set("draft contents")
	destination := filepath.Join(h.a.root, "saved.txt")
	h.host.newFile = func(string) (string, error) { return destination, nil }
	h.host.confirm = func(string) (int, error) { return 0, nil }
	g := h.a.focus
	h.a.closeTabs(g, keep.path, closeOtherTabs)
	h.settle(func() bool { return !draft.untitled && h.a.confirm == nil })
	if !slices.Equal(g.order, []string{keep.path}) || h.a.docs[destination] != nil {
		t.Fatalf("saved candidate remains open: %v", g.order)
	}
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "draft contents" {
		t.Fatalf("saved contents %q: %v", data, err)
	}
}

func TestBulkCloseReconfirmsEditsWhilePromptIsPending(t *testing.T) {
	for _, editOriginal := range []bool{false, true} {
		t.Run(fmt.Sprint("editOriginal=", editOriginal), func(t *testing.T) {
			h := newHarness(t, writeTree(t, map[string]string{"keep.txt": "keep", "dirty.txt": "dirty", "clean.txt": "clean"}))
			keep := h.open("keep.txt")
			dirty := h.open("dirty.txt")
			clean := h.open("clean.txt")
			dirty.buf.Set("first edit")
			g := h.a.focus
			original := slices.Clone(g.order)
			h.a.closeTabs(g, keep.path, closeOtherTabs)
			h.settle(func() bool { return h.a.confirm != nil && h.a.confirm.inWindow })
			first := h.a.confirm
			changed := clean
			if editOriginal {
				changed = dirty
			}
			changed.buf.Set("unconfirmed edit")
			first.actions[1].fn()
			if h.a.confirm == nil || h.a.confirm == first || !slices.Equal(g.order, original) {
				t.Fatalf("discard bypassed new edit confirmation: %v", g.order)
			}
			h.a.confirm.actions[2].fn()
			if changed.buf.String() != "unconfirmed edit" || !slices.Equal(g.order, original) {
				t.Fatal("Cancel discarded unconfirmed edits")
			}
		})
	}
}

func TestBulkCloseKeepsTabsOpenedDuringPrompt(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"keep.txt": "keep", "dirty.txt": "dirty", "later.txt": "later"}))
	keep := h.open("keep.txt")
	dirty := h.open("dirty.txt")
	dirty.buf.Set("approved edit")
	g := h.a.focus
	h.a.closeTabs(g, keep.path, closeOtherTabs)
	h.settle(func() bool { return h.a.confirm != nil && h.a.confirm.inWindow })
	later := h.open("later.txt")
	later.buf.Set("new unsaved tab")
	h.a.confirm.actions[1].fn()
	if h.a.confirm != nil || !slices.Equal(g.order, []string{keep.path, later.path}) || later.buf.String() != "new unsaved tab" {
		t.Fatalf("new tab was included in old close operation: %v", g.order)
	}
}
