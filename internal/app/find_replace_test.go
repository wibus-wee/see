package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestReplaceInFileShortcutAppliesUndoableEdit(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "seed seed\n"}))
	d := h.open("a.txt")
	_, _, mods, key := fileReplaceShortcut()
	h.tt.Key(mods, key)
	h.tt.Type("seed")
	field, ok := h.tt.AccessByRole(ui.RoleTextField, "Replace")
	if !ok {
		t.Fatal("file replacement field did not open")
	}
	h.tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
	h.tt.Type("sprout")
	clickFindControl(t, h, "Replace All")
	if got := d.buf.String(); got != "sprout sprout\n" || !d.buf.Modified() {
		t.Fatalf("buffer became %q, modified=%v", got, d.buf.Modified())
	}
	if got := readFindTestFile(t, d.path); got != "seed seed\n" {
		t.Fatalf("file was saved automatically: %q", got)
	}
	h.a.activeEditor().Run(editor.Undo)
	if got := d.buf.String(); got != "seed seed\n" {
		t.Fatalf("undo: %q", got)
	}
}

func TestWorkspaceReplacePreviewSingleAndBulk(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	root := writeTree(t, map[string]string{"a.txt": "seed seed\n", "b.txt": "seed\n", "c.txt": "seed\n"})
	h := newHarness(t, root)
	clean := h.open("a.txt")
	dirty := h.open("c.txt")
	dirty.buf.Set("seed and unsaved work\n")
	h.tt.Frame()

	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyH)
	h.tt.Type("seed")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 4 })
	clickFindControl(t, h, "Replace with")
	h.tt.Type("sprout")
	if _, ok := h.tt.Find("sprout"); !ok {
		t.Fatal("replacement was not previewed")
	}
	clickFindControl(t, h, "Replace All…")
	if _, ok := h.tt.Find("Replace 4 matches in 3 files?"); !ok {
		t.Fatal("confirmation did not summarize the replacement")
	}
	if got := readFindTestFile(t, clean.path); got != "seed seed\n" {
		t.Fatalf("preview wrote to disk: %q", got)
	}
	clickFindControl(t, h, "Cancel")
	clickFindControl(t, h, "Replace this match")
	h.settle(func() bool {
		return !h.a.find.replacing && !h.a.find.running && !clean.loading && h.a.find.stats.Matches == 3
	})
	if got := clean.buf.String(); got != "sprout seed\n" {
		t.Fatalf("single replacement in open file: %q", got)
	}

	clickFindControl(t, h, "Replace All…")
	if _, ok := h.tt.Find("Replace 3 matches in 3 files?"); !ok {
		t.Fatal("second confirmation did not use refreshed results")
	}
	clickFindControl(t, h, "Replace All")
	h.settle(func() bool {
		return !h.a.find.replacing && !h.a.find.running && !clean.loading && h.a.find.stats.Matches == 1
	})
	if got := clean.buf.String(); got != "sprout sprout\n" {
		t.Fatalf("bulk replacement in open file: %q", got)
	}
	if got := readFindTestFile(t, filepath.Join(root, "b.txt")); got != "sprout\n" {
		t.Fatalf("bulk replacement in closed file: %q", got)
	}
	if got := readFindTestFile(t, dirty.path); got != "seed\n" || dirty.buf.String() != "seed and unsaved work\n" {
		t.Fatalf("dirty file changed: disk=%q buffer=%q", got, dirty.buf.String())
	}
}

func clickFindControl(t *testing.T, h *harness, label string) {
	t.Helper()
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func readFindTestFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestWorkspaceReplaceProtectsDirtyBufferAcrossSymlinkAlias(t *testing.T) {
	base := t.TempDir()
	work := filepath.Join(base, "work")
	project := filepath.Join(work, "project")
	if err := os.MkdirAll(project, 0o755); err != nil {
		t.Fatal(err)
	}
	realFile := filepath.Join(project, "a.txt")
	if err := os.WriteFile(realFile, []byte("seed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(base, "alias")
	if err := os.Symlink(project, alias); err != nil {
		t.Skipf("symlink: %v", err)
	}
	workspaceFile := filepath.Join(base, "team.code-workspace")
	if err := os.WriteFile(workspaceFile, []byte(`{"folders":[{"path":"alias"},{"path":"work"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.a.userOpen(realFile)
	h.settle(func() bool {
		for _, d := range h.a.docs {
			return !d.loading
		}
		return false
	})
	var d *doc
	for _, openDoc := range h.a.docs {
		d = openDoc
	}
	if d == nil {
		t.Fatal("document did not open")
	}
	d.buf.Set("seed and unsaved work\n")
	if !h.a.replaceProtected(realFile) {
		t.Fatalf("dirty buffer at %q did not protect real path %q", d.path, realFile)
	}
}
