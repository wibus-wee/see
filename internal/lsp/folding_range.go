package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
)

// FoldingRange kinds the protocol names; a range may carry none.
const (
	FoldingComment = "comment"
	FoldingImports = "imports"
	FoldingRegion  = "region"
)

// FoldingRange is a span of lines a server reports foldable: StartLine
// to EndLine, both 0-based, with EndLine itself hidden, as the editor's
// folds are. Kind names the protocol's comment, imports or region; ""
// is anything else. CollapsedText is what the server would draw over
// the folded lines, given only when the client asks, which this one
// does not.
type FoldingRange struct {
	StartLine      int
	EndLine        int
	Kind           string
	StartCharacter int
	EndCharacter   int
	CollapsedText  string
}

// foldingRangeJSON is a FoldingRange on the wire: startLine and
// startCharacter, which the spec renames when the client advertises
// zeroBasedLine, are separate fields there — this client does not,
// so they always pair.
type foldingRangeJSON struct {
	StartLine      int    `json:"startLine"`
	EndLine        int    `json:"endLine"`
	Kind           string `json:"kind"`
	StartCharacter int    `json:"startCharacter"`
	EndCharacter   int    `json:"endCharacter"`
	CollapsedText  string `json:"collapsedText"`
}

// FoldingRanges asks for the document's folding ranges.
func (c *Client) FoldingRanges(ctx context.Context, uri string) ([]FoldingRange, error) {
	var raw json.RawMessage
	err := c.call(ctx, "textDocument/foldingRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseFoldingRanges(raw)
}

// ParseFoldingRanges decodes a textDocument/foldingRange result: null
// or a list. Ranges that end where they start, or start off the text,
// are kept as they came; callers line-clamp against their snapshot.
func ParseFoldingRanges(raw json.RawMessage) ([]FoldingRange, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, nil
	}
	var items []foldingRangeJSON
	if err := json.Unmarshal(raw, &items); err != nil {
		return nil, fmt.Errorf("lsp: bad folding ranges: %w", err)
	}
	out := make([]FoldingRange, 0, len(items))
	for _, it := range items {
		out = append(out, FoldingRange{
			StartLine: it.StartLine, EndLine: it.EndLine, Kind: it.Kind,
			StartCharacter: it.StartCharacter, EndCharacter: it.EndCharacter,
			CollapsedText: it.CollapsedText,
		})
	}
	return out, nil
}
