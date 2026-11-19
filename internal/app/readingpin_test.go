package app

import (
	"context"
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

func TestPinScopes(t *testing.T) {
	symbols := []lsp.DocumentSymbol{
		pinSymbol("Pkg", 4, 0, 0, 400),         // 0
		pinSymbol("Server", 23, 1, 10, 300),    // 1
		pinSymbol("Name", 8, 2, 12, 12),        // 2, a field: too short
		pinSymbol("Start", 6, 2, 30, 120),      // 3
		pinSymbol("middleware", 12, 3, 32, 33), // 4, short: not a scope
		pinSymbol("handler", 12, 4, 50, 100),   // 5
		pinSymbol("inner", 12, 5, 60, 90),      // 6, one more level
		pinSymbol("Stop", 6, 2, 140, 160),      // 7
		pinSymbol("Helper", 12, 1, 320, 340),   // 8
	}
	indexes := func(path []pinScope) []int {
		out := make([]int, len(path))
		for i, s := range path {
			out[i] = s.index
		}
		return out
	}
	hops := func(path []pinScope) []int {
		out := make([]int, len(path))
		for i, s := range path {
			out[i] = s.hops
		}
		return out
	}
	cases := []struct {
		name string
		line int
		want []int // symbol indexes outer first, nil for no row
		hops []int
	}{
		{"between symbols at top level", 310, []int{0}, []int{0}},
		{"inside Server only", 15, []int{0, 1}, []int{0, 0}},
		{"inside handler names it", 55, []int{3, 5}, []int{3, 0}},
		{"inside inner keeps one level", 70, []int{5, 6}, []int{4, 0}},
		{"inside handler below inner", 95, []int{3, 5}, []int{3, 0}},
		{"inside Stop", 150, []int{1, 7}, []int{1, 0}},
		{"inside Helper", 330, []int{0, 8}, []int{0, 0}},
		{"a scope too short names its parent", 13, []int{0, 1}, []int{0, 0}},
		{"the last line of a scope stays inside its parent", 120, []int{0, 1}, []int{0, 0}},
		{"on the declaration line is inside it", 30, []int{1, 3}, []int{1, 0}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := pinScopes(nil, symbols, tc.line)
			if len(got) == 0 {
				if tc.want != nil {
					t.Fatalf("no path, want %v", tc.want)
				}
				return
			}
			if tc.want == nil {
				t.Fatalf("path %v, want none", indexes(got))
			}
			if len(indexes(got)) != len(tc.want) {
				t.Fatalf("path %v, want symbols %v", indexes(got), tc.want)
			}
			for i, idx := range indexes(got) {
				if idx != tc.want[i] {
					t.Fatalf("path %v, want symbols %v", indexes(got), tc.want)
				}
			}
			for i, h := range hops(got) {
				if h != tc.hops[i] {
					t.Fatalf("hops %v, want %v", hops(got), tc.hops)
				}
			}
		})
	}
	// Sibling scopes at one depth: the longest containing one is the
	// answer, and a parent keeps a depth walk from crossing scopes.
	deep := []lsp.DocumentSymbol{
		pinSymbol("A", 6, 0, 0, 50),
		pinSymbol("B", 6, 1, 10, 20),
		pinSymbol("C", 6, 1, 30, 48),
	}
	if got := pinScopes(nil, deep, 40); len(got) != 2 || got[1].index != 2 {
		t.Fatalf("path %v, want [A C]", indexes(got))
	}
	// Flat SymbolInformation carries no nesting and answers one scope.
	flat := []lsp.DocumentSymbol{pinSymbol("Only", 6, 0, 10, 100)}
	if got := pinScopes(nil, flat, 50); len(got) != 1 || got[0].index != 0 {
		t.Fatalf("path %v, want [Only]", indexes(got))
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
		pinSymbol("(*Server).Start", 6, 0, 8, 36),
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

// scrollTo moves the viewport and lets it paint: VisibleLines answers
// for the last frame the editor drew, so the row is checked a frame
// after the one that scrolled.
func (h *harness) scrollTo(st *editor.State, line int) {
	h.t.Helper()
	st.ScrollTo(line)
	h.tt.Frame()
	h.tt.Frame()
}

func TestReadingPinShowsEnclosingScope(t *testing.T) {
	h, _ := pinHarness(t)
	st := h.a.activeEditor()
	// At the top of the file there is no answer and no row.
	h.tt.Frame()
	first, _ := st.VisibleLines()
	if first != 0 {
		t.Fatalf("top line %d", first)
	}
	if h.tt.HasText("(*Server).Start") {
		t.Fatal("the pin shows at the top of the file")
	}
	// Scroll the declaration above the viewport: the row names it.
	h.scrollTo(st, 20)
	if !h.tt.HasText("(*Server).Start") {
		t.Fatalf("the pin does not name Start: %q", h.tt.Texts())
	}
	// A scroll within the same declaration keeps one answer.
	h.scrollTo(st, 30)
	if !h.tt.HasText("(*Server).Start") || h.tt.HasText("Helper") {
		t.Fatalf("the pin changed inside one declaration: %q", h.tt.Texts())
	}
	// Back at the top of the scope the row disappears.
	h.scrollTo(st, 0)
	if h.tt.HasText("(*Server).Start") {
		t.Fatal("the pin stayed at the top of the scope")
	}
}

func TestReadingPinNestedSegmentsAndClick(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"b.go": `package b

func Outer() {
	one := 1
	two := 2
	three := 3
	four := 4
	five := 5
	six := 6
	seven := 7
	eight := 8
	nine := 9
	ten := 10
	eleven := 11
	twelve := 12
	thirteen := 13
	fourteen := 14
	fifteen := 15
	sixteen := 16
	seventeen := 17
	eighteen := 18
	nineteen := 19
	twenty := 20
}
`}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) {
		return []lsp.DocumentSymbol{
			pinSymbol("Pkg", 4, 0, 0, 30),
			pinSymbol("Outer", 12, 1, 2, 24),
			pinSymbol("deep", 12, 2, 6, 22),
		}, nil
	}}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	h.open("b.go")
	h.settle(func() bool { return !h.a.focus.pin.loading && len(h.a.focus.pin.symbols) == 3 })
	st := h.a.activeEditor()
	h.scrollTo(st, 15)
	// The skipped middle level collapses to an ellipsis before the row.
	if !h.tt.HasText("…") || !h.tt.HasText("Outer") || !h.tt.HasText("deep") {
		t.Fatalf("nested path missing: %q", h.tt.Texts())
	}
	// Clicking a segment goes to its declaration. The jump bar names the
	// same symbols, so the clicks go to the pin row's own bounds.
	clickPin := func(name string) {
		t.Helper()
		row, ok := h.tt.Access("Reading position pin")
		if !ok {
			t.Fatal("no pin row")
		}
		for _, info := range h.tt.AccessAll() {
			if info.Name != name {
				continue
			}
			b := info.Bounds
			if b.Y >= row.Bounds.Y && b.Y+b.H <= row.Bounds.Y+row.Bounds.H {
				h.tt.ClickAt(b.X+b.W/2, b.Y+b.H/2)
				return
			}
		}
		t.Fatalf("no %s in the pin row", name)
	}
	clickPin("Outer")
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 2 {
		t.Fatalf("click went to line %d, want 2", pos.Line)
	}
	// The navigation brought the declaration back in view, so the row
	// hides; scroll it away for the inner segment's click.
	h.scrollTo(st, 15)
	clickPin("deep")
	h.tt.Frame()
	if pos := st.CursorPos(); pos.Line != 6 {
		t.Fatalf("click went to line %d, want 6", pos.Line)
	}
}

func TestReadingPinOffAndNoServer(t *testing.T) {
	h, _ := pinHarness(t)
	st := h.a.activeEditor()
	h.scrollTo(st, 20)
	if !h.tt.HasText("(*Server).Start") {
		t.Fatal("the pin needs symbols to show")
	}
	// The setting turns the row off and back on.
	h.a.cfg.HideReadingPin = true
	h.tt.Frame()
	if h.tt.HasText("(*Server).Start") {
		t.Fatal("the pin stayed with the setting off")
	}
	h.a.cfg.HideReadingPin = false
	h.tt.Frame()
	if !h.tt.HasText("(*Server).Start") {
		t.Fatal("the pin did not come back with the setting on")
	}

	// Without a server there is no row.
	h2 := newHarness(t, writeTree(t, map[string]string{"a.go": pinSource}))
	h2.a.cfg.Trust(h2.a.root)
	h2.open("a.go")
	st2 := h2.a.activeEditor()
	h2.scrollTo(st2, 20)
	if h2.tt.HasText("(*Server).Start") {
		t.Fatal("the pin showed without a server")
	}
}
