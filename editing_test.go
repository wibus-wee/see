package main

import (
	"os"
	"path/filepath"
	"testing"

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
