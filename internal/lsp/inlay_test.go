package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestInlayHintsProtocol(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","textDocumentSync":1,"inlayHintProvider":{"resolveProvider":false}}`
	f.inlayHints = func(params json.RawMessage) string {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
			Range Range `json:"range"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("params: %v", err)
		}
		if p.TextDocument.URI != "file:///w/a.go" || p.Range != (Range{Position{3, 0}, Position{8, 12}}) {
			t.Errorf("params: %s", params)
		}
		// Both label shapes, as the union allows: a string and parts.
		return `[
			{"position":{"line":3,"character":9},"label":": int","kind":1,"paddingLeft":false,"paddingRight":true},
			{"position":{"line":5,"character":4},"label":[{"value":"name"},{"value":":"}],"kind":2}
		]`
	}
	c := connect(t, f, r, w, nil)
	if !c.HasInlayHints() {
		t.Fatal("inlay hint provider was not detected")
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
	if _, ok := textDocument["inlayHint"]; !ok {
		t.Fatal("inlayHint client capability is missing")
	}
	got, err := c.InlayHints(context.Background(), "file:///w/a.go", Range{Position{3, 0}, Position{8, 12}})
	want := []InlayHint{
		{Position: Position{3, 9}, Label: ": int", Kind: 1, PaddingRight: true},
		{Position: Position{5, 4}, Label: "name:", Kind: 2},
	}
	if err != nil || len(got) != len(want) {
		t.Fatalf("hints: %v, %v", got, err)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("hint %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestInlayHintCapabilities(t *testing.T) {
	for _, provider := range []string{"false", "null", "true", "{}"} {
		t.Run(provider, func(t *testing.T) {
			f, r, w := newFake(t)
			f.caps = `{"definitionProvider":true,"inlayHintProvider":` + provider + `}`
			c := connect(t, f, r, w, nil)
			want := provider == "true" || provider == "{}"
			if c.HasInlayHints() != want {
				t.Fatalf("HasInlayHints = %v, want %v", c.HasInlayHints(), want)
			}
		})
	}
	f, r, w := newFake(t)
	c := connect(t, f, r, w, nil)
	if c.HasInlayHints() {
		t.Fatal("absent inlay hint provider was enabled")
	}
}

func TestInlayHintsNullResponse(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"inlayHintProvider":true}`
	f.inlayHints = func(json.RawMessage) string { return "null" }
	c := connect(t, f, r, w, nil)
	got, err := c.InlayHints(context.Background(), "file:///w/a.go", Range{Position{0, 0}, Position{10, 0}})
	if err != nil || got != nil {
		t.Fatalf("null hints = %+v, %v", got, err)
	}
}

// gopls reports no hint until workspace/configuration answers with its
// settings; a server See has nothing to say to still gets null per item.
func TestConfigurationPerServer(t *testing.T) {
	for _, tc := range []struct {
		serverID string
		want     string
	}{
		{"", "[null,null]"},
		{"clangd", "[null,null]"},
		{"gopls", `[{"hints":{"assignVariableTypes":true,"compositeLiteralTypes":true,"parameterNames":true,"rangeVariableTypes":true}},{"hints":{"assignVariableTypes":true,"compositeLiteralTypes":true,"parameterNames":true,"rangeVariableTypes":true}}]`},
	} {
		t.Run(tc.serverID, func(t *testing.T) {
			f, r, w := newFake(t)
			f.configWant = tc.want
			connectOpts(t, f, r, w, Options{RootPath: "/w", RootURI: "file:///w", Name: "w", ServerID: tc.serverID, Logf: t.Logf})
		})
	}
}
