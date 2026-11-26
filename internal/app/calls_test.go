package app

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/lsp"
	"see/internal/uri"
)

// callHierarchyHarness returns a harness over a.go, whose b.go defines
// helper, which a.go calls. fc answers call hierarchy as that shape.
func callHierarchyHarness(t *testing.T) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"a.go": "package a\n\nfunc call() {\n\thelper()\n}\n",
		"b.go": "package a\n\nfunc helper() int {\n\treturn 1\n}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	aURI := uri.FromPath(filepath.Join(root, "a.go"))
	bURI := uri.FromPath(filepath.Join(root, "b.go"))
	h.fc.callHier.items = []lsp.CallItem{{
		Name: "call", Kind: 12, URI: aURI,
		Range:          lsp.Range{Start: lsp.Position{Line: 2, Character: 0}, End: lsp.Position{Line: 4, Character: 1}},
		SelectionRange: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 9}},
	}}
	h.fc.callHier.incoming = func(item lsp.CallItem) ([]lsp.CallEdge, error) {
		switch item.Name {
		case "call":
			return []lsp.CallEdge{{
				Item: lsp.CallItem{
					Name: "main", Kind: 12, URI: aURI,
					Range:          lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 1, Character: 1}},
					SelectionRange: lsp.Range{Start: lsp.Position{Line: 0, Character: 5}, End: lsp.Position{Line: 0, Character: 9}},
				},
				FromRanges: []lsp.Range{{Start: lsp.Position{Line: 0, Character: 8}, End: lsp.Position{Line: 0, Character: 14}}},
			}}, nil
		default:
			return nil, nil
		}
	}
	h.fc.callHier.outgoing = func(item lsp.CallItem) ([]lsp.CallEdge, error) {
		switch item.Name {
		case "call":
			return []lsp.CallEdge{{
				Item: lsp.CallItem{
					Name: "helper", Kind: 12, URI: bURI,
					Range:          lsp.Range{Start: lsp.Position{Line: 2, Character: 0}, End: lsp.Position{Line: 4, Character: 1}},
					SelectionRange: lsp.Range{Start: lsp.Position{Line: 2, Character: 5}, End: lsp.Position{Line: 2, Character: 11}},
				},
				FromRanges: []lsp.Range{{Start: lsp.Position{Line: 3, Character: 1}, End: lsp.Position{Line: 3, Character: 7}}},
			}}, nil
		default:
			return nil, nil
		}
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	return h, d
}

// callRootReady waits for the hierarchy to show its anchor's first
// level, as the prepare and the root's expansion post back.
func callRootReady(t *testing.T, h *harness, name string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if len(h.a.calls.nodes) > 1 {
			for _, n := range h.a.calls.nodes {
				if n.item.Name == name && n.parent == 0 {
					return
				}
			}
		}
		select {
		case fn := <-h.q:
			fn()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("call hierarchy never showed %q: nodes=%v calls=%v", name, h.a.calls.nodes, h.fc.calls())
		}
	}
}

func TestCallHierarchyIncoming(t *testing.T) {
	h, d := callHierarchyHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	if !h.a.term.open || h.a.term.view != panelCalls {
		t.Fatalf("panel: open=%v view=%v", h.a.term.open, h.a.term.view)
	}
	callRootReady(t, h, "main")
	cs := &h.a.calls
	rootIdx, childIdx := -1, -1
	for i, n := range cs.nodes {
		if n.item.Name == statusRow {
			continue
		}
		if n.parent == -1 {
			rootIdx = i
		} else if n.item.Name == "main" {
			childIdx = i
		}
	}
	if rootIdx < 0 || childIdx < 0 {
		t.Fatalf("tree: %v", cs.nodes)
	}
	root, child := cs.nodes[rootIdx], cs.nodes[childIdx]
	if cs.anchor.name != "call" || root.item.Name != "call" || child.item.Name != "main" {
		t.Fatalf("rows: anchor=%q %+v %+v", cs.anchor.name, root.item, child.item)
	}
	if len(root.children) != 1 || root.children[0] != childIdx {
		t.Fatalf("root children: %v", root.children)
	}
	h.tt.Frame()
	if !h.tt.HasText("main") || !h.tt.HasText("a.go") {
		t.Fatalf("texts=%q", h.tt.Texts())
	}
}

func TestCallHierarchyPanesResize(t *testing.T) {
	h, d := callHierarchyHarness(t)
	h.a.activeEditor().GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	callRootReady(t, h, "main")

	cs := &h.a.calls
	tt := ui.NewTester(h.a.callsPanel, harnessWidth, harnessHeight)
	tt.Frame()
	divider, ok := tt.AccessByRole(ui.RoleSplitter, "Divider")
	if !ok {
		t.Fatal("call hierarchy panes have no splitter")
	}
	if cs.split != 40 {
		t.Fatalf("initial preview share = %v%%, want 40%%", cs.split)
	}
	x, y := divider.Bounds.X+0.5, divider.Bounds.Y+divider.Bounds.H/2
	tt.Press(x, y)
	tt.Move(x+80, y)
	tt.Release(x+80, y)
	if cs.split <= 40 {
		t.Fatalf("dragging the divider right did not widen the preview: %v%%", cs.split)
	}
}

func TestCallHierarchyRowClickPreviewsAndDoubleClickOpens(t *testing.T) {
	h, d := callHierarchyHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	callRootReady(t, h, "main")
	h.tt.Frame()

	if err := h.tt.Click("main"); err != nil {
		t.Fatalf("clicking a call row: %v", err)
	}
	h.tt.Frame()
	cs := &h.a.calls
	if cs.row != 1 || cs.preview == nil || cs.previewFor != 1 {
		t.Fatalf("clicking a call row did not select and preview it: row=%d preview=%v for=%d", cs.row, cs.preview != nil, cs.previewFor)
	}
	if len(cs.preview.LineBackgrounds) != 1 || cs.preview.LineBackgrounds[0].First != 0 {
		t.Fatalf("preview did not mark the call line: %+v", cs.preview.LineBackgrounds)
	}

	if err := h.tt.Click("main"); err != nil {
		t.Fatalf("double-clicking a call row: %v", err)
	}
	if err := h.tt.Click("main"); err != nil {
		t.Fatalf("double-clicking a call row: %v", err)
	}
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 0 || pos.Col != 8 {
		t.Fatalf("double-clicking a call row moved to %+v", pos)
	}
}

func TestCallHierarchyOutgoingAndLazyExpansion(t *testing.T) {
	h, d := callHierarchyHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	callRootReady(t, h, "main")
	h.a.switchCallDir(callsOutgoing)
	callRootReady(t, h, "helper")
	cs := &h.a.calls
	// helper's children are not asked until its row expands.
	k := cs.nodes[0].children[0]
	if cs.nodes[k].fetched || cs.nodes[k].pending {
		t.Fatal("helper's calls were fetched before expansion")
	}
	if n := countCalls(h.fc.calls(), "outgoingCalls helper"); n != 0 {
		t.Fatalf("outgoingCalls(helper) = %d", n)
	}
	h.a.expandCall(k)
	deadline := time.After(5 * time.Second)
	for !cs.nodes[k].fetched {
		select {
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatal("helper's calls never arrived")
		}
	}
	if !cs.nodes[k].empty {
		t.Fatalf("helper should end empty: %+v", cs.nodes[k])
	}
}

func TestCallHierarchyNoItem(t *testing.T) {
	h, d := callHierarchyHarness(t)
	h.fc.callHier.prepare = lsp.ErrNoCallItem
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(1))
	h.a.showCallHierarchy()
	deadline := time.After(5 * time.Second)
	for h.a.calls.nodes == nil || !h.a.calls.nodes[h.a.calls.roots[0]].empty {
		select {
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatalf("no-item message never landed: %+v", h.a.calls.nodes)
		}
	}
	h.tt.Frame()
	if !h.tt.HasText("No symbol at this position") && h.a.calls.nodes[h.a.calls.roots[0]].err == nil {
		t.Fatalf("texts=%q", h.tt.Texts())
	}
}

func TestCallHierarchyUnsupported(t *testing.T) {
	h, d := callHierarchyHarness(t)
	h.fc.noCalls = true
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	deadline := time.After(5 * time.Second)
	for h.a.calls.nodes == nil || !h.a.calls.nodes[h.a.calls.roots[0]].empty {
		select {
		case fn := <-h.q:
			fn()
		case <-deadline:
			t.Fatalf("unsupported message never landed: %+v", h.a.calls.nodes)
		}
	}
	if !h.a.calls.nodes[h.a.calls.roots[0]].unsupported {
		t.Fatal("root should be the unsupported row")
	}
}

func TestCallHierarchyStaleReplyDropped(t *testing.T) {
	h, d := callHierarchyHarness(t)
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "call()") + 1))
	h.a.showCallHierarchy()
	callRootReady(t, h, "main")
	cs := &h.a.calls
	before := len(cs.nodes)
	// A newer request claiming the seq drops the reply it lands after.
	cs.nodes[0].fetched = false
	h.a.expandCall(0)
	cs.seq++
	deadline := time.After(5 * time.Second)
	for countCalls(h.fc.calls(), "incomingCalls call") < 2 {
		select {
		case fn := <-h.q:
			fn()
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("incomingCalls never ran: %v", h.fc.calls())
		}
	}
	drainHarnessPosts(h)
	if len(cs.nodes) != before {
		t.Fatalf("stale reply added nodes: %d -> %d", before, len(cs.nodes))
	}
	if cs.nodes[0].fetched {
		t.Fatal("stale reply marked the node fetched")
	}
}
