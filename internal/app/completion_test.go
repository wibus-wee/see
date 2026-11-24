package app

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

func TestCompletionItemsOnlyAcceptSafePrefixEdits(t *testing.T) {
	buf := editor.NewBuffer("ab, suffix")
	req := editor.CompletionRequest{Offset: 2, Prefix: "ab", Version: buf.Version()}
	good := lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 2}}
	bad := lsp.Range{Start: lsp.Position{Line: 0, Character: 0}, End: lsp.Position{Line: 0, Character: 1}}
	got := completionItems([]lsp.CompletionItem{
		{Label: "abi", Kind: 10, Detail: "boolean", Documentation: "docs", FilterText: "abi", TextEdit: &lsp.CompletionTextEdit{Range: &good, NewText: "abi"}},
		{Label: "wrong range", TextEdit: &lsp.CompletionTextEdit{Range: &bad, NewText: "wrong"}},
		{Label: "extra edit", HasAdditionalEdits: true},
		{Label: "command", HasCommand: true},
		{Label: "snippet", InsertTextFormat: 2},
	}, buf, req, lsp.UTF16)
	if len(got) != 1 || got[0].Label != "abi" || got[0].Insert != "abi" ||
		got[0].Detail != "boolean" || got[0].Documentation != "docs" ||
		got[0].FilterText != "abi" || got[0].Kind != editor.CompletionField {
		t.Fatalf("safe completions = %+v", got)
	}
}

func TestCompletionAutomaticCtrlSpaceAndAcceptPreservesSuffix(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	var requests atomic.Int32
	h.fc.completion = func(_ context.Context, _ string, pos lsp.Position) (*lsp.CompletionResult, error) {
		n := requests.Add(1)
		item := lsp.CompletionItem{Label: "abi", Kind: 10, Detail: "boolean", FilterText: "abi", InsertText: "abi"}
		if n == 1 || n == 2 {
			rng := lsp.Range{
				Start: lsp.Position{Line: pos.Line, Character: pos.Character - 2},
				End:   pos,
			}
			item.TextEdit = &lsp.CompletionTextEdit{Range: &rng, NewText: "abi"}
		}
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{item}}, nil
	}
	st := h.a.activeEditor()
	at := strings.Index(d.buf.String(), ", next")
	st.GoTo(d.buf.Pos(at))
	var explicit atomic.Bool
	original := st.OnCompletion
	st.OnCompletion = func(req editor.CompletionRequest) {
		if req.Explicit {
			explicit.Store(true)
		}
		original(req)
	}
	h.tt.Frame()
	h.tt.Type("ab")
	waitForCompletion(t, h, "abi")
	if countCalls(h.fc.calls(), "completion") == 0 {
		t.Fatal("typing a property prefix did not request completions")
	}
	group := h.a.focus
	h.a.signature = signatureState{
		group: group, doc: d, editor: st, offset: st.Cursor(), version: d.buf.Version(),
		help: signatureHelpResult(),
	}
	if h.a.signatureHelp(group, d) != nil {
		t.Fatal("signature help remained visible over the completion menu")
	}
	if h.a.signatureInput(group, st, ui.InputEvent{Kind: ui.InputKeyDown, Key: ui.KeyDown}) {
		t.Fatal("signature help consumed completion navigation")
	}
	if h.a.signature.active != 0 {
		t.Fatal("completion navigation changed the signature overload")
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	if !strings.Contains(d.buf.String(), "abi, next: 1") {
		t.Fatalf("accepting completion changed suffix: %q", d.buf.String())
	}
	before := countCalls(h.fc.calls(), "completion")
	h.tt.Key(ui.Ctrl, ui.KeySpace)
	deadline := time.Now().Add(5 * time.Second)
	for countCalls(h.fc.calls(), "completion") <= before {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("Ctrl+Space completion request did not arrive")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !explicit.Load() || countCalls(h.fc.calls(), "completion") <= before {
		t.Fatalf("Ctrl+Space did not request explicitly: explicit=%v calls=%v", explicit.Load(), h.fc.calls())
	}
}

func TestCompletionTrustGateAndStaleCaret(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { ab, next: 1 }\n", false)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
	}
	st := h.a.activeEditor()
	at := strings.Index(d.buf.String(), "ab") + 2
	st.GoTo(d.buf.Pos(at))
	st.OnCompletion(editor.CompletionRequest{Offset: at, Prefix: "ab", Version: d.buf.Version()})
	if countCalls(h.fc.calls(), "completion") != 0 {
		t.Fatal("untrusted workspace requested completions")
	}

	h.a.cfg.Trust(h.a.root)
	called := make(chan struct{}, 1)
	release := make(chan struct{})
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		called <- struct{}{}
		<-release
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
	}
	st.OnCompletion(editor.CompletionRequest{Offset: at, Prefix: "ab", Version: d.buf.Version()})
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("trusted completion request did not reach the server")
	}
	st.GoTo(d.buf.Pos(0))
	h.tt.Frame()
	close(release)
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if h.tt.HasText("abi") || h.a.completionFor(h.a.focus).visible {
		t.Fatalf("stale caret response reopened completion: %q", h.tt.Texts())
	}
}

func TestCompletionEscapeAndDocumentSwitchInvalidateReplies(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	st := h.a.activeEditor()
	at := strings.Index(d.buf.String(), ", next")
	st.GoTo(d.buf.Pos(at))
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
	}
	h.tt.Frame()
	h.tt.Type("ab")
	waitForCompletion(t, h, "abi")
	cs := h.a.completionFor(h.a.focus)
	if !cs.visible {
		t.Fatal("completion popup was not visible before Escape")
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("abi") || cs.visible {
		t.Fatal("Escape did not dismiss the visible completion popup")
	}

	firstRelease := make(chan struct{})
	firstCalled := make(chan struct{}, 1)
	firstReturned := make(chan struct{}, 1)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		firstCalled <- struct{}{}
		defer func() { firstReturned <- struct{}{} }()
		<-firstRelease
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
	}
	st.GoTo(d.buf.Pos(at + 2))
	st.OnCompletion(editor.CompletionRequest{Offset: at + 2, Prefix: "ab", Version: d.buf.Version()})
	select {
	case <-firstCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("completion request did not reach the server")
	}
	seq := cs.seq
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if cs.cancel != nil || cs.seq == seq || cs.visible {
		t.Fatal("Escape did not invalidate the pending completion request")
	}
	close(firstRelease)
	<-firstReturned
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if h.tt.HasText("abi") {
		t.Fatal("Escape allowed a late completion to reopen the menu")
	}

	secondRelease := make(chan struct{})
	secondCalled := make(chan struct{}, 1)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		secondCalled <- struct{}{}
		<-secondRelease
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
	}
	st.GoTo(d.buf.Pos(at + 2))
	st.OnCompletion(editor.CompletionRequest{Offset: at + 2, Prefix: "ab", Version: d.buf.Version()})
	select {
	case <-secondCalled:
	case <-time.After(5 * time.Second):
		t.Fatal("second completion request did not reach the server")
	}
	h.open("other.ts")
	close(secondRelease)
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if h.tt.HasText("abi") || h.a.completionFor(h.a.focus).visible {
		t.Fatal("document switch allowed a late completion to reopen the menu")
	}
}

func TestCompletionErrorClearsRequest(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { ab }\n", true)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		return nil, errors.New("completion failed")
	}
	st := h.a.activeEditor()
	at := strings.Index(d.buf.String(), "ab") + 2
	st.GoTo(d.buf.Pos(at))
	st.OnCompletion(editor.CompletionRequest{Offset: at, Prefix: "ab", Version: d.buf.Version()})
	deadline := time.Now().Add(5 * time.Second)
	for h.a.completionFor(h.a.focus).cancel != nil {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("completion error left the request active")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func newTSCompletionHarness(t *testing.T, source string, trusted bool) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"bootstrap.go": "package p\n",
		"main.ts":      source,
		"other.ts":     "const other = 1\n",
	})
	h := newHarness(t, root)
	if trusted {
		h.a.cfg.Trust(root)
	}
	h.open("bootstrap.go")
	h.a.tsLSP = h.a.lsp
	d := h.open("main.ts")
	t.Cleanup(func() {
		if h.a.lsp != nil {
			h.a.lsp.StopWait(5 * time.Second)
		}
	})
	return h, d
}

func waitForCompletion(t *testing.T, h *harness, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.tt.HasText(text) {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("completion %q did not appear: calls=%v texts=%q", text, h.fc.calls(), h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
}
