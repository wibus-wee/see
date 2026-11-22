package app

import (
	"context"
	"see/internal/lsp"
	"testing"
	"time"
)

func (f *fakeClient) HasTypeHierarchy() bool { return !f.noCalls }
func (f *fakeClient) PrepareTypeHierarchy(ctx context.Context, uri string, pos lsp.Position) ([]lsp.TypeHierarchyItem, error) {
	items, err := f.PrepareCallHierarchy(ctx, uri, pos)
	out := make([]lsp.TypeHierarchyItem, len(items))
	for i, item := range items {
		out[i] = lsp.TypeHierarchyItem(item)
	}
	return out, err
}
func (f *fakeClient) Supertypes(ctx context.Context, item lsp.TypeHierarchyItem) ([]lsp.TypeHierarchyItem, error) {
	edges, err := f.IncomingCalls(ctx, lsp.CallItem(item))
	return testTypes(edges), err
}
func (f *fakeClient) Subtypes(ctx context.Context, item lsp.TypeHierarchyItem) ([]lsp.TypeHierarchyItem, error) {
	edges, err := f.OutgoingCalls(ctx, lsp.CallItem(item))
	return testTypes(edges), err
}
func testTypes(edges []lsp.CallEdge) []lsp.TypeHierarchyItem {
	out := make([]lsp.TypeHierarchyItem, len(edges))
	for i, edge := range edges {
		out[i] = lsp.TypeHierarchyItem(edge.Item)
	}
	return out
}

func TestTypeHierarchyDirections(t *testing.T) {
	h, _ := callHierarchyHarness(t)
	h.a.showTypeHierarchyAt(h.a.focus)
	callRootReady(t, h, "main")
	if !h.a.calls.types || h.a.hierarchyText("Incoming") != "Supertypes" {
		t.Fatal("not type hierarchy")
	}
	h.a.switchCallDir(callsOutgoing)
	callRootReady(t, h, "helper")
	h.tt.Frame()
	for k, n := range h.a.calls.nodes {
		if n.item.Name == "helper" {
			if len(h.a.callsChildren(k)) == 0 {
				t.Fatal("unfetched type has no expansion affordance")
			}
			h.a.calls.tree.Open.Add(k)
			h.tt.Frame()
			h.settle(func() bool { return h.a.calls.nodes[k].fetched })
		}
	}
	for _, n := range h.a.calls.nodes {
		if n.item.Name == "helper" && h.a.callSiteRange(&n) != n.item.SelectionRange {
			t.Fatal("navigating to call site instead of type")
		}
	}
	h.a.openCallRow(1)
}

func TestTypeHierarchyUnsupported(t *testing.T) {
	h, _ := callHierarchyHarness(t)
	h.fc.noCalls = true
	// Start the client so the command can inspect its capabilities.
	if _, err := h.a.lsp.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if h.a.commandEnabled("Show Type Hierarchy") {
		t.Fatal("unsupported command enabled")
	}
	h.a.showTypeHierarchyAt(h.a.focus)
	if h.a.calls.types {
		t.Fatal("unsupported panel opened")
	}
}

func TestTypeHierarchyEmptyAndError(t *testing.T) {
	for _, err := range []error{nil, lsp.ErrNoTypeItem} {
		h, _ := callHierarchyHarness(t)
		h.fc.callHier.items = nil
		h.fc.callHier.prepare = err
		h.a.showTypeHierarchyAt(h.a.focus)
		deadline := time.After(5 * time.Second)
		for h.a.finding {
			select {
			case fn := <-h.q:
				fn()
			case <-deadline:
				t.Fatal("hierarchy never answered")
			}
		}
		if len(h.a.calls.roots) == 0 {
			t.Fatal("missing empty/error row")
		}
	}
}
