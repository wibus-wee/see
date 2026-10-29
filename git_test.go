package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

func TestDiffFollowsEditorPreferences(t *testing.T) {
	h := newHarness(t, "")
	h.a.git.diff = &editor.DiffState{}
	h.a.cfg.FontFamily = "Test Mono"
	h.a.cfg.FontSize = 21
	h.a.cfg.SoftWrap = true
	h.a.cfg.LineNumbers = int(editor.LineNumbersOff)
	h.a.applyEditorPrefsAll()
	d := h.a.git.diff
	if d.Font.Family != "Test Mono" || d.Font.Size != 21 || !d.Wrap || !d.NoLineNumbers {
		t.Fatalf("diff preferences: font=%+v wrap=%v no numbers=%v", d.Font, d.Wrap, d.NoLineNumbers)
	}
	h.a.cfg.FontSize = 12
	h.a.cfg.SoftWrap = false
	h.a.cfg.LineNumbers = int(editor.LineNumbersOn)
	h.a.applyEditorPrefsAll()
	if d.Font.Size != 12 || d.Wrap || d.NoLineNumbers {
		t.Fatalf("updated preferences: %+v", d.Font)
	}
}

func TestGitBaseAndChangePreview(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if _, err := git.Command(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	path := filepath.Join(root, "file.txt")
	base := "one\ntwo\n" + strings.Repeat("another line\n", 150)
	if err := os.WriteFile(path, []byte(base), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "--", "file.txt")
	run("commit", "-qm", "initial")
	if err := os.WriteFile(path, []byte("one\nchanged\n"+strings.Repeat("changed line\n", 10)+strings.Repeat("another line\n", 140)), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.a.refreshGit()
	h.settle(func() bool { return h.a.git.root != "" })
	h.a.term.open, h.a.term.maximized = true, true
	d := h.a.openFile(path, nil)
	if h.a.term.maximized {
		t.Fatal("opening a file left the terminal maximized")
	}
	h.settle(func() bool { return h.ed(d) != nil && h.ed(d).DiffBase != nil })
	st := h.ed(d)
	if got := st.DiffBase.String(); got != base {
		t.Fatalf("base: %q", got)
	}
	h.tt.Frame()
	r, ok := h.tt.Find("Show Git change at line 2")
	if !ok {
		t.Fatal("change marker is missing")
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	if h.tt.Cursor() != ui.CursorPointer {
		t.Fatalf("marker cursor: %v", h.tt.Cursor())
	}
	h.click("Show Git change at line 2")
	if h.a.git.peek == nil || len(h.a.git.peek.diff.Hunks()) != 1 {
		t.Fatal("changed line did not open its preview")
	}
	h.shot("peek-light.png")
	h.tt.SetDark(true)
	h.shot("peek-dark.png")
	h.tt.SetDark(false)
	h.tt.Frame()
	closeRect, ok := h.tt.Find("Close Change Preview")
	if !ok {
		t.Fatal("preview close button is missing")
	}
	peek := h.a.git.peek
	panelX, panelY := r.X+r.W/2, closeRect.Y+closeRect.H/2
	if panelY <= r.Y || panelY >= r.Y+r.H {
		t.Fatal("test requires the change marker behind the preview")
	}
	h.tt.Move(panelX, panelY)
	if h.tt.Cursor() == ui.CursorPointer {
		t.Fatal("change marker received hover through the preview")
	}
	h.tt.ClickAt(panelX, panelY)
	if h.a.git.peek != peek {
		t.Fatal("change marker received a click through the preview")
	}
	h.tt.ClickAt(closeRect.X-100, closeRect.Y+60)
	h.tt.Key(0, ui.KeyEscape)
	if h.a.git.peek != nil {
		t.Fatal("Escape did not close the preview")
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
	h.tt.Advance(time.Second)
	h.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 100)
	if st.ScrollY() <= 0 {
		t.Fatal("scrolling over a change marker did not scroll the editor")
	}
	h.a.term.maximized = true
	h.a.showGitDiff(git.Change{Path: "file.txt", Index: ' ', Work: 'M'}, false)
	if h.a.term.maximized {
		t.Fatal("opening a diff left the terminal maximized")
	}
}

func TestGitRowOpenAndDiscardWorkingChanges(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if out, err := git.Command(ctx, root, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")
	path := filepath.Join(root, "file.txt")
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write("base\n")
	run("add", "--", "file.txt")
	run("commit", "-qm", "initial")
	write("staged\n")
	run("add", "--", "file.txt")
	write("working\n")
	h := newHarness(t, root)
	h.a.navigator = navGit
	h.a.refreshGit()
	h.settle(func() bool { return !h.a.git.loading })
	if _, ok := h.tt.Find("Discard changes file.txt"); ok {
		t.Fatal("row actions visible before hover")
	}
	r, ok := h.tt.Find("Unstaged diff: file.txt")
	if !ok {
		t.Fatal("unstaged row missing")
	}
	h.tt.Move(r.X+10, r.Y+r.H/2)
	if _, ok := h.tt.Find("Discard changes file.txt"); !ok {
		t.Fatal("row actions missing on hover")
	}
	h.tt.Move(1000, 650)
	if _, ok := h.tt.Find("Discard changes file.txt"); ok {
		t.Fatal("row actions remained visible after hover")
	}
	h.tt.Move(r.X+10, r.Y+r.H/2)
	h.click("Open file file.txt")
	d := h.a.docs[path]
	if d == nil {
		t.Fatal("file did not open")
	}
	h.settle(func() bool { return !d.loading })
	h.click("Discard changes file.txt")
	if h.a.deleteConfirm == nil {
		t.Fatal("discard confirmation missing")
	}
	h.a.deleteConfirm.actions[1].fn()
	if b, _ := os.ReadFile(path); string(b) != "working\n" {
		t.Fatalf("cancel changed file: %q", b)
	}
	h.a.discardGitChange(h.a.git.changes[0])
	h.a.deleteConfirm.actions[0].fn()
	h.settle(func() bool { return !h.a.git.busy && !d.loading && !h.a.git.loading })
	if b, _ := os.ReadFile(path); string(b) != "staged\n" {
		t.Fatalf("working tree = %q", b)
	}
	if d.buf.String() != "staged\n" {
		t.Fatalf("editor = %q", d.buf.String())
	}
	_, _, changes, err := git.Read(ctx, root)
	if err != nil || len(changes) != 1 || changes[0].Index != 'M' || changes[0].Work != ' ' {
		t.Fatalf("staged change lost: %#v %v", changes, err)
	}
}

func TestDiscardUntrackedUsesTrash(t *testing.T) {
	root := t.TempDir()
	if _, err := git.Command(context.Background(), root, "init", "-q"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "new.txt")
	if err := os.WriteFile(path, []byte("new"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.a.git.root = root
	h.host.trash = func(p string) error { return os.Remove(p) }
	h.a.discardGitChange(git.Change{Path: "new.txt", Index: '?', Work: '?'})
	h.a.deleteConfirm.actions[0].fn()
	h.settle(func() bool { return !h.a.git.busy })
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
