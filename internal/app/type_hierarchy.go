package app

import (
	"context"
	"errors"

	"see/internal/langserver"
	"see/internal/lsp"
)

type typeHierarchyClient interface {
	HasTypeHierarchy() bool
	PrepareTypeHierarchy(context.Context, string, lsp.Position) ([]lsp.TypeHierarchyItem, error)
	Supertypes(context.Context, lsp.TypeHierarchyItem) ([]lsp.TypeHierarchyItem, error)
	Subtypes(context.Context, lsp.TypeHierarchyItem) ([]lsp.TypeHierarchyItem, error)
}

type typeHierarchyAdapter struct{ typeHierarchyClient }

func (c typeHierarchyAdapter) HasCallHierarchy() bool { return c.HasTypeHierarchy() }
func (c typeHierarchyAdapter) PrepareCallHierarchy(ctx context.Context, uri string, pos lsp.Position) ([]lsp.CallItem, error) {
	items, err := c.PrepareTypeHierarchy(ctx, uri, pos)
	if errors.Is(err, lsp.ErrNoTypeItem) {
		err = lsp.ErrNoCallItem
	}
	out := make([]lsp.CallItem, len(items))
	for i, item := range items {
		out[i] = lsp.CallItem(item)
	}
	return out, err
}
func (c typeHierarchyAdapter) IncomingCalls(ctx context.Context, item lsp.CallItem) ([]lsp.CallEdge, error) {
	items, err := c.Supertypes(ctx, lsp.TypeHierarchyItem(item))
	return typeEdges(items), err
}
func (c typeHierarchyAdapter) OutgoingCalls(ctx context.Context, item lsp.CallItem) ([]lsp.CallEdge, error) {
	items, err := c.Subtypes(ctx, lsp.TypeHierarchyItem(item))
	return typeEdges(items), err
}
func typeEdges(items []lsp.TypeHierarchyItem) []lsp.CallEdge {
	out := make([]lsp.CallEdge, len(items))
	for i, item := range items {
		out[i].Item = lsp.CallItem(item)
	}
	return out
}
func hierarchyClient(c langserver.Client, types bool) (callClient, bool) {
	if types {
		p, ok := c.(typeHierarchyClient)
		if !ok {
			return nil, false
		}
		return typeHierarchyAdapter{p}, true
	}
	p, ok := c.(callClient)
	return p, ok
}
func (a *App) showTypeHierarchyAt(g *group) {
	if g == nil || !a.navigationEnabled(g.active, "typeHierarchy") {
		return
	}
	a.showHierarchyAt(g, true)
}
func (a *App) hierarchyText(suffix string) string {
	prefix := "callHierarchy"
	if a.calls.types {
		prefix = "typeHierarchy"
	}
	return navigationText(prefix, suffix)
}
