package lsp

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestFormattingRequest(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","documentFormattingProvider":true,"documentRangeFormattingProvider":{"rangeLimit":100}}`
	c := connect(t, f, r, w, func(error) {})
	if !c.HasDocumentFormatting() || !c.HasDocumentRangeFormatting() {
		t.Fatal("capabilities not read")
	}
	f.navigation = func(method string, params json.RawMessage) string {
		var p struct {
			TextDocument struct{ URI string } `json:"textDocument"`
			Range        *struct {
				Start struct{ Line, Character int }
				End   struct{ Line, Character int }
			} `json:"range"`
			Options struct {
				TabSize      int  `json:"tabSize"`
				InsertSpaces bool `json:"insertSpaces"`
			} `json:"options"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("params: %s", params)
		}
		if p.TextDocument.URI != "file:///w/a.go" || p.Options.TabSize != 4 || !p.Options.InsertSpaces {
			t.Errorf("params: %s", params)
		}
		switch method {
		case "textDocument/formatting":
			if p.Range != nil {
				t.Errorf("formatting carried a range: %s", params)
			}
			return `[{"range":{"start":{"line":1,"character":0},"end":{"line":1,"character":4}},"newText":"\t"}]`
		case "textDocument/rangeFormatting":
			if p.Range == nil || p.Range.Start.Line != 2 || p.Range.End.Line != 4 {
				t.Errorf("rangeFormatting range: %s", params)
			}
			return `null`
		}
		return "null"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	edits, err := c.DocumentFormatting(ctx, "file:///w/a.go", FormattingOptions{TabSize: 4, InsertSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(edits) != 1 || edits[0].NewText != "\t" || edits[0].Range.Start.Line != 1 {
		t.Fatalf("edits: %+v", edits)
	}
	edits, err = c.DocumentRangeFormatting(ctx, "file:///w/a.go", Range{Start: Position{Line: 2, Character: 0}, End: Position{Line: 4, Character: 3}}, FormattingOptions{TabSize: 4, InsertSpaces: true})
	if err != nil {
		t.Fatal(err)
	}
	if edits != nil {
		t.Fatalf("edits: %+v", edits)
	}
}

func TestFormattingCapabilities(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","definitionProvider":true}`
	c := connect(t, f, r, w, func(error) {})
	if c.HasDocumentFormatting() || c.HasDocumentRangeFormatting() {
		t.Fatal("capabilities without the providers")
	}
}
