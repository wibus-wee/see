package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

func TestCallStartAt(t *testing.T) {
	text := `package a

func call() {
	helper(1, (2 + 3), "x (not a call") // (nor this
	/* (nor this) */
	arr[i] = x{y: z}
}
`
	buf := editor.NewBuffer(text)
	inside := func(sub string, add int) int {
		return strings.Index(text, sub) + add
	}
	open := inside("helper(", 6)
	for _, tc := range []struct {
		name   string
		offset int
		want   int
	}{
		{"top level", inside("package", 3), -1},
		{"inside helper args", inside("helper(1", 10), open},
		{"inside nested paren", inside("(2 + 3)", 4), inside(", (2", 2)},
		{"at nested open", inside("(2 + 3)", 2), inside(", (2", 2)},
		{"after string quote paren", inside("\"x (not a call\")", 15), open},
		{"inside string paren", inside("\"x (not a call\")", 10), open},
		{"inside line comment", inside("// (nor this", 10), -1},
		{"inside block comment", inside("/* (nor this) */", 5), -1},
		{"inside index", inside("arr[i]", 5), -1},
		{"inside literal", inside("{y: z}", 3), -1},
		{"inside braces only", inside("{\n\thelper", 1), -1},
		{"before the call", inside("\thelper", 3), -1},
		{"at file end", len(text) - 2, -1},
	} {
		if got := callStartAt(buf, tc.offset); got != tc.want {
			t.Errorf("%s: callStartAt = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func signatureHarness(t *testing.T, help *lsp.SignatureHelp) (*harness, *doc) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc main() { greet(1, 2) }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.signature = func(context.Context, string, lsp.Position) (*lsp.SignatureHelp, error) {
		return help, nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	return h, d
}

func signatureHelpResult() *lsp.SignatureHelp {
	return &lsp.SignatureHelp{
		Signatures: []lsp.SignatureInformation{
			{Label: "greet(n int, s string) int"},
			{Label: "greet(n int)"},
		},
		ActiveSignature: ptr(0), ActiveParameter: ptr(0),
	}
}

func ptr[T any](v T) *T { return &v }

func TestSignatureHelpShowsAtCaret(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	deadline := time.Now().Add(5 * time.Second)
	for {
		h.tt.Frame()
		drainHarnessPosts(h)
		if h.tt.HasText("greet(n int, s string) int") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("signature strip did not appear: calls=%v texts=%q", h.fc.calls(), h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	if countCalls(h.fc.calls(), "signatureHelp") != 1 {
		t.Fatalf("signature help requests = %d", countCalls(h.fc.calls(), "signatureHelp"))
	}
}

func TestSignatureHelpManualTrigger(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	h.a.cfg.ParameterHelp = "trigger"
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if countCalls(h.fc.calls(), "signatureHelp") != 0 {
		t.Fatalf("On Trigger requested help without the shortcut: %v", h.fc.calls())
	}
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeySpace)
	deadline := time.Now().Add(5 * time.Second)
	for !h.tt.HasText("greet(n int, s string) int") {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("manual trigger did not show help: calls=%v", h.fc.calls())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSignatureHelpOffDoesNothing(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	h.a.cfg.ParameterHelp = "off"
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if countCalls(h.fc.calls(), "signatureHelp") != 0 {
		t.Fatalf("Off requested signature help: %v", h.fc.calls())
	}
}

func TestSignatureHelpEscapeDismisses(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	deadline := time.Now().Add(5 * time.Second)
	for !h.tt.HasText("greet(n int, s string) int") {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("signature strip did not appear")
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.tt.HasText("greet(n int, s string) int") {
		t.Fatal("Escape did not dismiss the signature strip")
	}
	// Inside the same call it stays down; the next call shows again.
	for range 30 {
		h.tt.Frame()
		drainHarnessPosts(h)
		if countCalls(h.fc.calls(), "signatureHelp") > 1 {
			t.Fatal("dismissed signature help re-asked in the same call")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSignatureHelpStaleReplyIsDropped(t *testing.T) {
	release := make(chan struct{})
	root := writeTree(t, map[string]string{
		"a.go": "package a\nfunc main() { greet(1, 2) }\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.fc.signature = func(context.Context, string, lsp.Position) (*lsp.SignatureHelp, error) {
		<-release
		return signatureHelpResult(), nil
	}
	d := h.open("a.go")
	t.Cleanup(func() { h.a.lsp.StopWait(5 * time.Second) })
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	deadline := time.Now().Add(5 * time.Second)
	for countCalls(h.fc.calls(), "signatureHelp") == 0 {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatal("request never sent")
		}
		time.Sleep(20 * time.Millisecond)
	}
	// The caret leaves the call before the reply lands.
	st.GoTo(d.buf.Pos(0))
	close(release)
	for range 50 {
		h.tt.Frame()
		drainHarnessPosts(h)
		time.Sleep(20 * time.Millisecond)
	}
	if h.a.signature.help != nil || h.tt.HasText("greet(n int, s string) int") {
		t.Fatalf("stale reply showed the strip: %q", h.tt.Texts())
	}
}

func TestSignatureHelpOverloadSwitch(t *testing.T) {
	h, d := signatureHarness(t, signatureHelpResult())
	st := h.a.activeEditor()
	st.GoTo(d.buf.Pos(strings.Index(d.buf.String(), "1, 2") + 1))
	deadline := time.Now().Add(5 * time.Second)
	for !h.tt.HasText("1/2") {
		h.tt.Frame()
		drainHarnessPosts(h)
		if time.Now().After(deadline) {
			t.Fatalf("overload counter did not appear: %q", h.tt.Texts())
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.tt.Key(0, ui.KeyDown)
	h.tt.Frame()
	if !h.tt.HasText("2/2") || !h.tt.HasText("greet(n int)") {
		t.Fatalf("Down did not switch the overload: %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyUp)
	h.tt.Frame()
	if !h.tt.HasText("1/2") {
		t.Fatalf("Up did not switch back: %q", h.tt.Texts())
	}
}
