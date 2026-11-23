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

func TestCallStartAtNestedLiterals(t *testing.T) {
	text := `outer({ abi: inner(1), list: [x], callback: function() { inner2(2) }})`
	buf := editor.NewBuffer(text)
	for _, tc := range []struct {
		name string
		at   int
		want int
	}{
		{"object property", strings.Index(text, "abi") + 3, signatureOff},
		{"nested call in object", strings.Index(text, "inner(") + len("inner(") + 1, strings.Index(text, "inner(") + len("inner")},
		{"array element", strings.Index(text, "[x") + 1, signatureOff},
		{"callback body", strings.Index(text, "function() {") + len("function() {"), signatureOff},
		{"nested call in callback", strings.Index(text, "inner2(") + len("inner2(") + 1, strings.Index(text, "inner2(") + len("inner2")},
	} {
		if got := callStartAt(buf, tc.at); got != tc.want {
			t.Errorf("%s: callStartAt = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestSignatureStripBoundsClampAndHide(t *testing.T) {
	bounds := ui.Rect{X: 10, Y: 20, W: 300, H: 100}
	r, ok := signatureStripBounds(bounds, ui.Point{X: 300, Y: 55}, 40, 15, 120, 30)
	if !ok || r.W != 120 || r.X >= 300 || r.X < bounds.X || r.X+r.W > bounds.X+bounds.W {
		t.Fatalf("right-edge strip = %+v, visible=%v", r, ok)
	}
	r, ok = signatureStripBounds(ui.Rect{X: 10, Y: 20, W: 900, H: 200}, ui.Point{X: 400, Y: 60}, 40, 15, 900, 30)
	if !ok || r.W != 600 {
		t.Fatalf("content width was not capped: %+v, visible=%v", r, ok)
	}
	if _, ok := signatureStripBounds(ui.Rect{W: 200, H: 100}, ui.Point{X: 100, Y: 40}, 25, 15, 900, 30); !ok {
		t.Fatal("strip did not clamp to a narrow viewport")
	}
	if _, ok := signatureStripBounds(ui.Rect{W: 200, H: 60}, ui.Point{X: 100, Y: 40}, 30, 15, 100, 40); ok {
		t.Fatal("strip clipped across an offscreen line")
	}
	if _, ok := signatureStripBounds(ui.Rect{W: 200, H: 100}, ui.Point{X: 100, Y: 110}, 101, 15, 100, 20); ok {
		t.Fatal("strip shown for an offscreen caret")
	}
}

func TestSignatureIgnoresUnfocusedGroup(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	active, st := h.a.focus, h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	h.a.syncSignature(active, d, st)
	other := &group{id: active.id + 100, active: d.path}
	h.a.syncSignature(other, d, st)
	if h.a.signature.group != active {
		t.Fatalf("unfocused group replaced signature owner: %p", h.a.signature.group)
	}
	if h.a.signatureHelp(other, d) != nil {
		t.Fatal("unfocused group drew the active group's signature")
	}
	h.a.cancelSignature()
}

func TestSignatureCaretRefreshDropsSameCallStaleReply(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc main() { greet(1, 2) }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.cfg.ParameterHelp = "trigger"
	first := make(chan lsp.Position, 1)
	second := make(chan lsp.Position, 1)
	release := make(chan struct{})
	var calls atomic.Int32
	h.fc.signature = func(_ context.Context, _ string, pos lsp.Position) (*lsp.SignatureHelp, error) {
		if calls.Add(1) == 1 {
			first <- pos
			<-release
			return signatureHelpActive(0), nil
		}
		second <- pos
		return signatureHelpActive(1), nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	st := h.a.activeEditor()
	pos1 := strings.Index(d.buf.String(), "1, 2") + 1
	pos2 := strings.Index(d.buf.String(), "2)") + 1
	st.GoTo(d.buf.Pos(pos1))
	h.a.showSignatureAtCaret()
	firstPos := waitSignatureChannel(t, h, first)
	st.GoTo(d.buf.Pos(pos2))
	h.tt.Frame()
	close(release)
	secondPos := waitSignatureChannel(t, h, second)
	if secondPos.Character == firstPos.Character {
		t.Fatal("caret movement did not change the signature request position")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.tt.Frame()
		drainHarnessPosts(h)
		if h.a.signature.help != nil && h.a.signature.help.ActiveParameter != nil && *h.a.signature.help.ActiveParameter == 1 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("new caret's signature was not shown: calls=%v state=%+v", h.fc.calls(), h.a.signature)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if h.a.signature.offset != pos2 {
		t.Fatalf("signature offset = %d, want %d", h.a.signature.offset, pos2)
	}
}

func signatureHelpActive(parameter int) *lsp.SignatureHelp {
	h := signatureHelpResult()
	h.ActiveParameter = ptr(parameter)
	return h
}

func waitSignatureChannel(t *testing.T, h *harness, ch <-chan lsp.Position) lsp.Position {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		select {
		case pos := <-ch:
			return pos
		default:
		}
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("signature request did not arrive")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSignatureEscapePersistsAcrossEditsAndManualReopens(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	waitForSignature(t, h, "greet(n int, s string) int")
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	at := strings.Index(d.buf.String(), "2)")
	d.buf.Edit(editor.Edit{Start: at, End: at, Text: " "})
	st.GoTo(d.buf.Pos(at + 2))
	for range 20 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if countCalls(h.fc.calls(), "signatureHelp") != 1 || h.tt.HasText("greet(n int, s string) int") {
		t.Fatalf("dismissal did not survive same-call edit: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
	}
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeySpace)
	waitForSignature(t, h, "greet(n int, s string) int")
	if countCalls(h.fc.calls(), "signatureHelp") != 2 {
		t.Fatalf("manual trigger did not reopen help: %v", h.fc.calls())
	}
}

func waitForSignature(t *testing.T, h *harness, text string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !h.tt.HasText(text) {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("signature %q did not appear: %q", text, h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSignatureManualTriggerSurvivesDocumentSwitch(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc main() { greet(1, 2) }\n",
		"b.go": "package b\nfunc main() { greet(3, 4) }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.cfg.ParameterHelp = "trigger"
	h.fc.signature = func(context.Context, string, lsp.Position) (*lsp.SignatureHelp, error) {
		return signatureHelpResult(), nil
	}
	d1 := h.open("a.go")
	st1 := h.a.activeEditor()
	st1.GoTo(d1.buf.Pos(strings.Index(d1.buf.String(), "1, 2") + 1))
	h.a.showSignatureAtCaret()
	waitForSignature(t, h, "greet(n int, s string) int")
	d2 := h.open("b.go")
	st2 := h.a.activeEditor()
	st2.GoTo(d2.buf.Pos(strings.Index(d2.buf.String(), "3, 4") + 1))
	h.a.showSignatureAtCaret()
	deadline := time.Now().Add(5 * time.Second)
	for h.a.signature.doc != d2 || h.a.signature.help == nil || countCalls(h.fc.calls(), "signatureHelp") < 2 {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("manual trigger was lost on document switch: calls=%v state=%+v", h.fc.calls(), h.a.signature)
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
}
