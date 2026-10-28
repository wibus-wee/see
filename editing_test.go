package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestEditingSavePreservesEncodingAndProtectsExternalChanges(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	original := "\xef\xbb\xbfone\r\ntwo\r\n"
	if err := os.WriteFile(path, []byte(original), 0640); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.a.openFile(path, nil)
	h.settle(func() bool { return d.buf != nil })
	if h.ed(d).ReadOnly {
		t.Fatal("file is read-only")
	}
	d.buf.Edit(editor.Edit{Start: 0, End: 3, Text: "changed"})
	if !h.a.saveDoc(d, false) {
		t.Fatal("save failed")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "\xef\xbb\xbfchanged\r\ntwo\r\n" {
		t.Fatalf("saved %q", got)
	}
	if d.buf.Modified() {
		t.Fatal("still dirty after save")
	}
	d.buf.Edit(editor.Edit{Start: 0, End: 7, Text: "local"})
	if err := os.WriteFile(path, []byte("external"), 0640); err != nil {
		t.Fatal(err)
	}
	if h.a.saveDoc(d, false) || !d.conflict {
		t.Fatal("external change was not protected")
	}
	got, _ = os.ReadFile(path)
	if string(got) != "external" {
		t.Fatalf("overwrote external changes: %q", got)
	}
	if !h.a.saveDoc(d, true) {
		t.Fatal("explicit overwrite failed")
	}
}

func TestDirtyCloseAndWorkspaceSwitchRequireDecision(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.a.openFile(path, nil)
	h.settle(func() bool { return d.buf != nil })
	d.buf.Set("edited")
	h.a.closeDoc(path)
	if h.a.confirm == nil || !h.a.focus.has(path) {
		t.Fatal("dirty tab closed without asking")
	}
	h.a.confirm.actions[2].fn()
	h.a.openWorkspace(t.TempDir())
	if h.a.confirm == nil || h.a.root != root {
		t.Fatal("workspace switched without asking")
	}
	h.a.confirm.actions[0].fn()
	got, _ := os.ReadFile(path)
	if string(got) != "edited" || h.a.root == root {
		t.Fatal("save and switch did not complete")
	}
}

func TestEditAndSaveWithKeyboard(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "a.txt")
	if err := os.WriteFile(path, []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	d := h.a.openFile(path, nil)
	h.settle(func() bool { return d.buf != nil })
	h.a.focusEditor = h.ed(d)
	h.tt.Frame()
	h.tt.Type("X")
	if !d.buf.Modified() {
		t.Fatalf("typing did not edit: %q", d.buf.String())
	}
	h.tt.Key(ui.Cmd, ui.KeyS)
	got, err := os.ReadFile(path)
	if err != nil || string(got) != d.buf.String() || d.buf.Modified() {
		t.Fatalf("keyboard save: %q, %v", got, err)
	}
}

func TestFindInFileFocusesQuery(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "one needle two needle"})
	h := newHarness(t, root)
	d := h.a.openFile(filepath.Join(root, "a.txt"), nil)
	h.settle(func() bool { return d.buf != nil && h.ed(d) != nil })
	h.a.focusEditor = h.ed(d)
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyF)
	h.tt.Type("needle")
	if count, _ := h.ed(d).FindMatches(); count != 2 {
		t.Fatalf("find matches %d after typing, document %q", count, d.buf.String())
	}
	if d.buf.String() != "one needle two needle" {
		t.Fatalf("typing changed document: %q", d.buf.String())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Click("Show the Find Navigator")
	h.tt.Key(ui.Cmd, ui.KeyF)
	h.tt.Type("one")
	if count, _ := h.ed(d).FindMatches(); count != 1 {
		t.Fatalf("find matches %d after opening from sidebar", count)
	}
}
