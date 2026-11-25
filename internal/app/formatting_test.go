package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

// formattingHarness opens a Go file whose fake server answers the
// formatting requests the test arms.
func formattingHarness(t *testing.T) (*harness, *doc, *editor.State) {
	t.Helper()
	root := writeTree(t, map[string]string{
		"main.go": "package main\n\nfunc main() {\n    x := 1\n    _ = x\n}\n",
	})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	d := h.open("main.go")
	return h, d, h.a.activeEditor()
}

func armDocumentFormat(h *harness, edits []lsp.TextEdit) {
	h.fc.format.document = &struct {
		opts  lsp.FormattingOptions
		edits []lsp.TextEdit
		err   error
	}{edits: edits}
}

func armRangeFormat(h *harness, edits []lsp.TextEdit) {
	h.fc.format.rng = &struct {
		r     lsp.Range
		opts  lsp.FormattingOptions
		edits []lsp.TextEdit
		err   error
	}{edits: edits}
}

func TestFormatDocumentAppliesEdits(t *testing.T) {
	h, d, st := formattingHarness(t)
	armDocumentFormat(h, []lsp.TextEdit{
		{Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 0}, End: lsp.Position{Line: 3, Character: 4}}, NewText: "\t"},
		{Range: lsp.Range{Start: lsp.Position{Line: 4, Character: 0}, End: lsp.Position{Line: 4, Character: 4}}, NewText: "\t"},
	})
	h.tt.Frame()
	h.a.formatDocument()
	h.settle(func() bool {
		requested := false
		for _, s := range h.fc.calls() {
			if s == "formatting" {
				requested = true
				break
			}
		}
		snap := d.buf.Snapshot()
		return requested && snap.Line(3) == "\tx := 1" && snap.Line(4) == "\t_ = x"
	})
	// The request carried the file's FormattingOptions: Go indents with
	// tabs of 4.
	fc := h.fc.format.document
	if fc.opts.TabSize != 4 || fc.opts.InsertSpaces {
		t.Fatalf("options: %+v", fc.opts)
	}
	// The edits landed in the buffer in one step and reached the server.
	if got := d.buf.Snapshot().Line(3); got != "\tx := 1" {
		t.Fatalf("line 3: %q", got)
	}
	if got := d.buf.Snapshot().Line(4); got != "\t_ = x" {
		t.Fatalf("line 4: %q", got)
	}
	if h.a.finding || h.a.notice == "" {
		t.Fatal("no formatted note")
	}
	_ = st
	h.settle(func() bool {
		for _, s := range h.fc.calls() {
			if s == "change main.go" {
				return true
			}
		}
		return false
	})
}

func TestFormatSelectionSendsRange(t *testing.T) {
	h, d, st := formattingHarness(t)
	armRangeFormat(h, []lsp.TextEdit{
		{Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 0}, End: lsp.Position{Line: 3, Character: 4}}, NewText: "\t"},
	})
	// Select part of line 3 into line 4: the request covers whole lines.
	start := d.buf.Snapshot().LineStart(3) + 5
	end := d.buf.Snapshot().LineStart(4) + 2
	st.Select(start, end)
	h.tt.Frame()
	h.a.formatSelection()
	h.settle(func() bool {
		requested := false
		for _, s := range h.fc.calls() {
			if s == "rangeFormatting" {
				requested = true
				break
			}
		}
		return requested && d.buf.Snapshot().Line(3) == "\tx := 1"
	})
	want := lsp.Range{Start: lsp.Position{Line: 3, Character: 0}, End: lsp.Position{Line: 4, Character: 9}}
	fc := h.fc.format.rng
	if fc.r != want {
		t.Fatalf("range: %+v, want %+v", fc.r, want)
	}
	if fc.opts.TabSize != 4 || fc.opts.InsertSpaces {
		t.Fatalf("options: %+v", fc.opts)
	}
	if got := d.buf.Snapshot().Line(3); got != "\tx := 1" {
		t.Fatalf("line 3: %q", got)
	}
}

func TestFormatCommandEnabled(t *testing.T) {
	h, _, st := formattingHarness(t)
	a := h.a
	armDocumentFormat(h, nil)
	h.tt.Frame()
	if !a.commandEnabled("format.document") {
		t.Fatal("Format Document disabled")
	}
	if a.commandEnabled("format.selection") {
		t.Fatal("Format Selection enabled without a selection")
	}
	// A selection still leaves it off: the server answers no ranges.
	st.Select(0, 2)
	h.tt.Frame()
	if a.commandEnabled("format.selection") {
		t.Fatal("Format Selection enabled without the capability")
	}
	// Range formatting plus a selection turns it on; none turns it off.
	armRangeFormat(h, nil)
	h.tt.Frame()
	if !a.commandEnabled("format.selection") {
		t.Fatal("Format Selection disabled with capability and selection")
	}
	st.Select(2, 2)
	h.tt.Frame()
	if a.commandEnabled("format.selection") {
		t.Fatal("Format Selection enabled on an empty selection")
	}
}

func TestFormatUnsupported(t *testing.T) {
	h, _, st := formattingHarness(t)
	a := h.a
	h.tt.Frame()
	document, rng := a.formatEnabled(a.activePath())
	if document || rng {
		t.Fatal("formatEnabled on a server without the capability")
	}
	st.Select(0, 2)
	h.tt.Frame()
	if a.commandEnabled("format.document") || a.commandEnabled("format.selection") {
		t.Fatal("format commands enabled without the capability")
	}
}
