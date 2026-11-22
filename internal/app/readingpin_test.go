package app

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

func pinSymbol(name string, kind, depth, start, end int) lsp.DocumentSymbol {
	return lsp.DocumentSymbol{
		Name: name, Kind: kind, Depth: depth,
		Position: lsp.Position{Line: start, Character: 5},
		Range:    lsp.Range{Start: lsp.Position{Line: start}, End: lsp.Position{Line: end}},
	}
}

func TestStickyScopes(t *testing.T) {
	// Intentionally unordered and flat, as a server's SymbolInformation can be.
	symbols := []lsp.DocumentSymbol{
		pinSymbol("Inner", 12, 0, 60, 90),
		pinSymbol("Start", 6, 0, 30, 120),
		pinSymbol("Package", 4, 0, 0, 400),
		pinSymbol("Stop", 6, 0, 140, 160),
		pinSymbol("Server", 23, 0, 10, 300),
		pinSymbol("Property", 7, 0, 11, 22),
		pinSymbol("Handler", 12, 0, 50, 100),
		pinSymbol("Short", 12, 0, 320, 323),
	}
	cases := []struct {
		name string
		line int
		want []string
	}{
		{"at the type declaration", 10, nil},
		{"inside a non-scope property", 15, []string{"Server"}},
		{"method declaration still visible under the type", 29, []string{"Server"}},
		{"method declaration reached the type's pinned row", 30, []string{"Server", "Start"}},
		{"inside nested functions", 55, []string{"Server", "Start", "Handler"}},
		{"cap keeps the innermost scope", 70, []string{"Server", "Start", "Inner"}},
		{"method ends at the bottom of its slot", 118, []string{"Server"}},
		{"inside the next method", 150, []string{"Server", "Stop"}},
		{"between declarations", 310, nil},
		{"inside a short function", 321, []string{"Short"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var got []string
			for _, s := range stickyScopes(symbols, []int{tc.line, tc.line + 1, tc.line + 2, tc.line + 3}) {
				got = append(got, s.Name)
			}
			if !slices.Equal(got, tc.want) {
				t.Fatalf("headers %v, want %v", got, tc.want)
			}
		})
	}
}

const pinSource = `package a

// doc
type Server struct {
	Name string
}

func (s *Server) Start() {
	x := 1
	y := 2
	z := 3
	a := 4
	b := 5
	c := 6
	d := 7
	e := 8
	f := 9
	g := 10
	h := 11
	i := 12
	j := 13
	k := 14
	l := 15
	m := 16
	n := 17
	o := 18
	p := 19
	q := 20
	r := 21
	u := 22
	v := 23
	w := 24
}

func Helper() {
	tail := 1
	tail2 := 2
	tail3 := 3
	tail4 := 4
	tail5 := 5
	tail6 := 6
	tail7 := 7
	tail8 := 8
	tail9 := 9
	tail10 := 10
	_ = tail
}
`

func pinSymbols() []lsp.DocumentSymbol {
	return []lsp.DocumentSymbol{
		pinSymbol("Server", 23, 0, 3, 5),
		pinSymbol("Name", 8, 1, 4, 4),
		pinSymbol("(*Server).Start", 6, 0, 7, 32),
		pinSymbol("Helper", 12, 0, 38, 52),
	}
}

func pinHarness(t *testing.T) (*harness, *fileSymbolClient) {
	t.Helper()
	h := newHarness(t, writeTree(t, map[string]string{"a.go": pinSource, "notes.txt": "plain"}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return pinSymbols(), nil }}
	h.a.lsp = serverOf(h.a.root, client)
	t.Cleanup(func() { h.a.lsp.StopWait(time.Second) })
	h.open("a.go")
	h.settle(func() bool { return !h.a.focus.pin.loading && len(h.a.focus.pin.symbols) > 0 })
	return h, client
}

func (h *harness) scrollTo(st *editor.State, line int) {
	h.t.Helper()
	st.ScrollTo(line)
	h.tt.Frame()
	h.tt.Frame()
}

const startDeclaration = "func (s *Server) Start() {"

func TestStickyHeaderFollowsViewport(t *testing.T) {
	h, _ := pinHarness(t)
	st := h.a.activeEditor()
	// At the top of the file there is no answer and no row.
	h.tt.Frame()
	first, _ := st.VisibleLines()
	if first != 0 {
		t.Fatalf("top line %d", first)
	}
	if h.tt.HasText(startDeclaration) {
		t.Fatal("the pin shows at the top of the file")
	}
	height := st.ViewportHeight()
	// Scroll the declaration above the viewport: the row names it.
	h.scrollTo(st, 20)
	if !h.tt.HasText(startDeclaration) {
		t.Fatalf("the pin does not show the Start declaration: %q", h.tt.Texts())
	}
	if st.ViewportHeight() != height || st.CursorPos().Line != 0 {
		t.Fatal("showing the pin changed the viewport height or moved the caret")
	}
	// A scroll within the same declaration keeps one answer.
	h.scrollTo(st, 30)
	if !h.tt.HasText(startDeclaration) {
		t.Fatalf("the pin changed inside one declaration: %q", h.tt.Texts())
	}
	// Back at the top of the scope the row disappears.
	h.scrollTo(st, 7)
	if h.tt.HasText(startDeclaration) {
		t.Fatal("the pin stayed at the top of the scope")
	}
}

func TestStickyHeadersStackScrollAndNavigate(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"indexer.ts": `export const version = 1;

export class ProjectIndexer {
	name = "project";
	entries: string[] = [];

	rebuild(root: string) {
		this.entries.push(root + "/config");
		this.entries.push(root + "/docs");
		this.entries.push(root + "/examples");
		this.entries.push(root + "/index");
		this.entries.push(root + "/locale");
		this.entries.push(root + "/manifest");
		this.entries.push(root + "/modules");
		this.entries.push(root + "/packages");
		this.entries.push(root + "/scripts");
		this.entries.push(root + "/src");
		this.entries.push(root + "/tests");
		this.entries.push(root + "/tools");
		this.entries.push(root + "/vendor");
		this.entries.push(root + "/web");
		return this.entries;
	}
}
`}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) {
		return []lsp.DocumentSymbol{
			pinSymbol("ProjectIndexer", 5, 0, 2, 24),
			pinSymbol("rebuild", 6, 1, 6, 23),
		}, nil
	}}
	h.a.tsLSP = serverOf(h.a.root, client)
	defer h.a.tsLSP.StopWait(time.Second)
	h.open("indexer.ts")
	h.settle(func() bool { return !h.a.focus.pin.loading && len(h.a.focus.pin.symbols) == 2 })
	st := h.a.activeEditor()
	h.scrollTo(st, 15)
	pin := func(name string) (float32, float32) {
		t.Helper()
		row, ok := h.tt.Access(name)
		if !ok {
			t.Fatalf("no %q sticky header: %q", name, h.tt.Texts())
		}
		b := row.Bounds
		return b.X + b.W/2, b.Y + b.H/2
	}
	const outer, inner = "export class ProjectIndexer {", "rebuild(root: string) {"
	x, y := pin(outer)
	_, innerY := pin(inner)
	if innerY <= y {
		t.Fatal("the function is not stacked under the type")
	}
	before := st.ScrollY()
	h.tt.Wheel(x, y, 0, 20)
	h.tt.Frame()
	if st.ScrollY() <= before {
		t.Fatal("wheel scrolling stopped over the sticky header")
	}
	h.tt.ClickAt(x, y)
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 2 {
		t.Fatalf("click went to line %d, want 2", pos.Line)
	}
	// The navigation brought the declaration back in view, so the row
	// hides; scroll it away for the inner segment's click.
	h.scrollTo(st, 15)
	x, y = pin(inner)
	h.tt.ClickAt(x, y)
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 6 {
		t.Fatalf("click went to line %d, want 6", pos.Line)
	}
}

func TestStickyHeadersOffAndNoServer(t *testing.T) {
	h, _ := pinHarness(t)
	st := h.a.activeEditor()
	h.scrollTo(st, 20)
	if !h.tt.HasText(startDeclaration) {
		t.Fatal("the pin needs symbols to show")
	}
	// The setting turns the row off and back on.
	h.a.cfg.HideReadingPin = true
	h.tt.Frame()
	if h.tt.HasText(startDeclaration) {
		t.Fatal("the pin stayed with the setting off")
	}
	h.a.cfg.HideReadingPin = false
	h.tt.Frame()
	if !h.tt.HasText(startDeclaration) {
		t.Fatal("the pin did not come back with the setting on")
	}

	// Without a server there is no row.
	h2 := newHarness(t, writeTree(t, map[string]string{"a.go": pinSource}))
	h2.a.cfg.Trust(h2.a.root)
	h2.open("a.go")
	st2 := h2.a.activeEditor()
	h2.scrollTo(st2, 20)
	if h2.tt.HasText(startDeclaration) {
		t.Fatal("the pin showed without a server")
	}
}
