package app

import (
	"strings"
	"testing"
	"time"
)

func TestInspectorFollowsCaret(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	h.a.cfg.QuickHelpInspector = true
	st := h.a.focus.eds[d.path]

	// Move the caret onto "helper" and let the debounce fire.
	st.GoTo(st.Buffer.Pos(offset))
	h.tt.Frame()

	// Wait for the inspector to show the hover content.
	deadline := time.After(5 * time.Second)
	for {
		h.tt.Frame()
		if h.tt.HasText("Returns the result.") {
			break
		}
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("inspector did not show documentation: texts=%q", h.tt.Texts())
		}
	}
	if !h.tt.HasText("Declaration") {
		t.Fatal("inspector did not show the declaration section")
	}
}

func TestInspectorPinHoldsSymbol(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	h.a.cfg.QuickHelpInspector = true
	st := h.a.focus.eds[d.path]

	st.GoTo(st.Buffer.Pos(offset))
	h.tt.Frame()
	waitForInspector(t, h, "Returns the result.")

	// Pin, then move the caret to another symbol on the same line; the
	// content should remain.
	h.a.pinInspector()
	st.GoTo(st.Buffer.Pos(strings.Index(d.buf.String(), "call")))
	h.tt.Frame()

	if !h.tt.HasText("Returns the result.") {
		t.Fatal("pinned inspector lost its documentation")
	}

	// Unpinning resumes following the caret; the next request lands after
	// the debounce, so pump until the content returns.
	h.a.pinInspector()
	h.tt.Frame()
	waitForInspector(t, h, "Returns the result.")
}

func TestInspectorKeepsContentWhileLoading(t *testing.T) {
	h, d, offset := quickHelpHarness(t)
	h.a.cfg.QuickHelpInspector = true
	st := h.a.focus.eds[d.path]

	st.GoTo(st.Buffer.Pos(offset))
	h.tt.Frame()
	waitForInspector(t, h, "Returns the result.")

	// Hold the next hover request; the old content should stay while the
	// new one is in flight.
	started, release := holdQuickHelpHover(h)
	st.GoTo(st.Buffer.Pos(strings.Index(d.buf.String(), "call")))
	h.tt.Frame()
	// Pump the queued debounce/post work until the request reaches the
	// held hover, with a deadline so a missed signal fails rather than hangs.
	waitForSignalOrTimeout(t, h, started, "the held hover request")

	if !h.tt.HasText("Returns the result.") {
		t.Fatal("inspector dropped content while a new request was in flight")
	}
	close(release)
	waitForInspector(t, h, "Returns the result.")
}

func TestInspectorWidthClamp(t *testing.T) {
	h := newHarness(t, "")
	h.a.cfg.QuickHelpWidth = 0
	if got := h.a.inspectorW(); got != inspectorDefWidth {
		t.Fatalf("inspectorW with width 0 = %v, want %v", got, inspectorDefWidth)
	}
	h.a.cfg.QuickHelpWidth = 50
	if got := h.a.inspectorW(); got != inspectorMinWidth {
		t.Fatalf("inspectorW below min = %v, want %v", got, inspectorMinWidth)
	}
	h.a.cfg.QuickHelpWidth = 999
	if got := h.a.inspectorW(); got != inspectorMaxWidth {
		t.Fatalf("inspectorW above max = %v, want %v", got, inspectorMaxWidth)
	}
}

func TestInspectorToggle(t *testing.T) {
	h := newHarness(t, "")
	if h.a.inspectorOpen() {
		t.Fatal("inspector opened by default")
	}
	h.a.toggleInspector()
	if !h.a.inspectorOpen() {
		t.Fatal("inspector did not open on toggle")
	}
	h.a.toggleInspector()
	if h.a.inspectorOpen() {
		t.Fatal("inspector did not close on toggle")
	}
}

// waitForSignalOrTimeout pumps the harness's queued work until signal
// closes, as a debounced request that must reach the language server.
func waitForSignalOrTimeout(t *testing.T, h *harness, signal <-chan struct{}, what string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		select {
		case <-signal:
			return
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("timed out waiting for %s: calls=%v", what, h.fc.calls())
		}
	}
}

func waitForInspector(t *testing.T, h *harness, text string) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		h.tt.Frame()
		if h.tt.HasText(text) {
			return
		}
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-time.After(10 * time.Millisecond):
		case <-deadline:
			t.Fatalf("inspector did not show %q: calls=%v texts=%q", text, h.fc.calls(), h.tt.Texts())
		}
	}
}
