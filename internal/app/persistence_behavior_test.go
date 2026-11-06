package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestRecoveredEditsRefuseToOverwriteExternalChanges(t *testing.T) {
	const base = "original disk text\n"
	const recovered = "unsaved recovered text\n"
	const external = "external process wrote this\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": base}))
	d := h.open("a.txt")
	d.buf.Set(recovered)
	h.a.cacheRecovery(d)
	h.settle(func() bool { r := readRecovery(h.a.root, d.path); return r != nil && r.Text == recovered })
	if err := os.WriteFile(d.path, []byte(external), 0600); err != nil {
		t.Fatal(err)
	}
	next := newHarness(t, h.a.root)
	restored := next.open("a.txt")
	if restored.buf.String() != recovered || !restored.buf.Modified() || !restored.conflict {
		t.Fatalf("recovery lost edit or conflict: text=%q modified=%v conflict=%v", restored.buf.String(), restored.buf.Modified(), restored.conflict)
	}
	next.tt.Key(ui.Cmd, ui.KeyS)
	next.tt.Frame()
	disk, err := os.ReadFile(d.path)
	if err != nil || string(disk) != external || restored.buf.String() != recovered || !restored.buf.Modified() {
		t.Fatalf("Save overwrote conflict: disk=%q buffer=%q modified=%v err=%v", disk, restored.buf.String(), restored.buf.Modified(), err)
	}
	if err := next.tt.Click("Overwrite"); err != nil {
		t.Fatalf("missing conflict action: %v", err)
	}
	next.tt.Frame()
	disk, err = os.ReadFile(d.path)
	if err != nil || string(disk) != recovered || restored.buf.Modified() || restored.conflict || readRecovery(next.a.root, d.path) != nil {
		t.Fatalf("overwrite failed: disk=%q dirty=%v conflict=%v err=%v", disk, restored.buf.Modified(), restored.conflict, err)
	}
}

func TestUntitledSaveKeepsEditsMadeBeforeCompletion(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"keep.txt": "keep"}))
	h.open("keep.txt")
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	old := draft.path
	draft.buf.Set("first snapshot")
	left := h.a.focus
	h.a.splitActive(sideRight)
	right := h.a.focus
	leftEditor, rightEditor := left.eds[old], right.eds[old]
	destination := filepath.Join(h.a.root, "saved.txt")
	h.host.newFile = func(string) (string, error) { return destination, nil }
	edited := false
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyS)
	timeout := time.NewTimer(10 * time.Second)
	defer timeout.Stop()
	for draft.untitled || draft.saving {
		select {
		case complete := <-h.q:
			disk, err := os.ReadFile(destination)
			if !edited && draft.untitled && err == nil && string(disk) == "first snapshot" {
				draft.buf.Set("later unsaved edit")
				edited = true
			}
			complete()
		case <-timeout.C:
			t.Fatal("untitled save did not complete")
		}
	}
	h.tt.Frame()

	disk, err := os.ReadFile(destination)
	if !edited || err != nil || string(disk) != "first snapshot" || draft.buf.String() != "later unsaved edit" || !draft.buf.Modified() {
		t.Fatalf("save swallowed in-flight edits: edited=%v disk=%q buffer=%q dirty=%v err=%v", edited, disk, draft.buf.String(), draft.buf.Modified(), err)
	}
	for _, g := range []*group{left, right} {
		if g.has(old) || !g.has(destination) || g.active != destination {
			t.Fatalf("save did not migrate split tabs: %+v", g.order)
		}
	}
	if h.a.docs[old] != nil || h.a.docs[destination] != draft || left.eds[destination] != leftEditor || right.eds[destination] != rightEditor {
		t.Fatal("save replaced shared document or editor states")
	}
	h.tt.Key(ui.Cmd, ui.KeyS)
	h.tt.Frame()
	disk, err = os.ReadFile(destination)
	if err != nil || string(disk) != "later unsaved edit" || draft.buf.Modified() {
		t.Fatalf("second Save did not persist newer edit: %q dirty=%v err=%v", disk, draft.buf.Modified(), err)
	}
}

func TestTextAndPreviewSessionRestoresSplitPositions(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first\nsecond\nthird", "readme.md": "# Title\n\nBody"}))
	d := h.open("a.txt")
	left := h.a.focus
	left.eds[d.path].GoTo(editor.Pos{Line: 1, Col: 3})
	h.a.splitActive(sideBottom)
	right := h.a.focus
	right.eds[d.path].GoTo(editor.Pos{Line: 2, Col: 2})
	markdown := h.open("readme.md")
	h.a.openMarkdownTab(right)
	h.a.sidebarOff = true
	h.a.captureSession()
	next := newHarness(t, "")
	next.a.cfg = h.a.cfg
	next.a.openWorkspace(h.a.root)
	next.settle(func() bool {
		for _, d := range next.a.docs {
			if d.loading {
				return false
			}
		}
		return len(next.a.groups()) == 2
	})
	groups := next.a.groups()
	if !next.a.layout.vertical || next.a.focus != groups[1] || !next.a.sidebarOff {
		t.Fatal("session lost split, focused group or sidebar preference")
	}
	if !slices.Equal(groups[0].order, left.order) || !slices.Equal(groups[1].order, right.order) || groups[1].active != markdownTab(markdown.path) {
		t.Fatalf("session lost source/preview tabs: %v / %v", groups[0].order, groups[1].order)
	}
	for i, want := range []editor.Pos{{Line: 1, Col: 3}, {Line: 2, Col: 2}} {
		st := groups[i].eds[d.path]
		if st == nil {
			t.Fatalf("group %d has no restored text editor", i)
		}
		if st.CursorPos() != want {
			t.Fatalf("group %d position: got %v want %v", i, st.CursorPos(), want)
		}
	}
	if groups[0].eds[d.path].Buffer != groups[1].eds[d.path].Buffer {
		t.Fatal("restored split did not share document buffer")
	}
}
