package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDocumentHighlightsProtocol(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","textDocumentSync":1,"documentHighlightProvider":{}}`
	f.documentHighlights = func(params json.RawMessage) string {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		if err := json.Unmarshal(params, &p); err != nil || p.TextDocument.URI != "file:///w/a.go" || p.Position != (Position{2, 8}) {
			t.Errorf("params: %s (%v)", params, err)
		}
		return `[{"range":{"start":{"line":1,"character":3},"end":{"line":1,"character":8}},"kind":3},{"range":{"start":{"line":2,"character":5},"end":{"line":2,"character":10}}}]`
	}
	c := connect(t, f, r, w, nil)
	if !c.HasDocumentHighlights() {
		t.Fatal("document highlight provider was not detected")
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
	rawDocumentHighlight, ok := textDocument["documentHighlight"]
	if !ok {
		t.Fatal("documentHighlight client capability is missing")
	}
	var documentHighlightCapabilities map[string]bool
	if err := json.Unmarshal(rawDocumentHighlight, &documentHighlightCapabilities); err != nil {
		t.Fatal(err)
	}
	if enabled, ok := documentHighlightCapabilities["dynamicRegistration"]; !ok || enabled {
		t.Fatalf("documentHighlight dynamicRegistration = %v, want false", enabled)
	}
	got, err := c.DocumentHighlights(context.Background(), "file:///w/a.go", Position{2, 8})
	want := []DocumentHighlight{
		{Range: Range{Position{1, 3}, Position{1, 8}}, Kind: 3},
		{Range: Range{Position{2, 5}, Position{2, 10}}},
	}
	if err != nil || len(got) != len(want) {
		t.Fatalf("highlights: %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("highlight %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDocumentHighlightCapabilities(t *testing.T) {
	for _, provider := range []string{"false", "null", "true", "{}"} {
		t.Run(provider, func(t *testing.T) {
			f, r, w := newFake(t)
			f.caps = `{"definitionProvider":true,"documentHighlightProvider":` + provider + `}`
			c := connect(t, f, r, w, nil)
			want := provider == "true" || provider == "{}"
			if c.HasDocumentHighlights() != want {
				t.Fatalf("HasDocumentHighlights = %v, want %v", c.HasDocumentHighlights(), want)
			}
		})
	}
	f, r, w := newFake(t)
	c := connect(t, f, r, w, nil)
	if c.HasDocumentHighlights() {
		t.Fatal("absent document highlight provider was enabled")
	}
}

func TestDocumentHighlightsEmptyResponse(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = "{\"documentHighlightProvider\":true}"
	f.documentHighlights = func(json.RawMessage) string { return "[]" }
	c := connect(t, f, r, w, nil)
	got, err := c.DocumentHighlights(context.Background(), "file:///w/a.go", Position{1, 2})
	if err != nil || len(got) != 0 {
		t.Fatalf("empty highlights = %+v, %v", got, err)
	}
}
