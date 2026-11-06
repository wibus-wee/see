package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"see/internal/lsp"
)

type fileSymbolClient struct {
	*fakeClient
	symbols func(context.Context, string) ([]lsp.DocumentSymbol, error)
}

func (f *fileSymbolClient) DocumentSymbols(ctx context.Context, uri string) ([]lsp.DocumentSymbol, error) {
	return f.symbols(ctx, uri)
}

func TestFileSymbolsFilterAndNavigate(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n// 中🎉 target\nfunc First() {}\nfunc Second() {}\n"}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) {
		return []lsp.DocumentSymbol{{Name: "First", Position: lsp.Position{Line: 1, Character: 6}}, {Name: "Second", Position: lsp.Position{Line: 3, Character: 5}}}, nil
	}}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("a.go")
	st := h.a.activeEditor()
	st.Select(3, 3)
	h.tt.Frame()
	h.tt.Key(ui.Alt|ui.Cmd, ui.KeyO)
	h.settle(func() bool { return h.a.quick.fileLoaded })
	if len(h.a.quick.symbols) != 2 || !h.tt.HasText("First") || !h.tt.HasText("Second") {
		t.Fatalf("missing symbols: %v", h.tt.Texts())
	}
	h.tt.Type("fIR")
	h.tt.Frame()
	if len(h.a.quick.symbols) != 1 || h.a.quick.symbols[0].name != "First" {
		t.Fatal("file symbols were not filtered")
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if h.a.quick.open || st.CursorPos().Line != 1 || st.CursorPos().Col != 10 {
		t.Fatalf("UTF-16 jump: %+v", st.CursorPos())
	}
	h.a.goBack()
	if st.Cursor() != 3 {
		t.Fatalf("history: %d", st.Cursor())
	}
	h.a.openQuickPanel("@missing")
	h.tt.Frame()
	h.settle(func() bool { return h.a.quick.fileLoaded })
	if len(h.a.quick.results) != 0 || !h.tt.HasText("No Matches") {
		t.Fatalf("empty results: %v", h.tt.Texts())
	}
}

func TestFileSymbolsRejectStaleBufferResults(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\nfunc Old() {}\n"}))
	h.a.cfg.Trust(h.a.root)
	entered, release := make(chan context.Context, 1), make(chan struct{})
	var calls atomic.Int32
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(ctx context.Context, _ string) ([]lsp.DocumentSymbol, error) {
		if calls.Add(1) == 1 {
			entered <- ctx
			<-release
			return []lsp.DocumentSymbol{{Name: "Old"}}, nil
		}
		return []lsp.DocumentSymbol{{Name: "New", Position: lsp.Position{Line: 1, Character: 5}}}, nil
	}}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	d := h.open("a.go")
	h.a.openQuickPanel("@")
	h.tt.Frame()
	var firstContext context.Context
	select {
	case firstContext = <-entered:
	case <-time.After(time.Second):
		t.Fatal("no symbols request")
	}
	d.buf.Set("package a\nfunc New() {}\n")
	close(release)
	// The request cancels its context after posting its completion. Avoid a
	// frame here: ranking the edited buffer would cancel that context early.
	select {
	case <-firstContext.Done():
	case <-time.After(time.Second):
		t.Fatal("stale request did not post completion")
	}
	for len(h.q) > 0 {
		(<-h.q)()
	}
	if h.a.quick.fileLoaded || len(h.a.quick.symbols) != 0 {
		t.Fatal("stale request populated the edited file's symbols")
	}
	h.tt.Frame()
	h.settle(func() bool { return h.a.quick.fileLoaded })
	if len(h.a.quick.symbols) != 1 || h.a.quick.symbols[0].name != "New" {
		t.Fatalf("stale results replaced symbols: %+v", h.a.quick.symbols)
	}
	h.a.openQuickPanel("@")
	h.a.focus.active = ""
	h.tt.Frame()
	if len(h.a.quick.results) != 0 || !h.tt.HasText("Open a text file to search its symbols.") {
		t.Fatal("missing no-editor state")
	}
}
