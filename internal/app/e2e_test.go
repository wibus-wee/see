package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The end-to-end tests drive the app as a reader does: a folder opens, its
// files show, a file opens in an editor, and the edits reach the disk.
// They need no window: the harness runs the view, and the app's background
// work hands its results back to the test.

func TestOpensAWorkspaceEditsAndSaves(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go":      "package a\n",
		"docs/b.md": "# b\n",
	})
	h := newHarness(t, root)

	// The tree shows the folder's entries.
	for _, name := range []string{"a.go", "docs"} {
		if !h.treeHas(filepath.Join(root, name)) {
			t.Fatalf("the tree does not show %q", name)
		}
	}

	// A file opens with its text, and takes an edit.
	d := h.open("a.go")
	if got := d.buf.String(); got != "package a\n" {
		t.Fatalf("opened %q", got)
	}
	d.buf.Set("package a\n\nfunc main() {}\n")
	if len(h.a.dirtyDocs()) != 1 {
		t.Fatalf("dirty docs = %d, want the edited file", len(h.a.dirtyDocs()))
	}

	// The edit reaches the disk, and the file is no longer dirty.
	if !h.a.saveDoc(d, false) {
		t.Fatalf("save refused: %s", h.a.notice)
	}
	h.settle(func() bool { return len(h.a.dirtyDocs()) == 0 })
	on, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(on) != "package a\n\nfunc main() {}\n" {
		t.Fatalf("on disk %q", on)
	}
}

func TestSaveRefusesAFileChangedOnDisk(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	d := h.open("a.go")
	d.buf.Set("package a\n\nvar X = 1\n")

	// Something else writes the file; saving must not lose its work.
	if err := os.WriteFile(filepath.Join(root, "a.go"), []byte("package a\n\nvar Y = 2\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if h.a.saveDoc(d, false) {
		t.Fatal("saved over a file changed on disk")
	}
	if !d.conflict {
		t.Fatal("the conflict is not shown")
	}
	if !h.a.saveDoc(d, true) { // Overwrite
		t.Fatalf("overwrite refused: %s", h.a.notice)
	}
	on, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(on) != "package a\n\nvar X = 1\n" {
		t.Fatalf("on disk %q", on)
	}
}

func TestUnsavedChangesHoldTheQuit(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	d := h.open("a.go")
	d.buf.Set("package a\n\nvar X = 1\n")

	// Cancel keeps the app open and the edits in place.
	h.host.confirm = func(string) (int, error) { return 2, nil }
	quit := false
	h.a.guardDirty(h.a.dirtyDocs(), func() { quit = true })
	h.settle(func() bool { return h.a.confirm == nil })
	if quit {
		t.Fatal("cancel did not hold the quit")
	}
	if len(h.a.dirtyDocs()) != 1 {
		t.Fatal("cancel dropped the edits")
	}

	// Discard goes on without saving.
	h.host.confirm = func(string) (int, error) { return 1, nil }
	quit = false
	h.a.guardDirty(h.a.dirtyDocs(), func() { quit = true })
	h.settle(func() bool { return quit })
	on, err := os.ReadFile(filepath.Join(root, "a.go"))
	if err != nil {
		t.Fatal(err)
	}
	if string(on) != "package a\n" {
		t.Fatalf("discard wrote %q", on)
	}
	// The edit is still in the buffer: discarding lets the app go, it does
	// not restore the file.
	if got := d.buf.String(); got != "package a\n\nvar X = 1\n" {
		t.Fatalf("discard restored %q", got)
	}
}
