package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
)

// Stashing from the Changes header names the stash, lists it under
// the changes, shows it as a commit, applies it and drops it.
func TestStashChangesBrowseApplyDrop(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n"})
	writeFile(t, root, "a.txt", "a\nchanged\n")
	writeFile(t, root, "new.txt", "new\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.historyClosed = true
	h.tt.Frame()

	if err := h.tt.Click("Stash Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	h.tt.Type("parser work")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.stashes) == 1 })
	if len(h.a.git.changes) != 0 || h.a.git.stashing || h.a.git.stashMessage != "" {
		t.Fatalf("after stashing: changes %v, stashing %v %q", h.a.git.changes, h.a.git.stashing, h.a.git.stashMessage)
	}
	if _, err := os.Stat(filepath.Join(root, "new.txt")); !os.IsNotExist(err) {
		t.Fatalf("untracked file not stashed: %v", err)
	}
	s := h.a.git.stashes[0]
	if s.Message != "parser work" || s.Branch == "" {
		t.Fatalf("stash %+v", s)
	}
	h.tt.Frame()
	if err := h.tt.Click("Stash: parser work"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.focus.active == commitTab(s.Hash) })
	v := h.a.focus.diffs[commitTab(s.Hash)]
	h.settle(func() bool { return !v.commit.loading })
	if v.name != "parser work" || len(v.commit.details.Files) != 1 || v.commit.details.Files[0].Path != "a.txt" {
		t.Fatalf("stash tab %q %+v", v.name, v.commit.details)
	}

	if err := h.tt.RightClick("Stash: parser work"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Apply Stashed Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.changes) == 2 })
	if b, _ := os.ReadFile(filepath.Join(root, "a.txt")); !strings.Contains(string(b), "changed") {
		t.Fatalf("a.txt %q", b)
	}
	if len(h.a.git.stashes) != 1 {
		t.Fatalf("apply dropped the stash: %+v", h.a.git.stashes)
	}

	if err := h.tt.RightClick("Stash: parser work"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Drop Stash…"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.deleteConfirm == nil {
		t.Fatal("drop did not ask")
	}
	if err := h.tt.Click("Drop Stash"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.stashes) == 0 })
	h.tt.Frame()
	if h.tt.HasText("Stashed Changes") {
		t.Fatal("empty stash list still shows")
	}
}

// Stashing waits for unsaved edits, which the stash would leave behind.
func TestStashRefusesUnsavedEdits(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n"})
	writeFile(t, root, "a.txt", "a\nchanged\n")
	h := newHarness(t, root)
	d := h.open("a.txt")
	d.buf.Set("edited buffer")
	h.readGit()
	h.a.beginStash()
	h.a.stashJob("", func() error { t.Fatal("stashed over unsaved edits"); return nil }, nil)
	if h.a.git.busy {
		t.Fatal("busy")
	}
}
