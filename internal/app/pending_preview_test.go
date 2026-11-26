package app

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/workspace"
)

func TestPreviewReplacementKeepsOutgoingVisibleUntilReady(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || g.pendingPreview.doc != b || h.a.activeEditor() != g.eds[a.path] || a.buf.String() != "A-visible-marker\n" {
		t.Fatalf("pending preview displaced the outgoing document: active=%q pending=%+v editor=%p want=%p focus=%p buffer=%q", g.active, g.pendingPreview, h.a.activeEditor(), g.eds[a.path], h.a.focusEditor, a.buf.String())
	}
	if b.loadingVisible {
		t.Fatal("pending target showed its tab spinner before the delay")
	}
	h.a.scheduleLoadingIndicator(b, b.seq, 40*time.Millisecond)
	waitForLoadingIndicator(t, h, b)
	h.tt.Frame()
	h.tt.Frame()
	if !b.loadingVisible || g.active != a.path || h.a.activeEditor() != g.eds[a.path] || a.buf.String() != "A-visible-marker\n" || h.tt.HasText(loadingText(b)) {
		t.Fatalf("delayed pending feedback disrupted the outgoing editor: active=%q visible=%v texts=%q", g.active, b.loadingVisible, h.tt.Texts())
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	h.tt.Frame()
	if g.active != b.path || g.has(a.path) || h.a.activeEditor() != g.eds[b.path] || b.buf.String() != ready {
		t.Fatalf("ready preview did not replace the outgoing tab atomically: active=%q tabs=%v texts=%q", g.active, g.order, h.tt.Texts())
	}
}

func TestPreviewReplacementLatestTargetWins(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	c := pendingPreviewDoc(h, "c.txt")
	h.a.previewOpen(c.path)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || g.pendingPreview.doc != c || h.a.docs[b.path] != nil || a.buf.String() != "A-visible-marker\n" {
		t.Fatalf("newer preview did not supersede the pending target: active=%q pending=%+v tabs=%v docs=%v", g.active, g.pendingPreview, g.order, h.a.docs)
	}
	h.a.finishPendingPreview(b)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || g.pendingPreview.doc != c {
		t.Fatalf("stale completion changed the pending target: active=%q pending=%+v", g.active, g.pendingPreview)
	}

	const ready = "C-ready-marker"
	h.a.loaded(c, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != c.path || g.has(a.path) || h.a.docs[b.path] != nil || h.a.activeEditor() != g.eds[c.path] || c.buf.String() != ready {
		t.Fatalf("latest preview did not become active: active=%q tabs=%v texts=%q", g.active, g.order, h.tt.Texts())
	}
}

func TestPreviewReplacementKeepsCurrentActiveTabWhenPreviewDiffers(t *testing.T) {
	h, a := newPreviewHarness(t)
	c := h.open("c.txt")
	g := h.a.focus
	g.preview = a.path
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.tt.Frame()
	if g.active != c.path || g.pendingPreview == nil || h.a.activeEditor() != g.eds[c.path] || c.buf.String() != "C-file-content\n" {
		t.Fatalf("pending preview displaced the active tab: active=%q pending=%+v texts=%q", g.active, g.pendingPreview, h.tt.Texts())
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || !g.has(c.path) || g.has(a.path) || h.a.activeEditor() != g.eds[b.path] || b.buf.String() != ready {
		t.Fatalf("preview completion did not replace only the preview tab: active=%q tabs=%v texts=%q", g.active, g.order, h.tt.Texts())
	}
}

func TestPinnedOutgoingTabStaysUntilNewPreviewReady(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	g.preview = ""
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || g.pendingPreview.replaceable || g.pendingPreview.replaced != "" || !g.has(a.path) || h.a.activeEditor() != g.eds[a.path] {
		t.Fatalf("new preview displaced the pinned outgoing tab: active=%q pending=%+v tabs=%v", g.active, g.pendingPreview, g.order)
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || !g.has(a.path) || g.preview != b.path || h.a.activeEditor() != g.eds[b.path] || b.buf.String() != ready {
		t.Fatalf("ready preview did not preserve the pinned outgoing tab: active=%q preview=%q tabs=%v", g.active, g.preview, g.order)
	}
}

func TestSelectingPendingPreviewPreservesPreviousPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.a.userActivateIn(g, b.path)
	h.tt.Frame()
	if g.active != b.path || g.pendingPreview != nil || !g.has(a.path) || g.preview != b.path {
		t.Fatalf("selecting pending target lost the previous preview or retained stale ownership: active=%q preview=%q pending=%+v tabs=%v", g.active, g.preview, g.pendingPreview, g.order)
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || !g.has(a.path) || b.buf.String() != ready || g.pendingPreview != nil {
		t.Fatalf("selected target completion lost the previous preview: active=%q tabs=%v pending=%+v", g.active, g.order, g.pendingPreview)
	}
}

func TestClickingActiveTabCancelsPendingPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	h.a.sidebarOff = true
	h.tt.Frame()
	h.tt.Advance(300 * time.Millisecond)
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || !g.has(b.path) {
		t.Fatalf("preview was not pending before clicking active tab: active=%q pending=%+v tabs=%v", g.active, g.pendingPreview, g.order)
	}

	if err := h.tt.Click("a.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil || h.a.activeEditor() != g.eds[a.path] {
		t.Fatalf("clicking active tab did not cancel pending preview: active=%q pending=%+v tabs=%v", g.active, g.pendingPreview, g.order)
	}

	const ready = "late-B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil || h.a.activeEditor() != g.eds[a.path] || a.buf.String() != "A-visible-marker\n" {
		t.Fatalf("late completion activated the canceled preview: active=%q pending=%+v tabs=%v", g.active, g.pendingPreview, g.order)
	}
}

func TestPinningPendingPreviewPreservesPreviousPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.a.pinPreview(g, b.path)
	h.a.userActivateIn(g, b.path)
	h.tt.Frame()
	if g.active != b.path || g.pendingPreview != nil || g.preview != "" || !g.has(a.path) || !g.has(b.path) {
		t.Fatalf("pinning pending target lost the previous preview or retained stale ownership: active=%q preview=%q pending=%+v tabs=%v", g.active, g.preview, g.pendingPreview, g.order)
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || g.pendingPreview != nil || g.preview != "" || !g.has(a.path) || b.buf.String() != ready {
		t.Fatalf("pinned target completion lost the previous preview: active=%q preview=%q tabs=%v", g.active, g.preview, g.order)
	}
}

func TestReturningToOutgoingPreviewCancelsPendingTarget(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.a.previewOpen(a.path)
	h.tt.Frame()
	h.tt.Frame()
	if g.active != a.path || g.preview != a.path || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil || h.a.activeEditor() != g.eds[a.path] || a.buf.String() != "A-visible-marker\n" {
		t.Fatalf("returning to the outgoing preview did not cancel the target: active=%q preview=%q pending=%+v tabs=%v", g.active, g.preview, g.pendingPreview, g.order)
	}
}

func TestPreviewReadErrorRestoresOutgoingDocument(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.a.loaded(b, workspace.File{}, nil, false, errors.New("read failed"))
	h.tt.Frame()
	if g.active != a.path || g.preview != a.path || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil || h.a.activeEditor() != g.eds[a.path] || a.buf.String() != "A-visible-marker\n" {
		t.Fatalf("failed target displaced or survived beside the outgoing document: active=%q preview=%q pending=%+v tabs=%v", g.active, g.preview, g.pendingPreview, g.order)
	}
	if !h.a.noticeError {
		t.Fatal("preview read failure was not reported")
	}
}

func TestDirtyOutgoingPreviewSurvivesPendingReplacement(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	a.buf.Set("A-dirty-marker")

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || !g.has(a.path) || !a.buf.Modified() || a.buf.String() != "A-dirty-marker" || h.a.activeEditor() != g.eds[b.path] || b.buf.String() != ready {
		t.Fatalf("dirty outgoing document was lost during replacement: active=%q tabs=%v modified=%v text=%q", g.active, g.order, a.buf.Modified(), a.buf.String())
	}
}

func TestInitiallyDirtyPreviewStaysVisibleUntilReplacementReady(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	a.buf.Set("A-dirty-marker")
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview == nil || g.pendingPreview.replaceable || a.buf.String() != "A-dirty-marker" || h.a.activeEditor() != g.eds[a.path] {
		t.Fatalf("dirty outgoing preview was not retained while loading: active=%q pending=%+v texts=%q", g.active, g.pendingPreview, h.tt.Texts())
	}

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != b.path || !g.has(a.path) || a.buf.String() != "A-dirty-marker" || h.a.activeEditor() != g.eds[b.path] || b.buf.String() != ready {
		t.Fatalf("dirty outgoing preview was lost after replacement: active=%q tabs=%v text=%q", g.active, g.order, a.buf.String())
	}
}

func TestClosingDirtyOutgoingPreviewCancelsPendingTarget(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	a.buf.Set("A-dirty-marker")
	h.host.confirm = func(string) (int, error) { return 2, nil }
	h.a.closeTab(g, a.path)
	h.settle(func() bool { return h.a.confirm == nil })
	h.tt.Frame()
	if !g.has(a.path) || !a.buf.Modified() || a.buf.String() != "A-dirty-marker" || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil {
		t.Fatalf("closing a dirty outgoing tab lost it or retained the pending target: tabs=%v pending=%+v docs=%v", g.order, g.pendingPreview, h.a.docs)
	}
}

func TestSwitchingGroupsCancelsPendingPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	other := h.a.splitBeside(g, sideRight)
	other.addTab(a.path, 0)
	h.a.activateIn(other, a.path)
	if h.a.focus != other || g.active != a.path || g.pendingPreview != nil || g.has(b.path) || h.a.docs[b.path] != nil {
		t.Fatalf("group switch left a pending preview that could steal focus: focus=%p old=%p active=%q pending=%+v tabs=%v", h.a.focus, other, g.active, g.pendingPreview, g.order)
	}
}

func TestSwitchingAwayAndBackDoesNotResurrectPendingPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	other := h.a.splitBeside(g, sideRight)
	h.a.observeFocusedGroup(other)
	h.a.observeFocusedGroup(g)

	const ready = "B-ready-marker"
	h.a.loaded(b, workspace.File{Kind: workspace.Text, Text: ready}, editor.NewBuffer(ready), false, nil)
	h.tt.Frame()
	if g.active != a.path || g.pendingPreview != nil || g.has(b.path) || a.buf.String() != "A-visible-marker\n" || h.a.activeEditor() != g.eds[a.path] {
		t.Fatalf("stale preview was resurrected after switching back: active=%q pending=%+v tabs=%v texts=%q", g.active, g.pendingPreview, g.order, h.tt.Texts())
	}
}

func TestClosingWorkspaceCancelsPendingPreview(t *testing.T) {
	h, a := newPreviewHarness(t)
	g := h.a.focus
	b := pendingPreviewDoc(h, "b.txt")
	h.a.previewOpen(b.path)
	h.a.closeWorkspace()
	h.tt.Frame()
	if g.pendingPreview != nil || h.a.docs[b.path] != nil || len(h.a.docs) != 0 {
		t.Fatalf("workspace close retained the pending preview: pending=%+v docs=%v active=%q", g.pendingPreview, h.a.docs, a.path)
	}
}

func newPreviewHarness(t *testing.T) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"a.txt": "A-visible-marker\n",
		"b.txt": "B-file-content\n",
		"c.txt": "C-file-content\n",
	})
	h := newHarness(t, root)
	a := h.open("a.txt")
	h.a.focus.preview = a.path
	h.tt.Frame()
	return h, a
}

func pendingPreviewDoc(h *harness, name string) *doc {
	path := filepath.Join(h.a.root, name)
	d := &doc{path: path, uri: uriOf(path), name: name, loading: true, seq: 1}
	h.a.docs[path] = d
	return d
}
