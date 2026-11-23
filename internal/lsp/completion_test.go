package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestCompletionProtocol(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"completionProvider":{}}`
	f.completion = func(p json.RawMessage) string {
		var got struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Position Position `json:"position"`
		}
		if err := json.Unmarshal(p, &got); err != nil {
			t.Error(err)
		}
		if got.TextDocument.URI != "file:///w/a.ts" || got.Position != (Position{Line: 1, Character: 5}) {
			t.Errorf("completion params: %s", p)
		}
		return `{"isIncomplete":true,"items":[{"label":"abi","kind":10,"detail":"boolean","filterText":"abi","insertText":"abi","documentation":{"kind":"markdown","value":"**ABI**"},"textEdit":{"range":{"start":{"line":1,"character":3},"end":{"line":1,"character":5}},"newText":"abi"}}]}`
	}
	c := connect(t, f, r, w, nil)
	if !c.HasCompletion() {
		t.Fatal("completion capability not read")
	}
	var init struct {
		Capabilities struct {
			TextDocument map[string]json.RawMessage `json:"textDocument"`
		} `json:"capabilities"`
	}
	if err := json.Unmarshal(f.initializeParams, &init); err != nil {
		t.Fatal(err)
	}
	var caps struct {
		CompletionItem struct {
			DocumentationFormat []string       `json:"documentationFormat"`
			Other               map[string]any `json:"-"`
		} `json:"completionItem"`
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(init.Capabilities.TextDocument["completion"], &raw); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(init.Capabilities.TextDocument["completion"], &caps); err != nil {
		t.Fatal(err)
	}
	if len(caps.CompletionItem.DocumentationFormat) != 2 ||
		caps.CompletionItem.DocumentationFormat[0] != "markdown" ||
		caps.CompletionItem.DocumentationFormat[1] != "plaintext" {
		t.Fatalf("completion documentation formats: %+v", caps.CompletionItem.DocumentationFormat)
	}
	var completionItem map[string]json.RawMessage
	if err := json.Unmarshal(raw["completionItem"], &completionItem); err != nil {
		t.Fatal(err)
	}
	for _, unsupported := range []string{"snippetSupport", "resolveSupport", "additionalTextEditsSupport"} {
		if _, ok := completionItem[unsupported]; ok {
			t.Errorf("advertised unsupported completion feature %q", unsupported)
		}
	}
	got, err := c.Completion(context.Background(), "file:///w/a.ts", Position{Line: 1, Character: 5})
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || !got.IsIncomplete || len(got.Items) != 1 {
		t.Fatalf("completion result = %+v", got)
	}
	item := got.Items[0]
	if item.Label != "abi" || item.Kind != 10 || item.Detail != "boolean" ||
		item.FilterText != "abi" || item.InsertText != "abi" || item.Documentation != "**ABI**" ||
		item.TextEdit == nil || item.TextEdit.Range == nil || item.TextEdit.NewText != "abi" ||
		item.TextEdit.Range.Start.Character != 3 || item.TextEdit.Range.End.Character != 5 {
		t.Fatalf("completion item = %+v", item)
	}

	f2, r2, w2 := newFake(t)
	c2 := connect(t, f2, r2, w2, nil)
	if c2.HasCompletion() {
		t.Error("server without completion provider reported support")
	}
}

func TestParseCompletionShapes(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
		want int
	}{
		{"list", `{"isIncomplete":true,"items":[{"label":"abi"}]}`, 1},
		{"array", `[{"label":"abi"},{"label":"abc"}]`, 2},
		{"null", "null", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseCompletion(json.RawMessage(tc.raw))
			if err != nil {
				t.Fatal(err)
			}
			if tc.want == 0 {
				if got != nil {
					t.Fatalf("null result = %+v", got)
				}
				return
			}
			if got == nil || len(got.Items) != tc.want {
				t.Fatalf("items = %+v", got)
			}
		})
	}
}
