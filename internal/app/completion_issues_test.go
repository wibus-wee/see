package app

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

func TestCompletionIncompleteResultsRefreshAfterTyping(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { , next: 1 }\n", true)
	at := strings.Index(d.buf.String(), ", next")
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(at))
	var calls atomic.Int32
	h.fc.completion = func(_ context.Context, _ string, pos lsp.Position) (*lsp.CompletionResult, error) {
		calls.Add(1)
		if pos.Character <= at+2 {
			return &lsp.CompletionResult{IsIncomplete: true, Items: []lsp.CompletionItem{{Label: "abi"}}}, nil
		}
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abiVersion"}}}, nil
	}
	h.tt.Frame()
	h.tt.Type("ab")
	waitForCompletion(t, h, "abi")
	if h.tt.HasText("abiVersion") {
		t.Fatal("incomplete response unexpectedly contained the later item")
	}
	before := calls.Load()
	h.tt.Type("i")
	deadline := time.Now().Add(5 * time.Second)
	for calls.Load() == before || !h.tt.HasText("abiVersion") {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("typing did not refresh incomplete results: calls=%d texts=%q", calls.Load(), h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCompletionMoveRebindsAndReleasesGroupState(t *testing.T) {
	h, d := newTSCompletionHarness(t, "const cfg = { ab, next: 1 }\n", true)
	from, st := h.a.focus, h.a.activeEditor()
	offset := strings.Index(d.buf.String(), "ab") + 2
	st.GoTo(d.buf.Pos(offset))
	release, called, returned := make(chan struct{}), make(chan struct{}, 1), make(chan struct{}, 1)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		called <- struct{}{}
		defer func() { returned <- struct{}{} }()
		<-release
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abiOld"}}}, nil
	}
	st.OnCompletion(editor.CompletionRequest{Offset: offset, Prefix: "ab", Version: d.buf.Version()})
	select {
	case <-called:
	case <-time.After(5 * time.Second):
		t.Fatal("initial completion request did not start")
	}
	old := h.a.completionFor(from)
	to := h.a.splitBeside(from, sideRight)
	h.a.moveTab(from, d.path, to, sideNone, 0)
	if old.cancel != nil || old.doc != nil || old.editor != nil {
		t.Fatal("moving the tab retained its completion request references")
	}
	close(release)
	<-returned
	for range 10 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(10 * time.Millisecond)
	}
	if from.eds[d.path] != nil || to.eds[d.path] != st || h.a.focus != to {
		t.Fatal("move did not reuse the editor in the destination group")
	}
	second := make(chan struct{}, 1)
	h.fc.completion = func(context.Context, string, lsp.Position) (*lsp.CompletionResult, error) {
		second <- struct{}{}
		return &lsp.CompletionResult{Items: []lsp.CompletionItem{{Label: "abiMoved"}}}, nil
	}
	st.OnCompletion(editor.CompletionRequest{Offset: offset, Prefix: "ab", Version: d.buf.Version()})
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("moved editor kept its original group's completion callback")
	}
	cs := h.a.completionFor(to)
	deadline := time.Now().Add(5 * time.Second)
	for cs.cancel != nil {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("moved completion result did not finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var bootstrap string
	for _, path := range from.order {
		if path != d.path {
			bootstrap = path
		}
	}
	h.a.closeTab(from, bootstrap)
	if _, ok := h.a.completions[from.id]; ok {
		t.Fatal("removed split retained completion state")
	}
	h.a.closeTab(to, d.path)
	if cs.doc != nil || cs.editor != nil || cs.path != "" || cs.cancel != nil {
		t.Fatal("closed document retained completion references")
	}
}

func TestHiddenSignatureDoesNotConsumeArrowKeys(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc main() { greet(1, 2) }\n" + strings.Repeat("// spacer\n", 100),
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.cfg.ParameterHelp = "trigger"
	h.fc.signature = func(context.Context, string, lsp.Position) (*lsp.SignatureHelp, error) {
		return signatureHelpResult(), nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	h.a.showSignatureAtCaret()
	waitForSignature(t, h, "greet(n int, s string) int")
	if !h.a.signature.visible {
		t.Fatal("signature strip was not drawn before scrolling")
	}
	st.ScrollTo(60)
	h.tt.Frame()
	if h.a.signature.visible {
		t.Fatal("signature strip remained visible after scrolling it offscreen")
	}
	before := st.Cursor()
	h.tt.Key(0, ui.KeyDown)
	h.tt.Frame()
	if st.Cursor() == before || h.a.signature.active != 0 {
		t.Fatal("hidden signature help consumed Down instead of moving the caret")
	}
}
