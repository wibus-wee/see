package main

import (
	"github.com/egoist/mygo/ui"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestRenameFolderPreservesOpenDirtyDocument(t *testing.T) {
	root := writeTree(t, map[string]string{"old/a.txt": "original"})
	h := newHarness(t, root)
	d := h.open("old/a.txt")
	d.buf.Set("edited")
	h.tt.Frame()
	old, next := filepath.Join(root, "old"), filepath.Join(root, "new")
	h.a.beginRename(old)
	q := h.a.renameEntry
	q.name = "new"
	h.a.submitRename(q)
	newFile := filepath.Join(next, "a.txt")
	h.settle(func() bool { return h.a.docs[newFile] == d })
	if d.buf.String() != "edited" || !d.buf.Modified() || h.a.focus.active != newFile {
		t.Fatal("document state lost")
	}
	if _, err := os.Stat(newFile); err != nil {
		t.Fatal(err)
	}
	if h.a.docs[filepath.Join(old, "a.txt")] != nil {
		t.Fatal("old document remains")
	}
}

func TestRenameRejectsExistingTarget(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "a", "b.txt": "b"})
	h := newHarness(t, root)
	h.a.beginRename(filepath.Join(root, "a.txt"))
	q := h.a.renameEntry
	q.name = "b.txt"
	h.a.submitRename(q)
	h.settle(func() bool { return h.a.renameEntry == q })
	if q.err == "" {
		t.Fatal("missing collision error")
	}
	b, _ := os.ReadFile(filepath.Join(root, "b.txt"))
	if string(b) != "b" {
		t.Fatal("target overwritten")
	}
}

func TestRenameWithReturn(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("macOS gesture")
	}
	root := writeTree(t, map[string]string{"a.txt": "hello"})
	h := newHarness(t, root)
	h.click("a.txt")
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if h.a.renameEntry == nil {
		t.Fatal("Return did not begin rename")
	}
	h.tt.Type("renamed")
	h.tt.Key(0, ui.KeyEnter)
	next := filepath.Join(root, "renamed.txt")
	h.settle(func() bool { _, err := os.Stat(next); return err == nil })
}
