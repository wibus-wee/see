package lsp

import (
	"context"
)

// DocumentSymbol is a declaration's name and selection position in server encoding.
type DocumentSymbol struct {
	Name     string
	Position Position
}

type documentSymbolResult struct {
	Name           string                 `json:"name"`
	SelectionRange *Range                 `json:"selectionRange"`
	Location       *Location              `json:"location"`
	Children       []documentSymbolResult `json:"children"`
}

// DocumentSymbols accepts both hierarchical DocumentSymbol and flat SymbolInformation results.
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	var result []documentSymbolResult
	if err := c.conn.Call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": uri}}, &result); err != nil {
		return nil, err
	}
	return flattenDocumentSymbols(result, uri), nil
}

func flattenDocumentSymbols(result []documentSymbolResult, uri string) []DocumentSymbol {
	var out []DocumentSymbol
	var visit func([]documentSymbolResult)
	visit = func(symbols []documentSymbolResult) {
		for _, s := range symbols {
			if s.Name != "" {
				if s.SelectionRange != nil {
					out = append(out, DocumentSymbol{Name: s.Name, Position: s.SelectionRange.Start})
				} else if s.Location != nil && s.Location.URI == uri {
					out = append(out, DocumentSymbol{Name: s.Name, Position: s.Location.Range.Start})
				}
			}
			visit(s.Children)
		}
	}
	visit(result)
	return out
}
