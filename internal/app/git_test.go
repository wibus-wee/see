package app

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
	d := &editor.DiffState{}
	h.a.focus.diffs = map[string]*gitDiffTab{"test": {state: d}}
	h.a.cfg.FontFamily = "Test Mono"
	h.a.cfg.FontSize = 21
	h.a.cfg.SoftWrap = true
	h.a.cfg.LineNumbers = int(editor.LineNumbersOff)
	h.a.applyEditorPrefsAll()
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

func TestGitGutterKeepsTextPositionWhenBaseChanges(t *testing.T) {
	for _, tc := range []struct {
		name      string
		numbers   editor.LineNumbers
		noFolding bool
	}{{"default", editor.LineNumbersOn, false}, {"no numbers or folding", editor.LineNumbersOff, true}} {
		t.Run(tc.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"file.txt": "example\n"})
			h := newHarness(t, root)
			d := h.open("file.txt")
			st := h.ed(d)
			st.LineNumbers, st.NoFolding = tc.numbers, tc.noFolding
			h.a.focusEditor = st
			h.tt.Frame()
			before, ok := h.tt.TextCaret()
			if !ok {
				t.Fatal("editor caret missing")
			}
			imageBefore := h.tt.Image()
			st.DiffBase = editor.NewBuffer(d.buf.String())
			h.tt.Frame()
			after, ok := h.tt.TextCaret()
			if !ok || before.X != after.X {
				t.Fatalf("caret moved when base loaded: %v -> %v", before, after)
			}
			if tc.numbers != editor.LineNumbersOff {
				imageAfter := h.tt.Image()
				// Compare the gutter on the first line, where the number is painted.
				for y := int(before.Y); y < int(before.Y+before.H); y++ {
					for x := max(0, int(before.X)-100); x < int(before.X)-8; x++ {
						if imageBefore.At(x, y) != imageAfter.At(x, y) {
							t.Fatalf("gutter changed when base loaded at %d,%d", x, y)
						}
					}
				}
			}
			st.DiffBase = nil
			h.tt.Frame()
			last, ok := h.tt.TextCaret()
			if !ok || last.X != before.X {
				t.Fatalf("caret moved when base cleared: %v -> %v", before, last)
			}
		})
	}
}

func TestGitChangePreviewScrollsUnderPointer(t *testing.T) {
	root := writeTree(t, map[string]string{"file.txt": strings.Repeat("new line\n", 100)})
	h := newHarness(t, root)
	d := h.open("file.txt")
	st := h.ed(d)
	st.DiffBase = editor.NewBuffer(strings.Repeat("old line\n", 100))
	h.a.showGitPeek(h.a.focus, d, st, 0)
	h.tt.Frame()
	peek := h.a.git.peek
	if peek == nil {
		t.Fatal("change preview missing")
	}
	r, ok := h.tt.Find("Git change preview")
	if !ok || peek.scroll.MaxY <= 0 {
		t.Fatalf("preview cannot scroll: %+v", peek.scroll)
	}
	h.tt.Scroll(r.X+r.W/2, r.Y+r.H/2, 0, 100)
	h.tt.Frame()
	if peek.scroll.Y <= 0 {
		t.Fatal("wheel over change preview did not scroll it")
	}
}

func TestGitDiffShowsWholeFile(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if out, err := git.Command(ctx, root, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	path := filepath.Join(root, "file.txt")
	base := "first line\n" + strings.Repeat("unchanged\n", 20) + "old middle\n" + strings.Repeat("unchanged\n", 20) + "last line\n"
	staged := strings.Replace(base, "old middle", "staged middle", 1)
	working := strings.Replace(staged, "staged middle", "working middle", 1)
	write := func(s string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(s), 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(base)
	run("add", "--", "file.txt")
	run("commit", "-qm", "initial")
	write(staged)
	run("add", "--", "file.txt")
	write(working)
	h := newHarness(t, root)
	h.a.refreshGit()
	h.settle(func() bool { return h.a.git.root != "" })
	ch := git.Change{Path: "file.txt", Index: 'M', Work: 'M'}
	for _, tc := range []struct {
		staged   bool
		old, new string
	}{{true, base, staged}, {false, staged, working}} {
		key := diffTab(ch.Path, tc.staged)
		h.a.showGitDiff(ch, tc.staged)
		h.settle(func() bool { return h.a.focus.diffs[key] != nil })
		d := h.a.focus.diffs[key].state
		if d.FitContent {
			t.Fatal("diff must use its shared scroll")
		}
		if d.Old.String() != strings.TrimSuffix(tc.old, "\n") || d.New.String() != strings.TrimSuffix(tc.new, "\n") {
			t.Fatalf("staged=%v: diff omitted file content", tc.staged)
		}
		if pos := d.CursorPos(); pos.Line != 21 {
			t.Fatalf("staged=%v: initial cursor at %v, want first change", tc.staged, pos)
		}
	}
	g := h.a.focus
	key := g.active
	h.a.splitActive(sideRight)
	other := h.a.focus
	if other == g || other.diffs[key] == nil || other.diffs[key].state == g.diffs[key].state {
		t.Fatal("splitting a diff did not create an independent view")
	}
	h.a.moveTab(other, key, g, sideNone, len(g.order))
	if h.a.focus != g || !g.has(key) || len(h.a.groups()) != 1 {
		t.Fatal("moving a diff back did not merge the groups")
	}
	h.tt.Frame()
	h.a.closeTab(g, key)
	if g.has(key) || g.diffs[key] != nil {
		t.Fatal("closing the diff left its tab or view behind")
	}
	h.tt.Frame()
	if _, ok := h.tt.Find("Open files"); ok {
		t.Fatal("single diff unnecessarily shows a tab bar")
	}
	if _, ok := h.tt.Find("Close file.txt (Staged)"); !ok {
		t.Fatal("single diff is missing its close control")
	}
}

func TestDragGitDiffFromSidebar(t *testing.T) {
	root := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		if out, err := git.Command(context.Background(), root, args...); err != nil {
			t.Fatalf("git %v: %v (%s)", args, err, out)
		}
	}
	run("init", "-q")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("old\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "file.txt")
	run("commit", "-qm", "initial")
	if err := os.WriteFile(path, []byte("new\n"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.a.navigator = navGit
	h.a.refreshGit()
	h.settle(func() bool { return h.a.git.root != "" && !h.a.git.loading })
	h.open("file.txt")
	h.tt.Frame()
	r, ok := h.tt.Find("Unstaged diff: file.txt")
	if !ok {
		t.Fatal("Git row missing")
	}
	h.tt.Press(r.X+20, r.Y+r.H/2)
	h.tt.Move(r.X+50, r.Y+r.H/2)
	h.tt.Move(harnessWidth-30, harnessHeight/2)
	h.tt.Release(harnessWidth-30, harnessHeight/2)
	key := diffTab("file.txt", false)
	h.settle(func() bool { return h.a.focus.diffs[key] != nil })
	if len(h.a.groups()) != 2 || h.a.focus.active != key {
		t.Fatalf("drag did not open a diff beside the editor: groups=%d", len(h.a.groups()))
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

func TestGitBaseReadsAgainOnlyWhenHeadMoves(t *testing.T) {
	root := t.TempDir()
	ctx := context.Background()
	run := func(args ...string) {
		t.Helper()
		if _, err := git.Command(ctx, root, args...); err != nil {
			t.Fatal(err)
		}
	}
	run("init", "-q")
	run("config", "commit.gpgsign", "false")
	run("config", "user.name", "Test")
	run("config", "user.email", "test@example.com")
	path := filepath.Join(root, "file.txt")
	if err := os.WriteFile(path, []byte("first\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("add", "--", "file.txt")
	run("commit", "-qm", "first")
	h := newHarness(t, root)
	h.a.refreshGit()
	h.settle(func() bool { return h.a.git.head != "" })
	d := h.a.openFile(path, nil)
	h.settle(func() bool { return h.ed(d) != nil && h.ed(d).DiffBase != nil })
	seq := d.gitBaseSeq
	for range 3 {
		h.a.refreshGit()
		h.settle(func() bool { return !h.a.git.loading })
	}
	if d.gitBaseSeq != seq {
		t.Fatalf("refreshes with HEAD unchanged read the committed version again: seq %d -> %d", seq, d.gitBaseSeq)
	}
	if err := os.WriteFile(path, []byte("second\n"), 0600); err != nil {
		t.Fatal(err)
	}
	run("commit", "-qam", "second")
	h.a.refreshGit()
	h.settle(func() bool { return h.ed(d).DiffBase != nil && h.ed(d).DiffBase.String() == "second\n" })
}
