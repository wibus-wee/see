package app

import (
	"os"
	"path/filepath"
	"see/internal/config"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestNewFileShortcut(t *testing.T) {
	root := writeTree(t, map[string]string{"existing.txt": "keep"})
	h := newHarness(t, root)
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	path := filepath.Join(root, "new.txt")
	called := false
	h.host.newFile = func(dir string) (string, error) { called = true; return path, nil }
	h.tt.Key(ui.Cmd, ui.KeyN)
	d := h.a.docs[h.a.activePath()]
	if d == nil || !d.untitled || called {
		t.Fatal("new file did not open directly")
	}
	if h.tt.HasText(d.path) {
		t.Fatal("cache path is visible")
	}
	d.buf.Set("draft text")
	h.tt.Frame()
	cachePath := d.path
	t.Cleanup(func() { os.Remove(cachePath) })
	h.settle(func() bool { b, _ := os.ReadFile(cachePath); return string(b) == "draft text" })
	h.a.saveConfig()
	h.a.closeWorkspace()
	h.a.cfg = config.Load(h.a.cfg.Path())
	h.a.openWorkspace(root)
	h.settle(func() bool { return h.a.docs[cachePath] != nil && h.a.docs[cachePath].buf != nil })
	d = h.a.docs[cachePath]
	if !d.untitled || d.buf.String() != "draft text" || !d.buf.Modified() {
		t.Fatal("draft was not restored")
	}
	h.tt.Key(ui.Cmd, ui.KeyS)
	h.settle(func() bool { return h.a.docs[path] == d })
	if data, err := os.ReadFile(path); err != nil || string(data) != "draft text" || d.untitled {
		t.Fatalf("saved file = %q, %v", data, err)
	}
}

func TestEmptyUntitledClosesWithoutConfirmation(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.tt.Key(ui.Cmd, ui.KeyN)
	path := h.a.activePath()
	h.tt.Key(ui.Cmd, ui.KeyW)
	if h.a.confirm != nil || h.a.docs[path] != nil {
		t.Fatal("empty file did not close")
	}
}

func TestDirtyConfirmationUsesDialog(t *testing.T) {
	h := newHarness(t, t.TempDir())
	h.tt.Key(ui.Cmd, ui.KeyN)
	d := h.a.docs[h.a.activePath()]
	d.buf.Set("draft")
	h.host.confirm = func(string) (int, error) { return 2, nil }
	h.tt.Key(ui.Cmd, ui.KeyW)
	h.settle(func() bool { return h.a.confirm == nil })
	if h.a.docs[d.path] != d {
		t.Fatal("cancel closed the file")
	}
	h.host.confirm = func(string) (int, error) { return 1, nil }
	h.tt.Key(ui.Cmd, ui.KeyW)
	h.settle(func() bool { return h.a.docs[d.path] == nil })
}

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
