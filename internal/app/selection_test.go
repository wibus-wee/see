package app

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

const selectionFixture = "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n"

// selectionFakeClient answers textDocument/selectionRange from a test
// script, as scopeHighlightClient answers documentHighlight.
type selectionFakeClient struct {
	*fakeClient
	supported bool
	answer    func(ctx context.Context, uri string, positions []lsp.Position) ([]*lsp.SelectionRange, error)
}

func (c *selectionFakeClient) HasSelectionRanges() bool { return c.supported }

func (c *selectionFakeClient) SelectionRanges(ctx context.Context, uri string, positions []lsp.Position) ([]*lsp.SelectionRange, error) {
	c.record("selectionRange")
	return c.answer(ctx, uri, positions)
}

// selectionChain builds a SelectionRange chain of the byte ranges of
// text, innermost first, as a server's answer nests them.
func selectionChain(text string, encoding string, byteRanges ...[2]int) *lsp.SelectionRange {
	var head *lsp.SelectionRange
	for i := len(byteRanges) - 1; i >= 0; i-- {
		head = &lsp.SelectionRange{
			Parent: head,
			Range: lsp.Range{
				Start: lspPositionAt(text, byteRanges[i][0], encoding),
				End:   lspPositionAt(text, byteRanges[i][1], encoding),
			},
		}
	}
	return head
}

func newSelectionHarness(t *testing.T, answer func(context.Context, string, []lsp.Position) ([]*lsp.SelectionRange, error)) (*harness, *doc, *selectionFakeClient) {
	t.Helper()
	root := writeTree(t, map[string]string{"main.go": selectionFixture})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	client := &selectionFakeClient{fakeClient: &fakeClient{enc: lsp.UTF16}, supported: true, answer: answer}
	manager := serverOf(root, client)
	h.a.lsp = manager
	t.Cleanup(func() { manager.StopWait(5 * time.Second) })
	return h, h.open("main.go"), client
}

// The caret sits inside "hello": expand walks the server's chain — the
// string's inside, the call's argument list, the statement — and shrink
// unwinds the stack it left.
func TestExpandSelectionRangesChainAndShrink(t *testing.T) {
	var requests [][]lsp.Position
	h, d, client := newSelectionHarness(t, func(_ context.Context, _ string, positions []lsp.Position) ([]*lsp.SelectionRange, error) {
		requests = append(requests, positions)
		steps := [][2]int{
			{38, 43}, // hello
			{37, 44}, // "hello"
			{29, 45}, // println("hello")
			{28, 45}, // the statement's line
		}
		var chains []*lsp.SelectionRange
		for range positions {
			chains = append(chains, selectionChain(selectionFixture, lsp.UTF16, steps...))
		}
		return chains, nil
	})
	st := h.a.activeEditor()
	caret := strings.Index(selectionFixture, "hell")
	st.GoTo(d.buf.Pos(caret))
	h.tt.Frame()

	expand := func(want editor.Selection) {
		t.Helper()
		h.tt.Key(ui.Alt|ui.Shift, ui.KeyUp)
		h.settle(func() bool { return slices.Equal(st.Selections(), []editor.Selection{want}) })
	}
	expand(editor.Selection{Anchor: 38, Head: 43})
	expand(editor.Selection{Anchor: 37, Head: 44})
	expand(editor.Selection{Anchor: 29, Head: 45})
	if len(requests) != 3 {
		t.Fatalf("selectionRange requests: %d", len(requests))
	}
	if got := lspPositionAt(selectionFixture, caret, lsp.UTF16); requests[0][0] != got {
		t.Fatalf("request position = %+v, want %+v", requests[0][0], got)
	}
	if len(h.a.selStack) != 3 {
		t.Fatalf("selection stack: %+v", h.a.selStack)
	}
	if !slices.Contains(client.calls(), "selectionRange") {
		t.Fatal("selectionRange request was not sent")
	}

	shrink := func(want editor.Selection) {
		t.Helper()
		prev := st.Selections()
		h.tt.Key(ui.Alt|ui.Shift, ui.KeyDown)
		h.settle(func() bool { s := st.Selections(); return s != nil && !slices.Equal(s, prev) })
		if got := st.Selections(); !slices.Equal(got, []editor.Selection{want}) {
			t.Fatalf("shrink selection = %+v, want %+v", got, want)
		}
	}
	shrink(editor.Selection{Anchor: 37, Head: 44})
	shrink(editor.Selection{Anchor: 38, Head: 43})
	// The last shrink pops the caret the first expand pushed: a caret
	// is not strictly inside itself, so nothing pops and the editor's
	// own Select Smaller stays out.
	shrink(editor.Selection{Anchor: caret, Head: caret})
	if len(h.a.selStack) != 0 {
		t.Fatalf("selection stack not unwound: %+v", h.a.selStack)
	}
}

// The editor's Select Larger steps in when the server has no selection
// ranges: the word, then the line, as the fallback does.
func TestExpandSelectionFallsBackWithoutProvider(t *testing.T) {
	h, d, client := newSelectionHarness(t, nil)
	client.supported = false
	st := h.a.activeEditor()
	caret := strings.Index(selectionFixture, "hell")
	st.GoTo(d.buf.Pos(caret))
	h.tt.Frame()

	h.tt.Key(ui.Alt|ui.Shift, ui.KeyUp)
	h.settle(func() bool {
		return len(st.Selections()) == 1 && !st.Selections()[0].Empty()
	})
	got := st.Selections()[0]
	if got.Anchor != 38 || got.Head != 43 {
		t.Fatalf("fallback selection = %+v, want the word hello", got)
	}
	if slices.Contains(client.calls(), "selectionRange") {
		t.Fatal("selectionRange was requested of a server without the capability")
	}
}

// A request error answers as no capability does: the editor's step.
func TestExpandSelectionFallsBackOnServerError(t *testing.T) {
	h, d, _ := newSelectionHarness(t, func(context.Context, string, []lsp.Position) ([]*lsp.SelectionRange, error) {
		return nil, errors.New("server exploded")
	})
	st := h.a.activeEditor()
	caret := strings.Index(selectionFixture, "hell")
	st.GoTo(d.buf.Pos(caret))
	h.tt.Frame()

	h.tt.Key(ui.Alt|ui.Shift, ui.KeyUp)
	h.settle(func() bool {
		return len(st.Selections()) == 1 && !st.Selections()[0].Empty()
	})
	got := st.Selections()[0]
	if got.Anchor != 38 || got.Head != 43 {
		t.Fatalf("fallback selection = %+v, want the word hello", got)
	}
	if len(h.a.selStack) != 0 {
		t.Fatalf("a failed expand pushed onto the stack: %+v", h.a.selStack)
	}
}

// An empty answer leaves the selection where it is.
func TestExpandSelectionEmptyResult(t *testing.T) {
	h, d, _ := newSelectionHarness(t, func(context.Context, string, []lsp.Position) ([]*lsp.SelectionRange, error) {
		return nil, nil
	})
	st := h.a.activeEditor()
	caret := strings.Index(selectionFixture, "hell")
	st.GoTo(d.buf.Pos(caret))
	h.tt.Frame()

	h.tt.Key(ui.Alt|ui.Shift, ui.KeyUp)
	h.settle(func() bool {
		return len(st.Selections()) == 1 && !st.Selections()[0].Empty()
	})
	got := st.Selections()[0]
	if got.Anchor != 38 || got.Head != 43 {
		t.Fatalf("fallback selection = %+v, want the word hello", got)
	}
}

// selectionExpansions steps to the first range covering the selection,
// so a selection partway down the chain resumes from where it is rather
// than restarting.
func TestSelectionExpansions(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": selectionFixture})
	h := newHarness(t, root)
	d := h.open("a.go")
	snap := d.buf.Snapshot()
	chain := selectionChain(selectionFixture, lsp.UTF16, [2]int{38, 43}, [2]int{37, 44}, [2]int{29, 45})

	// A caret inside "hello" expands to the string's inside.
	got, err := selectionExpansions(snap, []editor.Selection{{Anchor: 39, Head: 39}}, []*lsp.SelectionRange{chain}, lsp.UTF16)
	if err != nil || len(got) != 1 || got[0] != (editor.Selection{Anchor: 38, Head: 43}) {
		t.Fatalf("expansion = %+v, %v", got, err)
	}
	// The innermost range selected already: the next step is its parent.
	got, err = selectionExpansions(snap, []editor.Selection{{Anchor: 38, Head: 43}}, []*lsp.SelectionRange{chain}, lsp.UTF16)
	if err != nil || got[0] != (editor.Selection{Anchor: 37, Head: 44}) {
		t.Fatalf("expansion = %+v, %v", got, err)
	}
	// A selection wider than the whole chain stays as it is.
	got, err = selectionExpansions(snap, []editor.Selection{{Anchor: 0, Head: 44}}, []*lsp.SelectionRange{chain}, lsp.UTF16)
	if err != nil || got[0] != (editor.Selection{Anchor: 0, Head: 44}) {
		t.Fatalf("expansion = %+v, %v", got, err)
	}
	// A chain count not matching the selections is an error.
	if _, err = selectionExpansions(snap, []editor.Selection{{Anchor: 33, Head: 33}}, nil, lsp.UTF16); err == nil {
		t.Fatal("mismatched chains answered without an error")
	}
}
