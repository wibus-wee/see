package app

import (
	"os"
	"path/filepath"
	"see/internal/config"
	"testing"
)

func TestNamedFileRecoveryAndConflict(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "original"})
	h := newHarness(t, root)
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	d := h.open("a.txt")
	d.buf.Set("unsaved")
	h.tt.Frame()
	path := d.path
	h.settle(func() bool { r := readRecovery(root, path); return r != nil && r.Text == "unsaved" })
	h.a.saveConfig()
	h.a.closeWorkspace()
	if err := os.WriteFile(path, []byte("external"), 0644); err != nil {
		t.Fatal(err)
	}
	h.a.cfg = config.Load(h.a.cfg.Path())
	h.a.openWorkspace(root)
	h.settle(func() bool { return h.a.docs[path] != nil && h.a.docs[path].buf != nil })
	d = h.a.docs[path]
	if d.buf.String() != "unsaved" || !d.buf.Modified() || !d.conflict {
		t.Fatalf("recovery: %q dirty=%v conflict=%v", d.buf.String(), d.buf.Modified(), d.conflict)
	}
	if h.a.saveDoc(d, false) {
		t.Fatal("overwrote external change")
	}
	if !h.a.saveDoc(d, true) {
		t.Fatal("explicit overwrite failed")
	}
	if readRecovery(root, path) != nil {
		t.Fatal("recovery survived save")
	}
}

func TestNamedFileRecoveryWhenDeleted(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "original"})
	h := newHarness(t, root)
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	d := h.open("a.txt")
	d.buf.Set("unsaved")
	h.tt.Frame()
	path := d.path
	h.settle(func() bool { return readRecovery(root, path) != nil })
	h.a.saveConfig()
	h.a.closeWorkspace()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	h.a.cfg = config.Load(h.a.cfg.Path())
	h.a.openWorkspace(root)
	h.settle(func() bool { return h.a.docs[path] != nil && h.a.docs[path].buf != nil })
	d = h.a.docs[path]
	if d.buf.String() != "unsaved" || !d.gone || !d.buf.Modified() {
		t.Fatalf("recovery: %q gone=%v", d.buf.String(), d.gone)
	}
}
