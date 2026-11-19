package lsp

import (
	"context"
	"encoding/json"
	"testing"
)

func TestNavigationRequests(t *testing.T) {
	for _, method := range []string{"implementation", "typeDefinition"} {
		t.Run(method, func(t *testing.T) {
			responses := []string{
				"null", "[]",
				`{"uri":"file:///w/b.go","range":{"start":{"line":4,"character":5},"end":{"line":4,"character":6}}}`,
				`[{"uri":"file:///w/b.go","range":{"start":{"line":4,"character":5},"end":{"line":4,"character":6}}}]`,
				`[{"targetUri":"file:///w/b.go","targetRange":{"start":{"line":0,"character":0},"end":{"line":9,"character":0}},"targetSelectionRange":{"start":{"line":4,"character":5},"end":{"line":4,"character":6}}}]`,
				`"invalid"`,
			}
			f, r, w := newFake(t)
			f.caps = `{"definitionProvider":true,"implementationProvider":{},"typeDefinitionProvider":true}`
			call := 0
			f.navigation = func(got string, raw json.RawMessage) string {
				var p struct {
					TextDocument struct {
						URI string `json:"uri"`
					} `json:"textDocument"`
					Position Position `json:"position"`
				}
				if err := json.Unmarshal(raw, &p); err != nil || got != "textDocument/"+method || p.TextDocument.URI != "file:///w/a.go" || p.Position != (Position{2, 8}) {
					t.Errorf("request %s: %s (%v)", got, raw, err)
				}
				response := responses[call]
				call++
				return response
			}
			c := connect(t, f, r, w, nil)
			if !c.HasImplementation() || !c.HasTypeDefinition() {
				t.Fatal("missing capabilities")
			}
			var init struct {
				Capabilities struct {
					TextDocument map[string]struct {
						LinkSupport bool `json:"linkSupport"`
					} `json:"textDocument"`
				} `json:"capabilities"`
			}
			if err := json.Unmarshal(f.initializeParams, &init); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"implementation", "typeDefinition"} {
				if !init.Capabilities.TextDocument[name].LinkSupport {
					t.Errorf("%s linkSupport not advertised", name)
				}
			}
			request := c.Implementation
			if method == "typeDefinition" {
				request = c.TypeDefinition
			}
			for i := range responses {
				locs, err := request(context.Background(), "file:///w/a.go", Position{2, 8})
				if i == 5 {
					if err == nil {
						t.Fatal("malformed result accepted")
					}
				} else if i < 2 {
					if err != nil || len(locs) != 0 {
						t.Fatalf("empty: %v %v", locs, err)
					}
				} else if err != nil || len(locs) != 1 || locs[0].URI != "file:///w/b.go" || locs[0].Range.Start != (Position{4, 5}) {
					t.Fatalf("locations: %v %v", locs, err)
				}
			}
		})
	}
}

func TestNavigationCapabilities(t *testing.T) {
	for _, provider := range []string{"false", "null", "true", "{}"} {
		t.Run(provider, func(t *testing.T) {
			f, r, w := newFake(t)
			f.caps = `{"definitionProvider":true,"implementationProvider":` + provider + `,"typeDefinitionProvider":` + provider + `}`
			c := connect(t, f, r, w, nil)
			want := provider == "true" || provider == "{}"
			if c.HasImplementation() != want || c.HasTypeDefinition() != want {
				t.Fatalf("capabilities: %v %v", c.HasImplementation(), c.HasTypeDefinition())
			}
		})
	}
	f, r, w := newFake(t)
	c := connect(t, f, r, w, nil)
	if c.HasImplementation() || c.HasTypeDefinition() {
		t.Fatal("absent providers enabled")
	}
}
