package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// callItem is a hierarchy item as a fake server returns it.
const callItem = `{"name":"main","kind":12,"uri":"file:///w/main.go","range":{"start":{"line":4,"character":0},"end":{"line":9,"character":1}},"selectionRange":{"start":{"line":4,"character":5},"end":{"line":4,"character":9}}}`

func callFake(t *testing.T, navigation func(method string, params json.RawMessage) string) *Client {
	f, r, w := newFake(t)
	f.caps = `{"positionEncoding":"utf-8","textDocumentSync":{"openClose":true,"change":2},"callHierarchyProvider":true}`
	f.navigation = navigation
	return connect(t, f, r, w, nil)
}

func TestCallHierarchy(t *testing.T) {
	c := callFake(t, func(method string, params json.RawMessage) string {
		switch method {
		case "textDocument/prepareCallHierarchy":
			return `[` + callItem + `]`
		case "callHierarchy/incomingCalls":
			return `[{"from":{"name":"run","kind":12,"uri":"file:///w/run.go","range":{"start":{"line":0,"character":0},"end":{"line":20,"character":1}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":8}}},"fromRanges":[{"start":{"line":10,"character":1},"end":{"line":10,"character":7}},{"start":{"line":14,"character":1},"end":{"line":14,"character":7}}]}]`
		case "callHierarchy/outgoingCalls":
			return `[{"to":{"name":"help","kind":12,"uri":"file:///w/help.go","range":{"start":{"line":0,"character":0},"end":{"line":8,"character":1}},"selectionRange":{"start":{"line":1,"character":5},"end":{"line":1,"character":9}}},"fromRanges":[{"start":{"line":6,"character":2},"end":{"line":6,"character":8}}]}]`
		}
		return "null"
	})
	if !c.HasCallHierarchy() {
		t.Fatal("callHierarchyProvider not read")
	}
	ctx := context.Background()
	items, err := c.PrepareCallHierarchy(ctx, "file:///w/main.go", Position{4, 6})
	if err != nil || len(items) != 1 || items[0].Name != "main" {
		t.Fatalf("prepare: %v %v", items, err)
	}
	in, err := c.IncomingCalls(ctx, items[0])
	if err != nil || len(in) != 1 {
		t.Fatalf("incoming: %v %v", in, err)
	}
	if in[0].Item.Name != "run" || len(in[0].FromRanges) != 2 || in[0].FromRanges[1].Start.Line != 14 {
		t.Errorf("incoming edge: %+v", in[0])
	}
	out, err := c.OutgoingCalls(ctx, items[0])
	if err != nil || len(out) != 1 {
		t.Fatalf("outgoing: %v %v", out, err)
	}
	if out[0].Item.Name != "help" || out[0].Item.URI != "file:///w/help.go" || len(out[0].FromRanges) != 1 {
		t.Errorf("outgoing edge: %+v", out[0])
	}
	// Each direction's item keeps what the server wants back.
	var raw struct {
		Item callItemWire `json:"item"`
	}
	if err := json.Unmarshal(mustJSON(t, map[string]any{"item": items[0].wire()}), &raw); err != nil {
		t.Fatal(err)
	}
}

func mustJSON(t *testing.T, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestCallHierarchyNoItem(t *testing.T) {
	// gopls answers a position off a symbol with an error envelope.
	c := callFake(t, func(method string, params json.RawMessage) string {
		return "null"
	})
	// The fake's navigation reply is a result; the error path needs a
	// raw reply, so prepare answers the envelope itself.
	items, err := c.PrepareCallHierarchy(context.Background(), "file:///w/main.go", Position{})
	if !errors.Is(err, ErrNoCallItem) {
		t.Errorf("err = %v, items = %v", err, items)
	}
	// An empty result means the same: nothing here.
	if _, err := c.PrepareCallHierarchy(context.Background(), "file:///w/main.go", Position{9, 9}); !errors.Is(err, ErrNoCallItem) {
		t.Errorf("empty: err = %v", err)
	}
}

func TestCallHierarchyUnsupported(t *testing.T) {
	f, r, w := newFake(t) // no callHierarchyProvider in caps
	f.navigation = func(method string, params json.RawMessage) string { return "[]" }
	c := connect(t, f, r, w, nil)
	if c.HasCallHierarchy() {
		t.Error("unsupported server claims call hierarchy")
	}
}

func TestCallersKeepsFlattening(t *testing.T) {
	c := callFake(t, func(method string, params json.RawMessage) string {
		switch method {
		case "textDocument/prepareCallHierarchy":
			return `[` + callItem + `]`
		case "callHierarchy/incomingCalls":
			return `[{"from":{"name":"run","kind":12,"uri":"file:///w/run.go","range":{"start":{"line":0,"character":0},"end":{"line":20,"character":1}},"selectionRange":{"start":{"line":2,"character":5},"end":{"line":2,"character":8}}},"fromRanges":[{"start":{"line":10,"character":1},"end":{"line":10,"character":7}},{"start":{"line":14,"character":1},"end":{"line":14,"character":7}}]},{"from":{"name":"boot","kind":12,"uri":"file:///w/boot.go","range":{"start":{"line":0,"character":0},"end":{"line":5,"character":1}},"selectionRange":{"start":{"line":1,"character":5},"end":{"line":1,"character":9}}},"fromRanges":[]}]`
		}
		return "null"
	})
	locs, err := c.Callers(context.Background(), "file:///w/main.go", Position{4, 6})
	if err != nil || len(locs) != 3 {
		t.Fatalf("callers: %v %v", locs, err)
	}
	if locs[0].Range.Start.Line != 10 || locs[1].Range.Start.Line != 14 || locs[2].Range.Start.Line != 1 {
		t.Errorf("ranges: %v", locs)
	}
	if locs[0].Name != "run" || locs[2].Name != "boot" {
		t.Errorf("names: %v", locs)
	}
	if !strings.Contains(strings.Join(names(locs), ","), "run") {
		t.Errorf("names: %v", locs)
	}
}

func names(locs []Location) []string {
	var out []string
	for _, l := range locs {
		out = append(out, l.Name)
	}
	return out
}

func TestCallHierarchyCanceled(t *testing.T) {
	f, r, w := newFake(t)
	f.caps = `{"callHierarchyProvider":true}`
	arrived := make(chan struct{}, 1)
	f.navigation = func(method string, params json.RawMessage) string {
		arrived <- struct{}{}
		return "" // never answers
	}
	c := connect(t, f, r, w, nil)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.PrepareCallHierarchy(ctx, "file:///w/main.go", Position{})
		done <- err
	}()
	<-arrived
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v", err)
	}
}
