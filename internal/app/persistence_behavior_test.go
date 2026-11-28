package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
	h.host.newFile = func(string, string) (string, error) { return destination, nil }
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
				h.tt.Frame() // caches this version while the draft still has its old path
				edited = true
			}
			complete()
		case <-timeout.C:
			t.Fatal("untitled save did not complete")
		}
	}
	h.tt.Frame()
	h.settle(func() bool {
		r := readRecovery(h.a.root, destination)
		return r != nil && r.Base == "first snapshot" && r.Text == "later unsaved edit"
	})

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

func TestUntitledSaveOverwritesNativePanelDestination(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"existing.txt": "old contents"}))
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	draft.buf.Set("approved replacement")
	destination := filepath.Join(h.a.root, "existing.txt")
	h.host.newFile = func(string, string) (string, error) { return destination, nil }

	h.a.saveActive()
	h.settle(func() bool { return !draft.saving && !draft.untitled })
	data, err := os.ReadFile(destination)
	if err != nil || string(data) != "approved replacement" || draft.path != destination || draft.buf.Modified() {
		t.Fatalf("overwrite save: data=%q path=%q modified=%v err=%v", data, draft.path, draft.buf.Modified(), err)
	}
}

func TestAtomicWriteFailureLeavesNoPartialDestination(t *testing.T) {
	wantErr := errors.New("write stopped")
	for _, test := range []struct {
		name     string
		existing bool
	}{
		{name: "existing", existing: true},
		{name: "new"},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "destination.txt")
			if test.existing {
				if err := os.WriteFile(path, []byte("original"), 0644); err != nil {
					t.Fatal(err)
				}
			}
			err := writeFileAtomically(path, func(w io.Writer) error {
				_, _ = io.WriteString(w, "partial")
				return wantErr
			})
			if !errors.Is(err, wantErr) {
				t.Fatalf("write error = %v, want %v", err, wantErr)
			}
			data, readErr := os.ReadFile(path)
			if test.existing {
				if readErr != nil || string(data) != "original" {
					t.Fatalf("failed write changed destination: data=%q err=%v", data, readErr)
				}
			} else if !os.IsNotExist(readErr) {
				t.Fatalf("failed write left new destination: data=%q err=%v", data, readErr)
			}
		})
	}
}

func TestSaveFailureReportsErrorAndKeepsOldBytes(t *testing.T) {
	const base = "the last saved truth\n"
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": base}))
	d := h.open("a.txt")
	d.buf.Set("unsaved new text\n")
	// A read-only file makes the save fail before any write begins, as a
	// permission problem does; the failure must surface rather than
	// report success, and the file must not change.
	if err := os.Chmod(d.path, 0400); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(d.path, 0600) })
	h.tt.Key(ui.Cmd, ui.KeyS)
	h.tt.Frame()
	errmsg, isErr := h.feedbackError()
	if !isErr || !strings.Contains(errmsg, d.name) {
		t.Fatalf("the failed save reported no error: feedback=%v", h.feedbackTexts())
	}
	if !d.buf.Modified() {
		t.Fatal("the failed save marked the document saved")
	}
	disk, err := os.ReadFile(d.path)
	if err != nil || string(disk) != base {
		t.Fatalf("the failed save changed the file: %q err=%v", disk, err)
	}
	// The retry succeeds once the file is writable again, and the write
	// is the complete new content in one publish.
	if err := os.Chmod(d.path, 0600); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(ui.Cmd, ui.KeyS)
	h.tt.Frame()
	disk, err = os.ReadFile(d.path)
	if err != nil || string(disk) != "unsaved new text\n" || d.buf.Modified() {
		t.Fatalf("the retry did not save: %q modified=%v err=%v", disk, d.buf.Modified(), err)
	}
	entries, err := os.ReadDir(filepath.Dir(d.path))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("the writes left staging behind: %s", e.Name())
		}
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
