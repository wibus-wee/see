package app

import (
	"context"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

func TestTokenKindOf(t *testing.T) {
	for lspType, want := range map[string]editor.TokenKind{
		"keyword":       editor.TokenKeyword,
		"modifier":      editor.TokenKeyword,
		"type":          editor.TokenType,
		"interface":     editor.TokenType,
		"struct":        editor.TokenType,
		"enumMember":    editor.TokenProperty,
		"property":      editor.TokenProperty,
		"function":      editor.TokenFunction,
		"method":        editor.TokenFunction,
		"variable":      editor.TokenVariable,
		"parameter":     editor.TokenVariable,
		"namespace":     editor.TokenVariable,
		"number":        editor.TokenNumber,
		"string":        editor.TokenString,
		"comment":       editor.TokenComment,
		"operator":      editor.TokenOperator,
		"unknownFuture": editor.TokenInvalid,
	} {
		if got := tokenKindOf(lspType); got != want {
			t.Errorf("tokenKindOf(%q) = %v, want %v", lspType, got, want)
		}
	}
}

func TestDecodeSemanticTokens(t *testing.T) {
	buf := editor.NewBuffer("package main\n\nfunc main() {\n\tx := \"a中🎉\"\n}\n")
	snap := buf.Snapshot()
	legend := []string{"keyword", "variable", "string", "function"}
	// "package" (0,0,7 kw), "main" same line (0,8,4 func), "x" two lines
	// down (2,1,1 var), the string after it (0,5,7 str): the deltaChar
	// of each is from the previous token's start when on its line.
	data := []uint32{
		0, 0, 7, 0, 0,
		0, 8, 4, 3, 0,
		3, 1, 1, 1, 0,
		0, 5, 10, 2, 0,
	}
	toks := decodeSemanticTokens(data, legend, snap, lsp.UTF8)
	var got []semanticToken
	for _, line := range []int{0, 3} {
		got = append(got, toks[line]...)
	}
	want := []semanticToken{
		{0, 7, editor.TokenKeyword},
		{8, 12, editor.TokenFunction},
		{1, 2, editor.TokenVariable},
		{6, 16, editor.TokenString},
	}
	if len(got) != len(want) {
		t.Fatalf("tokens: %+v", toks)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestDecodeSemanticTokensUTF16(t *testing.T) {
	buf := editor.NewBuffer("x := 中🎉y\n")
	snap := buf.Snapshot()
	legend := []string{"variable"}
	// "x := 中" is 6 UTF-16 units and 🎉 two more, so the variable y at
	// byte 12 is at column 8 in UTF-16.
	toks := decodeSemanticTokens([]uint32{0, 8, 1, 0, 0}, legend, snap, lsp.UTF16)
	if len(toks[0]) != 1 || toks[0][0].start != 12 || toks[0][0].end != 13 {
		t.Fatalf("utf-16 token: %+v", toks[0])
	}
}

func TestDecodeSemanticTokensDrops(t *testing.T) {
	buf := editor.NewBuffer("ab\n")
	snap := buf.Snapshot()
	legend := []string{"variable"}
	data := []uint32{
		0, 0, 0, 0, 0, // zero length
		0, 0, 1, 9, 0, // a type index past the legend
		9, 0, 1, 0, 0, // a line the text does not have
	}
	if toks := decodeSemanticTokens(data, legend, snap, lsp.UTF8); len(toks) != 0 {
		t.Fatalf("tokens: %+v", toks)
	}
}

func TestMergeTokens(t *testing.T) {
	base := []editor.Token{{End: 20, Kind: editor.TokenText}}
	sems := []semanticToken{
		{start: 8, end: 12, kind: editor.TokenFunction},
		{start: 14, end: 16, kind: editor.TokenVariable},
	}
	got := mergeTokens(base, sems)
	want := []editor.Token{
		{End: 8, Kind: editor.TokenText},
		{End: 12, Kind: editor.TokenFunction},
		{End: 14, Kind: editor.TokenText},
		{End: 16, Kind: editor.TokenVariable},
		{End: 20, Kind: editor.TokenText},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("merge: %v, want %v", got, want)
	}
	// A semantic token spanning base tokens covers them all.
	got = mergeTokens([]editor.Token{
		{End: 5, Kind: editor.TokenKeyword},
		{End: 10, Kind: editor.TokenText},
		{End: 15, Kind: editor.TokenString},
	}, []semanticToken{{start: 3, end: 12, kind: editor.TokenType}})
	want = []editor.Token{
		{End: 3, Kind: editor.TokenKeyword},
		{End: 12, Kind: editor.TokenType},
		{End: 15, Kind: editor.TokenString},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("spanning merge: %v, want %v", got, want)
	}
}

// semanticHarness opens a Go file whose fake server answers semantic
// tokens, on a trusted workspace so the server starts; the returned
// channel sees each semantic highlighter the buffer's language makes.
func semanticHarness(t *testing.T, text string, fn func(context.Context, string, int, int) (*lsp.SemanticTokensData, error)) (*harness, *doc, chan *semanticHighlighter) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": text})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.semanticTokens = fn
	h.fc.semanticTypes = []string{"keyword", "variable", "function"}
	made := make(chan *semanticHighlighter, 8)
	var mu sync.Mutex
	semanticHighlighterHook = func(hl *semanticHighlighter) {
		mu.Lock()
		defer mu.Unlock()
		select {
		case made <- hl:
		default:
		}
	}
	t.Cleanup(func() { semanticHighlighterHook = nil })
	d := h.open("main.go")
	return h, d, made
}

// liveHighlighter waits for the semantic highlighter of d to exist and
// have its first tokens applied.
func liveHighlighter(t *testing.T, h *harness, d *doc, made chan *semanticHighlighter) *semanticHighlighter {
	t.Helper()
	var hl *semanticHighlighter
	h.settle(func() bool {
		select {
		case hl = <-made:
		default:
		}
		h.tt.Frame()
		return hl != nil && hl.ready
	})
	return hl
}

// kindAt is the kind of the token covering byte col of the line's
// merged tokens.
func kindAt(toks []editor.Token, col int) editor.TokenKind {
	for _, tk := range toks {
		if col < tk.End {
			return tk.Kind
		}
	}
	return editor.TokenText
}

func TestSemanticTokensColorLine(t *testing.T) {
	text := "package main\n\nfunc main() {\n\tx := 1\n}\n"
	h, d, made := semanticHarness(t, text, func(context.Context, string, int, int) (*lsp.SemanticTokensData, error) {
		// "x" on line 3 (bytes 1..2) is a variable; "main" on line 2 a
		// function.
		return &lsp.SemanticTokensData{Data: []uint32{
			2, 5, 4, 2, 0,
			1, 1, 1, 1, 0,
		}}, nil
	})
	hl := liveHighlighter(t, h, d, made)
	if got := kindAt(mustTokens(t, hl, 3, "\tx := 1"), 1); got != editor.TokenVariable {
		t.Fatalf("x colored %v, want variable", got)
	}
	if got := kindAt(mustTokens(t, hl, 2, "func main() {"), 5); got != editor.TokenFunction {
		t.Fatalf("main colored %v, want function", got)
	}
}

func mustTokens(t *testing.T, hl *semanticHighlighter, line int, text string) []editor.Token {
	t.Helper()
	hl.Highlight(line, 0)
	toks, _ := hl.Tokens(line, text, nil)
	return toks
}

func TestSemanticTokensLanguageWrapped(t *testing.T) {
	text := "package main\n"
	h, d, made := semanticHarness(t, text, func(context.Context, string, int, int) (*lsp.SemanticTokensData, error) {
		return &lsp.SemanticTokensData{}, nil
	})
	liveHighlighter(t, h, d, made)
	if d.buf.Language() == nil || d.buf.Language().Highlighter == nil {
		t.Fatal("buffer language was not wrapped")
	}
}

func TestSemanticTokensNotAskedWithoutCapability(t *testing.T) {
	root := writeTree(t, map[string]string{"main.go": "package main\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.open("main.go")
	h.tt.Frame()
	h.tt.Frame()
	if slices.Contains(h.fc.calls(), "semanticTokens") {
		t.Fatal("semantic tokens asked of a server without the capability")
	}
}

func TestSemanticTokensEditRefetches(t *testing.T) {
	text := "package main\n\nvar x int\n"
	var calls atomic.Int32
	h, d, made := semanticHarness(t, text, func(context.Context, string, int, int) (*lsp.SemanticTokensData, error) {
		calls.Add(1)
		return &lsp.SemanticTokensData{Data: []uint32{2, 4, 1, 1, 0}}, nil
	})
	liveHighlighter(t, h, d, made)
	d.buf.Set(strings.Replace(text, "x int", "y int", 1))
	h.tt.Frame() // the buffer's edit marks the tokens stale and wakes a fetch
	h.settle(func() bool {
		h.tt.Frame()
		return calls.Load() > 1
	})
}
