package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDocumentSymbolsProtocol(t *testing.T) {
	for _, tc := range []struct {
		name, result string
		want         []DocumentSymbol
	}{
		{"nested", `[{"name":"Type","range":{"start":{"line":0,"character":0},"end":{"line":9,"character":0}},"selectionRange":{"start":{"line":1,"character":5},"end":{"line":1,"character":9}},"children":[{"name":"Method","selectionRange":{"start":{"line":3,"character":2},"end":{"line":3,"character":8}}}]}]`, []DocumentSymbol{{"Type", Position{1, 5}}, {"Method", Position{3, 2}}}},
		{"flat", `[{"name":"Local","location":{"uri":"file:///w/a.go","range":{"start":{"line":2,"character":3},"end":{"line":2,"character":7}}}},{"name":"Other","location":{"uri":"file:///w/other.go","range":{"start":{"line":0,"character":0},"end":{"line":0,"character":1}}}}]`, []DocumentSymbol{{"Local", Position{2, 3}}}},
		{"empty", "null", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, r, w := newFake(t)
			f.documentSymbols = func(params json.RawMessage) string {
				var p struct {
					TextDocument struct {
						URI string `json:"uri"`
					} `json:"textDocument"`
				}
				if err := json.Unmarshal(params, &p); err != nil || p.TextDocument.URI != "file:///w/a.go" {
					t.Errorf("params: %s", params)
				}
				return tc.result
			}
			c := connect(t, f, r, w, nil)
			got, err := c.DocumentSymbols(context.Background(), "file:///w/a.go")
			if err != nil || len(got) != len(tc.want) {
				t.Fatalf("got %v, %v", got, err)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("got %v want %v", got, tc.want)
				}
			}
		})
	}
}
