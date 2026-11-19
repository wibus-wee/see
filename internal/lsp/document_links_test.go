package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestDocumentLinksProtocol(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","textDocumentSync":1,"documentLinkProvider":{"resolveProvider":false}}`
	f.documentLinks = func(params json.RawMessage) string {
		var p struct {
			TextDocument struct {
				URI string `json:"uri"`
			} `json:"textDocument"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("params: %v", err)
		}
		if p.TextDocument.URI != "file:///w/a.go" {
			t.Errorf("params: %s", params)
		}
		return `[
			{"range":{"start":{"line":3,"character":9},"end":{"line":3,"character":18}},"target":"https://pkg.go.dev/fmt"},
			{"range":{"start":{"line":5,"character":4},"end":{"line":5,"character":12}},"target":"file:///w/b.go","tooltip":"embeds b.go"}
		]`
	}
	c := connect(t, f, r, w, nil)
	if !c.HasDocumentLinks() {
		t.Fatal("document link provider was not detected")
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
	if _, ok := textDocument["documentLink"]; !ok {
		t.Fatal("documentLink client capability is missing")
	}
	got, err := c.DocumentLinks(context.Background(), "file:///w/a.go")
	want := []DocumentLink{
		{Range: Range{Position{3, 9}, Position{3, 18}}, Target: "https://pkg.go.dev/fmt"},
		{Range: Range{Position{5, 4}, Position{5, 12}}, Target: "file:///w/b.go", Tooltip: "embeds b.go"},
	}
	if err != nil || len(got) != len(want) {
		t.Fatalf("links: %v, %v", got, err)
	}
	for i := range want {
		if got[i].Range != want[i].Range || got[i].Target != want[i].Target || got[i].Tooltip != want[i].Tooltip {
			t.Errorf("link %d: got %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDocumentLinkCapabilities(t *testing.T) {
	for _, provider := range []string{"false", "null", "true", "{}", `{"resolveProvider":true}`} {
		t.Run(provider, func(t *testing.T) {
			f, r, w := newFake(t)
			f.caps = `{"definitionProvider":true,"documentLinkProvider":` + provider + `}`
			c := connect(t, f, r, w, nil)
			want := provider == "true" || provider == "{}" || provider == `{"resolveProvider":true}`
			if c.HasDocumentLinks() != want {
				t.Fatalf("HasDocumentLinks = %v, want %v", c.HasDocumentLinks(), want)
			}
			resolve := provider == `{"resolveProvider":true}`
			if c.DocumentLinkResolveSupport() != resolve {
				t.Fatalf("DocumentLinkResolveSupport = %v, want %v", c.DocumentLinkResolveSupport(), resolve)
			}
		})
	}
	f, r, w := newFake(t)
	c := connect(t, f, r, w, nil)
	if c.HasDocumentLinks() {
		t.Fatal("absent document link provider was enabled")
	}
}

func TestDocumentLinkResolve(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","documentLinkProvider":{"resolveProvider":true}}`
	f.documentLinkResolve = func(params json.RawMessage) string {
		var p DocumentLink
		if err := json.Unmarshal(params, &p); err != nil {
			t.Errorf("resolve params: %v", err)
		}
		if p.Target != "" {
			t.Errorf("resolve was asked for a link that had a target: %s", params)
		}
		// The link's data goes back to the server, as the protocol wants;
		// the resolved target fills it.
		rb, err := json.Marshal(p.Range)
		if err != nil {
			t.Errorf("range: %v", err)
		}
		return `{"range":` + string(rb) + `,"target":"file:///w/lib/x.go"}`
	}
	c := connect(t, f, r, w, nil)
	link := &DocumentLink{
		Range: Range{Position{2, 0}, Position{2, 10}},
		Data:  json.RawMessage(`{"id":7}`),
	}
	resolved, err := c.DocumentLinkResolve(context.Background(), link)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Target != "file:///w/lib/x.go" {
		t.Fatalf("resolved target = %q", resolved.Target)
	}
}

func TestDocumentLinksNullResponse(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"documentLinkProvider":true}`
	f.documentLinks = func(json.RawMessage) string { return "null" }
	c := connect(t, f, r, w, nil)
	got, err := c.DocumentLinks(context.Background(), "file:///w/a.go")
	if err != nil || got != nil {
		t.Fatalf("null links = %+v, %v", got, err)
	}
}
