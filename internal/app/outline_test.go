package app

import (
	"context"
	"image/png"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

const outlineSource = "package a\n\ntype Server struct {\n\tName string\n}\n\nfunc (s *Server) Start() {}\n\nfunc Helper() {}\n"

func outlineSymbols() []lsp.DocumentSymbol {
	return []lsp.DocumentSymbol{
		{Name: "Server", Detail: "struct{...}", Kind: 23, Position: lsp.Position{Line: 2, Character: 5}, Range: lsp.Range{Start: lsp.Position{Line: 2}, End: lsp.Position{Line: 4, Character: 1}}},
		{Name: "Name", Detail: "string", Kind: 8, Depth: 1, Position: lsp.Position{Line: 3, Character: 1}, Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 1}, End: lsp.Position{Line: 3, Character: 12}}},
		{Name: "(*Server).Start", Detail: "func()", Kind: 6, Position: lsp.Position{Line: 6, Character: 17}, Range: lsp.Range{Start: lsp.Position{Line: 6}, End: lsp.Position{Line: 6, Character: 27}}},
		{Name: "Helper", Detail: "func()", Kind: 12, Position: lsp.Position{Line: 8, Character: 5}, Range: lsp.Range{Start: lsp.Position{Line: 8}, End: lsp.Position{Line: 8, Character: 16}}},
	}
}

func TestOutlineShowsFollowsAndNavigates(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": outlineSource, "notes.txt": "plain"}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return outlineSymbols(), nil }}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("a.go")
	st := h.a.activeEditor()
	at := st.Buffer.Offset(editor.Pos{Line: 3, Col: 3})
	st.Select(at, at)
	for _, cmd := range commands() {
		if cmd.name == "Show Outline" {
			cmd.run(h.a)
		}
	}
	h.tt.Frame()
	if h.a.navigator != navOutline || h.a.sidebarOff {
		t.Fatal("command did not show the outline")
	}
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == 4 })
	for _, text := range []string{"Outline", "a.go", "Server", "Name", "(*Server).Start", "Helper", "struct{...}", "string", "9"} {
		if !h.tt.HasText(text) {
			t.Fatalf("missing %q in %q", text, h.tt.Texts())
		}
	}
	if got := h.a.currentSymbol(); got != 1 {
		t.Fatalf("cursor in Name field marks %d", got)
	}
	if output := os.Getenv("SEE_OUTLINE_SCREENSHOT"); output != "" {
		f, err := os.Create(output)
		if err != nil {
			t.Fatal(err)
		}
		if err = png.Encode(f, h.tt.Image()); err != nil {
			t.Fatal(err)
		}
		if err = f.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if err := h.tt.Click("Helper — function, line 9"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if p := st.CursorPos(); p.Line != 8 || p.Col != 5 {
		t.Fatalf("jump: %+v", p)
	}
	if got := h.a.currentSymbol(); got != 3 {
		t.Fatalf("current after jump: %d", got)
	}
	h.a.goBack()
	if p := st.CursorPos(); p.Line != 3 {
		t.Fatalf("history: %+v", p)
	}
	h.tt.Frame()
	if err := h.tt.Click("Filter Symbols"); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("nam")
	h.tt.Frame()
	if rows := h.a.outlineRows(); len(rows) != 1 || rows[0].parent != "Server" || h.tt.HasText("Helper") {
		t.Fatalf("filter: %+v %q", rows, h.tt.Texts())
	}
	h.tt.Type("zzz")
	h.tt.Frame()
	if !h.tt.HasText("No matching symbols") {
		t.Fatalf("no matches: %q", h.tt.Texts())
	}
	h.a.outline.query = ""
	h.open("notes.txt")
	h.tt.Frame()
	if len(h.a.outline.symbols) != 0 || !h.tt.HasText("No language server is configured for this file.") {
		t.Fatalf("plain file: %q", h.tt.Texts())
	}
}

func TestOutlineRefreshesAfterEditsAndDropsStaleResults(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\nfunc Old() {}\n"}))
	h.a.cfg.Trust(h.a.root)
	entered, release := make(chan struct{}, 1), make(chan struct{})
	var calls atomic.Int32
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(ctx context.Context, _ string) ([]lsp.DocumentSymbol, error) {
		if calls.Add(1) == 1 {
			entered <- struct{}{}
			<-release
			return []lsp.DocumentSymbol{{Name: "Old", Kind: 12}}, nil
		}
		return []lsp.DocumentSymbol{{Name: "New", Kind: 12, Position: lsp.Position{Line: 1, Character: 5}}}, nil
	}}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	d := h.open("a.go")
	h.a.navigator, h.a.sidebarOff = navOutline, false
	h.tt.Frame()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("no symbols request")
	}
	if !h.tt.HasText("Loading symbols…") {
		t.Fatalf("loading state: %q", h.tt.Texts())
	}
	d.buf.Set("package a\nfunc New() {}\n")
	h.tt.Frame()
	close(release)
	h.settle(func() bool { return !h.a.outline.loading })
	if len(h.a.outline.symbols) != 1 || h.a.outline.symbols[0].Name != "New" || h.tt.HasText("Old") {
		t.Fatalf("stale outline: %+v %q", h.a.outline.symbols, h.tt.Texts())
	}
}
