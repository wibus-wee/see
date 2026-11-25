package app

import (
	"context"
	"path/filepath"
	"see/internal/lsp"
	"see/internal/uri"
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
	if h.a.commandEnabled("hierarchy.types") {
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

func TestTypeHierarchyAnchorAfterNavigationAndReinvocation(t *testing.T) {
	h, d := callHierarchyHarness(t)
	h.a.showTypeHierarchyAt(h.a.focus)
	callRootReady(t, h, "main")
	root := h.a.calls.items[0]
	label := hierarchyTypeName(root) + " · a.go"
	h.a.switchCallDir(callsOutgoing)
	callRootReady(t, h, "helper")
	child := h.a.calls.nodes[1].item
	h.a.openCallRow(1)
	h.settle(func() bool { return h.a.activePath() == filepath.Join(h.a.root, "b.go") && h.a.activeEditor() != nil })
	if got := h.a.callAnchorLabel(); got != label || h.a.calls.items[0].URI != d.uri {
		t.Fatalf("navigation changed root: label=%q items=%+v", got, h.a.calls.items)
	}
	h.fc.callHier.items = []lsp.CallItem{child}
	h.a.showTypeHierarchyAt(h.a.focus)
	h.settle(func() bool { return !h.a.finding && len(h.a.calls.nodes) > 0 && h.a.calls.nodes[0].fetched })
	if got := h.a.callAnchorLabel(); got != hierarchyTypeName(child)+" · b.go" || h.a.calls.nodes[0].item.URI != child.URI {
		t.Fatalf("new invocation retained previous root: label=%q nodes=%+v", got, h.a.calls.nodes)
	}
}

func TestHierarchyShortLocations(t *testing.T) {
	h, _ := callHierarchyHarness(t)
	cs := &h.a.calls
	cs.nodes = []callNode{
		{item: lsp.CallItem{Name: "Client", URI: uri.FromPath(filepath.Join(h.a.root, "internal/lsp/client.go"))}},
		{item: lsp.CallItem{Name: "Client", URI: uri.FromPath(filepath.Join(h.a.root, "internal/langserver/server.go"))}},
	}
	if got := hierarchyTypeName(cs.nodes[0].item); got != "lsp.Client" {
		t.Fatalf("name = %q", got)
	}
	if got := h.a.callItemPlace(&cs.nodes[0]); got != "client.go:1" {
		t.Fatalf("location = %q", got)
	}
	cs.nodes = append(cs.nodes, callNode{item: lsp.CallItem{URI: uri.FromPath(filepath.Join(h.a.root, "internal/other/client.go"))}})
	if got := h.a.callItemPlace(&cs.nodes[0]); got != filepath.Join("lsp", "client.go")+":1" {
		t.Fatalf("ambiguous location = %q", got)
	}
}
