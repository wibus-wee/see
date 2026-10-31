package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestCreateProjectEntries(t *testing.T) {
	root := writeTree(t, map[string]string{"existing": "keep"})
	h := newHarness(t, root)
	if err := h.tt.Click("New Folder"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("nested/folder")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { _, err := os.Stat(filepath.Join(root, "nested", "folder")); return err == nil })
	if err := h.tt.Click("New File"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("src/new.txt")
	h.tt.Key(0, ui.KeyEnter)
	path := filepath.Join(root, "src", "new.txt")
	h.settle(func() bool { return h.a.activePath() == path })
	if b, err := os.ReadFile(path); err != nil || len(b) != 0 {
		t.Fatalf("created file: %q, %v", b, err)
	}
	h.a.createEntry = &entryPrompt{dir: root, name: "existing"}
	h.a.submitEntry(h.a.createEntry)
	h.settle(func() bool { return h.a.createEntry != nil && h.a.createEntry.err != "" })
	if b, _ := os.ReadFile(filepath.Join(root, "existing")); string(b) != "keep" {
		t.Fatal("overwrote existing file")
	}
}

func TestExternalFileDrops(t *testing.T) {
	root := writeTree(t, map[string]string{"folder/child": "child", "anchor.txt": "anchor"})
	external := writeTree(t, map[string]string{"outside.txt": "outside"})
	h := newHarness(t, root)
	r, ok := h.tt.Find("folder")
	if !ok {
		t.Fatal("folder row missing")
	}
	if !h.tt.DropFiles(r.X+5, r.Y+r.H/2, filepath.Join(external, "outside.txt")) {
		t.Fatal("folder did not accept drop")
	}
	copied := filepath.Join(root, "folder", "outside.txt")
	h.settle(func() bool { _, err := os.Stat(copied); return err == nil })
	if b, err := os.ReadFile(copied); err != nil || string(b) != "outside" {
		t.Fatalf("copy: %q, %v", b, err)
	}
	h.a.userOpen(filepath.Join(root, "anchor.txt"))
	h.settle(func() bool { return h.a.docs[filepath.Join(root, "anchor.txt")].buf != nil })
	if !h.tt.DropFiles(650, 350, filepath.Join(external, "outside.txt")) {
		t.Fatal("editor did not accept drop")
	}
	if h.a.activePath() != filepath.Join(external, "outside.txt") {
		t.Fatalf("opened %q", h.a.activePath())
	}
}

func TestCopyEntryRejectsConflictsAndSelfCopy(t *testing.T) {
	root := writeTree(t, map[string]string{"dir/file": "source", "existing": "keep"})
	if err := copyEntry(filepath.Join(root, "dir"), filepath.Join(root, "dir", "copy")); err == nil {
		t.Fatal("copied folder into itself")
	}
	if err := copyEntry(filepath.Join(root, "dir", "file"), filepath.Join(root, "existing")); err == nil {
		t.Fatal("overwrote existing file")
	}
}

func TestTreeDragCopiesIntoFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"folder/child": "child", "source.txt": "source"})
	h := newHarness(t, root)
	source, ok := h.tt.Find("source.txt")
	if !ok {
		t.Fatal("source missing")
	}
	target, ok := h.tt.Find("folder")
	if !ok {
		t.Fatal("folder missing")
	}
	h.tt.Press(source.X+5, source.Y+source.H/2)
	h.tt.Move(source.X+20, source.Y+source.H/2)
	h.tt.Move(target.X+5, target.Y+target.H/2)
	h.tt.Release(target.X+5, target.Y+target.H/2)
	path := filepath.Join(root, "folder", "source.txt")
	h.settle(func() bool { _, err := os.Stat(path); return err == nil })
	if b, err := os.ReadFile(path); err != nil || string(b) != "source" {
		t.Fatalf("copy: %q %v", b, err)
	}
}

func TestNewEntryAppearsInsideSelectedFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"folder/child.txt": "child", "other.txt": "other"})
	h := newHarness(t, root)
	if err := h.tt.Click("folder"); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(root, "folder")
	h.settle(func() bool { d := h.a.dirs[folder]; return d != nil && d.loaded })
	if err := h.tt.Click("New File"); err != nil {
		t.Fatal(err)
	}
	if h.a.createEntry == nil || h.a.createEntry.dir != folder || !h.a.tree.Open.Has(folder) {
		t.Fatal("selected folder was not chosen and opened")
	}
	folderRow, inputRow := -1, -1
	for i := range h.a.tree.Rows() {
		if h.a.tree.Item(i) == folder {
			folderRow = i
		}
		if h.a.tree.Item(i) == folder+entrySuffix {
			inputRow = i
		}
	}
	if inputRow != folderRow+1 {
		t.Fatalf("input row %d is not immediately inside folder row %d", inputRow, folderRow)
	}
	h.tt.Type("deep/new.txt")
	h.tt.Key(0, ui.KeyEnter)
	path := filepath.Join(folder, "deep", "new.txt")
	h.settle(func() bool { return h.a.activePath() == path })
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
}

func TestNewFileRowFollowsFoldersAndMatchesHeight(t *testing.T) {
	root := writeTree(t, map[string]string{"alpha/a": "", "beta/b": "", "first.txt": "", "second.txt": ""})
	h := newHarness(t, root)
	if err := h.tt.Click("New File"); err != nil {
		t.Fatal(err)
	}
	var paths []string
	for i := range h.a.tree.Rows() {
		paths = append(paths, h.a.tree.Item(i))
	}
	if len(paths) < 4 || paths[0] != filepath.Join(root, "alpha") || paths[1] != filepath.Join(root, "beta") || paths[2] != root+entrySuffix || paths[3] != filepath.Join(root, "first.txt") {
		t.Fatalf("unexpected rows: %v", paths)
	}
	input, ok := h.tt.Find("New File name")
	if !ok {
		t.Fatal("input missing")
	}
	first, ok := h.tt.Find("first.txt")
	if !ok {
		t.Fatal("file missing")
	}
	second, ok := h.tt.Find("second.txt")
	if !ok {
		t.Fatal("second file missing")
	}
	if delta := first.Y + first.H/2 - input.Y - input.H/2; delta > second.Y-first.Y+3 || delta < second.Y-first.Y-3 {
		t.Fatalf("input row spacing %v differs from file row spacing %v", delta, second.Y-first.Y)
	}
}

func TestMoveFolderToTrashFromTree(t *testing.T) {
	root := writeTree(t, map[string]string{"folder/child.txt": "child"})
	h := newHarness(t, root)
	trashed := make(chan string, 1)
	h.host.trash = func(path string) error { err := os.RemoveAll(path); trashed <- path; return err }
	if err := h.tt.RightClick("folder"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Move to Trash"); err != nil {
		t.Fatal(err)
	}
	if h.a.deleteConfirm == nil {
		t.Fatal("confirmation missing")
	}
	h.a.deleteConfirm.actions[1].fn()
	select {
	case <-trashed:
		t.Fatal("cancel moved folder")
	default:
	}
	h.a.requestTrash(filepath.Join(root, "folder"))
	h.a.deleteConfirm.actions[0].fn()
	h.settle(func() bool { _, err := os.Stat(filepath.Join(root, "folder")); return os.IsNotExist(err) })
	h.settle(func() bool { d := h.a.dirs[root]; return d != nil && d.loaded && len(d.entries) == 0 })
	if !h.tt.HasText("This folder is empty.") {
		t.Fatal("empty tree not refreshed")
	}
	if got := <-trashed; got != filepath.Join(root, "folder") {
		t.Fatalf("moved %q", got)
	}
}
