package app

import (
	"bytes"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"image"
	"image/png"
	"testing"
)

func TestEditorGroupKeyboardNavigation(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first", "b.txt": "second"}))
	a := h.open("a.txt")
	left := h.a.focus
	left.eds[a.path].Select(2, 2)
	h.a.splitActive(sideRight)
	right := h.a.focus
	b := h.open("b.txt")
	right.eds[b.path].Select(3, 3)
	h.tt.Frame()
	h.tt.Key(0, ui.KeyF6)
	h.tt.Frame()
	if h.a.focus != left || h.a.activeEditor().Cursor() != 2 {
		t.Fatalf("next editor: focus=%d left=%d cursor=%d pending=%v", h.a.focus.id, left.id, h.a.activeEditor().Cursor(), h.a.focusGroupTarget)
	}
	h.tt.Key(ui.Shift, ui.KeyF6)
	h.tt.Frame()
	if h.a.focus != right || h.a.activeEditor().Cursor() != 3 {
		t.Fatal("previous editor did not wrap preserving cursor")
	}
	b.buf.Set("edited second")
	h.a.openPalette()
	h.a.quick.query = ">Move Tab to Next Editor"
	h.a.rankQuick()
	h.a.chooseQuick(0)
	h.tt.Frame()
	if h.a.focus != left || !left.has(b.path) || right.has(b.path) || h.a.docs[b.path] != b || h.a.confirm != nil {
		t.Fatal("move lost shared buffer or did not focus destination")
	}
	h.a.moveTabToNextEditor()
	h.tt.Frame()
	if !right.has(b.path) || h.a.focus != right {
		t.Fatal("move did not advance")
	}
}

func TestEditorGroupMoveRemovesEmptySource(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first"}))
	d := h.open("a.txt")
	original := h.a.focus
	h.a.splitActive(sideRight)
	source := h.a.focus
	h.a.moveTabToNextEditor()
	h.tt.Frame()
	if len(h.a.groups()) != 1 || h.a.focus != original || source.has(d.path) || h.a.docs[d.path] != d {
		t.Fatal("empty source not removed safely")
	}
	h.tt.Key(0, ui.KeyF6)
	h.tt.Key(ui.Shift, ui.KeyF6)
	h.a.moveTabToNextEditor()
	if h.a.focus != original || !original.has(d.path) {
		t.Fatal("single-group no-op changed tabs")
	}
}

func TestEditorGroupFocusNonTextViews(t *testing.T) {
	var pixels bytes.Buffer
	if err := png.Encode(&pixels, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"empty", "markdown", "image", "diff", "hex"} {
		t.Run(kind, func(t *testing.T) {
			h := newHarness(t, writeTree(t, map[string]string{"source.txt": "source text", "readme.md": "# Preview", "pixel.png": pixels.String(), "bytes.txt": "hex bytes"}))
			sourceDoc := h.open("source.txt")
			source := h.a.focus
			originalEditor := h.a.activeEditor()
			destination := h.a.splitBeside(source, sideRight)
			h.a.focus = destination
			switch kind {
			case "markdown":
				h.open("readme.md")
				h.a.openMarkdownTab(destination)
			case "image":
				h.open("pixel.png")
			case "diff":
				key := diffTab("source.txt", false)
				destination.addTab(key, 0)
				destination.diffs = map[string]*gitDiffTab{key: h.a.newGitDiffTab(editor.FilePatch{}, "source.txt", false)}
				h.a.activateIn(destination, key)
			case "hex":
				d := h.open("bytes.txt")
				destination.hexPreview = map[string]bool{d.path: true}
			}
			h.a.requestGroupFocus(source)
			h.tt.Frame()
			h.tt.Frame()
			if !originalEditor.Focused() {
				t.Fatal("source did not receive initial keyboard focus")
			}
			h.tt.Key(0, ui.KeyF6)
			h.tt.Frame()
			h.tt.Frame()
			if h.a.focus != destination || originalEditor.Focused() {
				t.Fatalf("F6 did not transfer keyboard to %s", kind)
			}
			h.tt.Type("X")
			h.tt.Frame()
			if sourceDoc.buf.String() != "source text" || h.a.focus != destination {
				t.Fatal("typing reached the previous editor")
			}
			h.tt.Key(ui.Shift, ui.KeyF6)
			h.tt.Frame()
			h.tt.Frame()
			if h.a.focus != source || !originalEditor.Focused() {
				t.Fatal("reverse traversal did not restore text focus")
			}
			if kind != "empty" {
				h.a.requestGroupFocus(destination)
				h.tt.Frame()
				h.tt.Frame()
				tab := destination.active
				h.a.moveTabToNextEditor()
				h.tt.Frame()
				h.tt.Frame()
				if h.a.focus != source || source.active != tab || !source.has(tab) {
					t.Fatal("non-text move did not focus destination")
				}
				before := sourceDoc.buf.String()
				h.tt.Type("Y")
				h.tt.Frame()
				if sourceDoc.buf.String() != before {
					t.Fatal("non-text move left keyboard in previous text editor")
				}
			}
		})
	}
}
