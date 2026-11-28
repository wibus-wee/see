package lsp

import (
	"context"
)

// DocumentSymbol is a declaration's name and selection position in server
// encoding, with its kind, its whole range and its nesting depth.
type DocumentSymbol struct {
	Name     string
	Detail   string
	Kind     int
	Depth    int
	Position Position
	Range    Range
}

type documentSymbolResult struct {
	Name           string                 `json:"name"`
	Detail         string                 `json:"detail"`
	Kind           int                    `json:"kind"`
	Range          *Range                 `json:"range"`
	SelectionRange *Range                 `json:"selectionRange"`
	Location       *Location              `json:"location"`
	Children       []documentSymbolResult `json:"children"`
}

// DocumentSymbols accepts both hierarchical DocumentSymbol and flat SymbolInformation results.
func (c *Client) DocumentSymbols(ctx context.Context, uri string) ([]DocumentSymbol, error) {
	var result []documentSymbolResult
	if err := c.call(ctx, "textDocument/documentSymbol", map[string]any{"textDocument": map[string]string{"uri": uri}}, &result); err != nil {
		return nil, err
	}
	return flattenDocumentSymbols(result, uri), nil
}

func flattenDocumentSymbols(result []documentSymbolResult, uri string) []DocumentSymbol {
	var out []DocumentSymbol
	var visit func([]documentSymbolResult, int)
	visit = func(symbols []documentSymbolResult, depth int) {
		for _, s := range symbols {
			next := depth
			if s.Name != "" {
				symbol := DocumentSymbol{Name: s.Name, Detail: s.Detail, Kind: s.Kind, Depth: depth}
				if s.SelectionRange != nil {
					symbol.Position, symbol.Range = s.SelectionRange.Start, *s.SelectionRange
					if s.Range != nil {
						symbol.Range = *s.Range
					}
				} else if s.Location != nil && s.Location.URI == uri {
					symbol.Position, symbol.Range = s.Location.Range.Start, s.Location.Range
				} else {
					symbol.Name = ""
				}
				if symbol.Name != "" {
					out = append(out, symbol)
					next = depth + 1
				}
			}
			visit(s.Children, next)
		}
	}
	visit(result, 0)
	return out
}
