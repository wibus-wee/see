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
	if v.name != "parser work" || len(v.commit.details.Files) != 2 || v.commit.details.Files[0].Path != "a.txt" || v.commit.details.Files[1].Path != "new.txt" {
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

// Each row's actions reach its own stash, not the newest, and still do
// once dropping a newer stash renumbers the older ones.
func TestStashActionsTargetTheirRow(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	for _, f := range []string{"a", "b", "c"} {
		writeFile(t, root, f+".txt", f+"\nstashed\n")
		gitRun(t, root, "stash", "push", "-q", "-m", "edit "+f)
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.historyClosed = true
	h.tt.Frame()
	stashes := func() []string {
		var m []string
		for _, s := range h.a.git.stashes {
			m = append(m, s.Message)
		}
		return m
	}
	changed := func() []string {
		var m []string
		for _, c := range h.a.git.changes {
			m = append(m, c.Path)
		}
		return m
	}
	if got := strings.Join(stashes(), ","); got != "edit c,edit b,edit a" {
		t.Fatalf("stashes %s", got)
	}

	if err := h.tt.RightClick("Stash: edit b"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Apply Stashed Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.changes) > 0 })
	if got := strings.Join(changed(), ","); got != "b.txt" {
		t.Fatalf("applying the middle stash changed %s", got)
	}

	r, ok := h.tt.Find("Stash: edit c")
	if !ok {
		t.Fatal("no row for edit c")
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Frame()
	if err := h.tt.Click("Drop stash edit c"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.deleteConfirm == nil || !strings.Contains(h.a.deleteConfirm.message, "“edit c”") {
		t.Fatalf("drop asks %+v", h.a.deleteConfirm)
	}
	if err := h.tt.Click("Drop Stash"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.stashes) == 2 })
	if got := strings.Join(stashes(), ","); got != "edit b,edit a" {
		t.Fatalf("after dropping the newest: %s", got)
	}

	if err := h.tt.RightClick("Stash: edit a"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Apply and Drop Stash"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && len(h.a.git.stashes) == 1 })
	if got := strings.Join(stashes(), ","); got != "edit b" {
		t.Fatalf("apply and drop removed the wrong stash: %s", got)
	}
	if got := strings.Join(changed(), ","); got != "a.txt,b.txt" {
		t.Fatalf("after apply and drop: %s", got)
	}
}

// Stash Changes from the palette shows its field even while the navigator
// compares against a base branch.
func TestStashLeavesBranchComparison(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n"})
	writeFile(t, root, "a.txt", "a\nchanged\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.git.branchDiffMode = compareBaseMode
	h.a.beginStash()
	if h.a.git.branchDiffMode != uncommittedMode || !h.a.git.stashing || h.a.navigator != navGit {
		t.Fatalf("mode %v stashing %v", h.a.git.branchDiffMode, h.a.git.stashing)
	}
}
