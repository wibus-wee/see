package lsp

import (
	"context"
	"errors"
)

var ErrNoTypeItem = errors.New("lsp: no type hierarchy item")
var ErrTypeHierarchyUnsupported error = unsupported("textDocument/prepareTypeHierarchy")

func (c *Client) HasTypeHierarchy() bool { return c.typeHierarchy }

func (c *Client) PrepareTypeHierarchy(ctx context.Context, uri string, pos Position) ([]TypeHierarchyItem, error) {
	if !c.HasTypeHierarchy() {
		return nil, ErrTypeHierarchyUnsupported
	}
	var items []TypeHierarchyItem
	err := c.call(ctx, "textDocument/prepareTypeHierarchy", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}, &items)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNoTypeItem
	}
	return items, nil
}

func (c *Client) Supertypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	return c.typeHierarchyItems(ctx, "typeHierarchy/supertypes", item)
}

func (c *Client) Subtypes(ctx context.Context, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	return c.typeHierarchyItems(ctx, "typeHierarchy/subtypes", item)
}

func (c *Client) typeHierarchyItems(ctx context.Context, method string, item TypeHierarchyItem) ([]TypeHierarchyItem, error) {
	if !c.HasTypeHierarchy() {
		return nil, ErrTypeHierarchyUnsupported
	}
	var items []TypeHierarchyItem
	err := c.call(ctx, method, map[string]any{"item": item}, &items)
	return items, err
}
