package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestMarkdownPreview(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Hello preview\n\n**Bold text**\n", "other.go": "package other\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyV)
	h.tt.Frame()
	if !h.a.focus.markdownPreview[d.path] || !h.tt.HasText("Hello preview") || !h.tt.HasText("Bold text") {
		t.Fatalf("preview not shown: %q", h.tt.Texts())
	}
	d.buf.Set("# Updated content\n")
	h.tt.Frame()
	if !h.tt.HasText("Updated content") || h.tt.HasText("Hello preview") {
		t.Fatalf("preview did not update: %q", h.tt.Texts())
	}
	h.click("Show Markdown Source")
	h.tt.Frame()
	if h.a.focus.markdownPreview[d.path] {
		t.Fatal("source button left preview open")
	}
	h.click("Show Markdown Preview")
	h.tt.Frame()
	h.a.openIn(h.a.focus, d.path, goTo{pos: &editor.Pos{Line: 0}})
	h.tt.Frame()
	if h.a.focus.markdownPreview[d.path] {
		t.Fatal("source navigation did not reveal editor")
	}
	h.open("other.go")
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyV)
	if h.a.focus.markdownPreview[filepath.Join(root, "other.go")] {
		t.Fatal("preview opened for Go file")
	}
}

func TestMarkdownPreviewPerGroup(t *testing.T) {
	root := writeTree(t, map[string]string{"README.md": "# Heading\n"})
	h := newHarness(t, root)
	d := h.open("README.md")
	first := h.a.focus
	h.a.toggleMarkdownPreview(first)
	h.a.splitActive(sideRight)
	second := h.a.focus
	if !first.markdownPreview[d.path] || second.markdownPreview[d.path] {
		t.Fatal("split preview state should be independent")
	}
	h.a.toggleMarkdownPreview(second)
	h.a.closeTab(first, d.path)
	if !second.markdownPreview[d.path] {
		t.Fatal("closing other group cleared preview")
	}
}
