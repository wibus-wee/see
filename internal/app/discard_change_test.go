package app

import (
	"math"
	"os"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestDiscardBufferChange(t *testing.T) {
	for _, tt := range []struct {
		name, base, current, want string
		line                      int
		ok                        bool
	}{
		{"added start", "b\nc\n", "a\nb\nc\n", "b\nc\n", 0, true},
		{"added middle", "a\nc\n", "a\nb\nc\n", "a\nc\n", 1, true},
		{"added end", "a\n", "a\nb\n", "a\n", 1, true},
		{"added unterminated", "a", "a\nb", "a", 1, true},
		{"deleted start", "a\nb\nc\n", "b\nc\n", "a\nb\nc\n", 0, true},
		{"deleted middle", "a\nb\nc\n", "a\nc\n", "a\nb\nc\n", 1, true},
		{"deleted end", "a\nb\n", "a\n", "a\nb\n", 1, true},
		{"deleted unterminated", "a\nb", "a", "a\nb", 0, true},
		{"modified", "a\nb\nc\n", "a\nB\nc\n", "a\nb\nc\n", 1, true},
		{"modified start", "a\nb\n", "A\nb\n", "a\nb\n", 0, true},
		{"modified end", "a\nb", "a\nB", "a\nb", 1, true},
		{"multiple first", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\nE\n", "a\nb\nc\nd\nE\n", 0, true},
		{"multiple last", "a\nb\nc\nd\ne\n", "A\nb\nc\nd\nE\n", "A\nb\nc\nd\ne\n", 4, true},
		{"empty base", "", "new\n", "", 0, true},
		{"empty current", "old\n", "", "old\n", 0, true},
		{"both empty", "", "", "", 0, false},
		{"unchanged line", "a\nb\n", "A\nb\n", "A\nb\n", 1, false},
		{"invalid line", "a\n", "A\n", "A\n", -1, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			buf := editor.NewBuffer(tt.current)
			diff := &editor.DiffState{Old: editor.NewBuffer(tt.base), New: buf}
			if got := discardBufferChange(diff, tt.line); got != tt.ok {
				t.Fatalf("discard = %v, want %v; hunks %+v", got, tt.ok, diff.Hunks())
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("discard = %q, want %q", got, tt.want)
			}
			if tt.ok && (!buf.Modified() || !buf.Undo() || buf.String() != tt.current) {
				t.Fatalf("discard was not an undoable buffer edit: %q", buf.String())
			}
		})
	}
	if discardBufferChange(nil, 0) {
		t.Fatal("discard without a base succeeded")
	}
}

func TestGitPeekHugsContentAndCapsLongHunks(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for _, lines := range []int{3, 7, 80} {
			base := strings.Repeat("old line\n", lines)
			current := strings.Repeat("new line\n", lines)
			h := newHarness(t, gitRepo(t, map[string]string{"a.txt": base}))
			h.a.cfg.DiffUnified = unified
			h.a.cfg.DiffLayout = ""
			h.tt = ui.NewTester(h.a.view, 1100, 700)
			h.readGit()
			d := h.open("a.txt")
			d.buf.Set(current)
			h.settle(func() bool { return d.gitChanges != nil })
			h.a.showGitPeek(h.a.focus, d, h.a.activeEditor(), 0)
			for range 4 {
				h.tt.Frame()
			}
			panel, ok := h.tt.AccessByRole(ui.RoleGroup, "a.txt — Git changes")
			if !ok {
				t.Fatal("Git preview panel missing")
			}
			contentHeight := float32(h.a.git.peek.diff.ViewportHeight())
			want := min(float32(300), 36+contentHeight+6)
			if math.Abs(float64(panel.Bounds.H-want)) > 2 {
				t.Fatalf("unified=%v lines=%d: panel height %g, content %g, want %g", unified, lines, panel.Bounds.H, contentHeight, want)
			}
			if lines == 80 && panel.Bounds.H != 300 {
				t.Fatalf("long hunk did not reach cap: %+v", panel.Bounds)
			}
		}
	}
}

func TestDiscardChangePreviewAndCommand(t *testing.T) {
	base, current := "a\nb\nc\nd\ne\n", "A\nb\nc\nd\nE\n"
	root := gitRepo(t, map[string]string{"a.txt": base})
	writeFile(t, root, "a.txt", current)
	h := newHarness(t, root)
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.gitChanges != nil && len(d.gitChanges.Hunks()) == 2 })
	st := h.a.activeEditor()
	h.tt.Frame()
	bar, ok := h.tt.Find("Show Git change at line 1")
	if !ok {
		t.Fatal("change bar missing")
	}
	h.tt.ClickAt(bar.X+bar.W/2, bar.Y+bar.H/2)
	h.tt.Frame()
	if h.a.git.peek == nil {
		t.Fatal("change bar did not open preview")
	}
	if err := h.tt.Click(commandLabel("change.discard")); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.git.peek != nil || d.buf.String() != "a\nb\nc\nd\nE\n" || !d.buf.Modified() || len(d.gitChanges.Hunks()) != 1 {
		t.Fatalf("discard: preview=%v, buffer=%q, modified=%v", h.a.git.peek != nil, d.buf.String(), d.buf.Modified())
	}
	if disk, err := os.ReadFile(d.path); err != nil || string(disk) != current {
		t.Fatalf("discard changed disk: %q, %v", disk, err)
	}
	st.Run(editor.Undo)
	h.tt.Frame()
	if d.buf.String() != current || len(d.gitChanges.Hunks()) != 2 {
		t.Fatalf("undo: %q", d.buf.String())
	}
	st.GoTo(editor.Pos{Line: 2})
	if h.a.commandEnabled("change.discard") {
		t.Fatal("command enabled outside a hunk")
	}
	h.a.discardChange()
	if d.buf.String() != current {
		t.Fatal("disabled command changed buffer")
	}
	st.GoTo(editor.Pos{Line: 4})
	if !h.a.commandEnabled("change.discard") {
		t.Fatal("command disabled in a hunk")
	}
	h.a.discardChange()
	if d.buf.String() != "A\nb\nc\nd\ne\n" {
		t.Fatalf("command discarded wrong hunk: %q", d.buf.String())
	}
	st.ReadOnly = true
	st.GoTo(editor.Pos{})
	if h.a.commandEnabled("change.discard") {
		t.Fatal("command enabled in read-only editor")
	}
}
