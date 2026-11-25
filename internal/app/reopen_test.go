package app

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestReopenClosedTabRestoresFileAndPosition(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "one\ntwo\nthree", "b.txt": "bee"}))
	d := h.open("a.txt")
	g := h.a.focus
	g.eds[d.path].GoTo(editor.Pos{Line: 1, Col: 2})
	h.open("b.txt")
	h.a.closeDoc(d.path)
	if g.has(d.path) {
		t.Fatal("tab did not close")
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyT)
	h.tt.Frame()
	if !g.has(d.path) || g.active != d.path || g.pending[d.path].pos == nil {
		t.Fatalf("tab did not reopen with its place: order=%v active=%q pending=%+v", g.order, g.active, g.pending[d.path])
	}
	if pos := *g.pending[d.path].pos; pos != (editor.Pos{Line: 1, Col: 2}) {
		t.Fatalf("reopened at %v, want the cursor it closed at", pos)
	}
}

func TestReopenClosedTabSkipsDiffTabs(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	g := h.a.focus
	key := diffTab("a.txt", false)
	g.diffs = map[string]*gitDiffTab{key: {name: "a.txt"}}
	g.addTab(key, len(g.order))
	h.a.closeTab(g, key)
	if len(h.a.closed) != 0 {
		t.Fatalf("diff tab was recorded for reopening: %+v", h.a.closed)
	}
}

func TestReopenClosedTabSkipsDirtyFiles(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	d := h.open("a.txt")
	d.buf.Set("unsaved edit")
	h.a.closeTab(h.a.focus, d.path)
	if len(h.a.closed) != 0 {
		t.Fatal("an unsaved file was recorded: its reopening would drop the edit")
	}
	if h.a.confirm == nil {
		t.Fatal("closing an unsaved file did not ask")
	}
}

func TestReopenClosedTabIsLIFOAndCapped(t *testing.T) {
	files := map[string]string{}
	for i := 0; i < maxClosedTabs+3; i++ {
		files[filepath.Join("f", string(rune('a'+i))+".txt")] = "text"
	}
	h := newHarness(t, writeTree(t, files))
	for i := 0; i < maxClosedTabs+3; i++ {
		p := filepath.Join(h.a.root, "f", string(rune('a'+i))+".txt")
		h.a.userOpen(p)
		d := h.a.docs[p]
		h.settle(func() bool { return !d.loading })
		h.a.closeDoc(p)
	}
	if len(h.a.closed) != maxClosedTabs {
		t.Fatalf("len(closed) = %d, want %d", len(h.a.closed), maxClosedTabs)
	}
	last := h.a.closed[len(h.a.closed)-1].path
	if filepath.Base(last) != string(rune('a'+maxClosedTabs+2))+".txt" {
		t.Fatalf("last closed = %q", last)
	}
	h.a.reopenClosedTab()
	if !h.a.focus.has(last) || len(h.a.closed) != maxClosedTabs-1 {
		t.Fatalf("reopen: order=%v closed=%d", h.a.focus.order, len(h.a.closed))
	}
}

func TestReopenClosedTabReopensMarkdownPreviewTab(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"readme.md": "# T\n"}))
	d := h.open("readme.md")
	g := h.a.focus
	h.a.openMarkdownTab(g)
	key := markdownTab(d.path)
	h.a.closeTab(g, key)
	if len(h.a.closed) != 1 {
		t.Fatalf("closed = %+v", h.a.closed)
	}
	h.a.reopenClosedTab()
	if !g.has(key) || g.active != key {
		t.Fatalf("markdown preview did not reopen: order=%v active=%q", g.order, g.active)
	}
}

func TestReopenClosedTabDeduplicatesSamePath(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text", "b.txt": "bee"}))
	d := h.open("a.txt")
	h.a.closeDoc(d.path)
	h.open("b.txt")
	e := h.a.closed
	if len(e) != 1 || e[0].path != d.path {
		t.Fatalf("closed = %+v", e)
	}
	h.a.reopenClosedTab()
	h.a.closeDoc(d.path)
	h.a.reopenClosedTab()
	if !h.a.focus.has(d.path) || len(h.a.closed) != 0 {
		t.Fatalf("second reopen: order=%v closed=%v", h.a.focus.order, h.a.closed)
	}
}

func TestReopenClosedTabRestoresPreviewTab(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text", "b.txt": "bee"}))
	d := h.open("a.txt")
	g := h.a.focus
	g.preview = d.path
	h.a.closeTab(g, d.path)
	if e := h.a.closed[len(h.a.closed)-1]; !e.preview {
		t.Fatal("the tab's preview state was not recorded")
	}
	h.open("b.txt")
	h.a.reopenClosedTab()
	if g.preview != d.path {
		t.Fatalf("reopened as a kept tab, not the preview it was: preview=%q", g.preview)
	}
}

func TestReopenClosedTabCommandEnabledOnlyWithHistory(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	if h.a.commandEnabled("tab.reopen") {
		t.Fatal("enabled with nothing closed")
	}
	d := h.open("a.txt")
	h.a.closeDoc(d.path)
	if !h.a.commandEnabled("tab.reopen") {
		t.Fatal("disabled with a closed tab to reopen")
	}
}
