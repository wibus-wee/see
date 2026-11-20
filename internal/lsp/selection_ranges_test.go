package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestSelectionRangesProtocol(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","textDocumentSync":1,"selectionRangeProvider":true}`
	f.selectionRanges = func(params json.RawMessage) string {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Positions []Position `json:"positions"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("params: %v", err)
		}
		if p.TextDocument.URI != "file:///w/a.go" {
			t.Errorf("params: %s", params)
		}
		if len(p.Positions) != 2 || p.Positions[0] != (Position{Line: 3, Character: 9}) || p.Positions[1] != (Position{Line: 6, Character: 2}) {
			t.Errorf("positions: %+v", p.Positions)
		}
		return `[
			{"range":{"start":{"line":3,"character":8},"end":{"line":3,"character":12}},
			 "parent":{"range":{"start":{"line":3,"character":4},"end":{"line":3,"character":24}},
			 "parent":{"range":{"start":{"line":2,"character":1},"end":{"line":4,"character":1}}}}},
			{"range":{"start":{"line":6,"character":0},"end":{"line":6,"character":40}}}
		]`
	}
	c := connect(t, f, r, w, nil)
	if !c.HasSelectionRanges() {
		t.Fatal("selection range provider was not detected")
	}
	var params map[string]json.RawMessage
	if err := json.Unmarshal(f.initializeParams, &params); err != nil {
		t.Fatal(err)
	}
	var capabilities map[string]json.RawMessage
	if err := json.Unmarshal(params["capabilities"], &capabilities); err != nil {
		t.Fatal(err)
	}
	var textDocument map[string]json.RawMessage
	if err := json.Unmarshal(capabilities["textDocument"], &textDocument); err != nil {
		t.Fatal(err)
	}
	if _, ok := textDocument["selectionRange"]; !ok {
		t.Fatal("selectionRange client capability is missing")
	}
	got, err := c.SelectionRanges(context.Background(), "file:///w/a.go", []Position{{Line: 3, Character: 9}, {Line: 6, Character: 2}})
	if err != nil {
		t.Fatalf("ranges: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("ranges: %+v", got)
	}
	chain := RangesOf(got[0])
	want := []Range{
		{Position{3, 8}, Position{3, 12}},
		{Position{3, 4}, Position{3, 24}},
		{Position{2, 1}, Position{4, 1}},
	}
	if len(chain) != len(want) {
		t.Fatalf("chain: %+v", chain)
	}
	for i := range want {
		if chain[i] != want[i] {
			t.Errorf("chain %d: got %+v, want %+v", i, chain[i], want[i])
		}
	}
	if got[1].Parent != nil {
		t.Errorf("second chain has a parent: %+v", got[1].Parent)
	}
}

func TestSelectionRangesError(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"selectionRangeProvider":true}`
	f.selectionRanges = func(json.RawMessage) string { return "" } // asks for a JSON-RPC error
	c := connect(t, f, r, w, nil)
	got, err := c.SelectionRanges(context.Background(), "file:///w/a.go", []Position{{Line: 0, Character: 0}})
	if err == nil {
		t.Fatalf("ranges: %+v, want an error", got)
	}
}

func TestSelectionRangesNull(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"selectionRangeProvider":true}`
	// selectionRanges unset: the fake answers null.
	c := connect(t, f, r, w, nil)
	got, err := c.SelectionRanges(context.Background(), "file:///w/a.go", []Position{{Line: 0, Character: 0}})
	if err != nil || got != nil {
		t.Fatalf("ranges: %+v, %v", got, err)
	}
}
