package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

type diagnosticClient struct{ *fakeClient }

func (c *diagnosticClient) Diagnostics(ctx context.Context, uri string) ([]lsp.Diagnostic, bool, error) {
	return []lsp.Diagnostic{{Range: lsp.Range{Start: lsp.Position{Line: 0, Character: 3}, End: lsp.Position{Line: 0, Character: 10}}, Severity: 1, Message: "Unknown type"}}, true, nil
}

func TestOpenDocumentRequestsDiagnostics(t *testing.T) {
	root := writeTree(t, map[string]string{"a.ts": "😀 Missing = 1"})
	h := newHarness(t, root)
	h.a.cfg.trust(root)
	h.a.tsLSP.stop()
	client := &diagnosticClient{&fakeClient{enc: lsp.UTF16}}
	m := newGoLSP(root, func(context.Context, string, func(error)) (langClient, error) { return client, nil }, nil)
	m.language = typeScriptLanguage
	m.onDiagnostics = h.a.diagnosticsHandler(h.a.gen, m)
	h.a.tsLSP = m
	defer m.stopWait(time.Second)
	d := h.open("a.ts")
	h.settle(func() bool { return len(h.ed(d).Diagnostics()) > 0 })
	diag := h.ed(d).Diagnostics()[0]
	if diag.Message != "Unknown type" || diag.Severity != editor.SeverityError || d.buf.Snapshot().Slice(diag.Start, diag.End) != "Missing" {
		t.Fatalf("diagnostic: %+v", diag)
	}
	old := d.buf.Snapshot()
	d.buf.Reset("new text")
	m.onDiagnostics(d.uri, old, []lsp.Diagnostic{{Message: "stale"}}, lsp.UTF16)
	for len(h.q) > 0 {
		(<-h.q)()
	}
	if ds := h.ed(d).Diagnostics(); len(ds) > 0 && ds[0].Message == "stale" {
		t.Fatal("applied stale diagnostic")
	}
}

func TestTrustPromptOnOpeningCode(t *testing.T) {
	root := writeTree(t, map[string]string{"a.ts": "const x = 1", "b.ts": "const y = 2"})
	h := newHarness(t, root)
	h.open("a.ts")
	if !h.tt.HasText("Trust Folder") {
		t.Fatal("trust prompt missing")
	}
	h.click("Not Now")
	h.open("b.ts")
	if h.tt.HasText("Trust Folder") {
		t.Fatal("dismissed prompt returned")
	}
}

func TestOutputContainsServerFailure(t *testing.T) {
	h := newHarness(t, "")
	h.a.serverStateLog("tsrs", lspFailed, context.DeadlineExceeded)
	st := ui.NewTester(h.a.logView, 700, 300)
	if !strings.Contains(strings.Join(st.Texts(), " "), "tsrs") || !st.HasText("Clear Output") {
		t.Fatalf("output: %v", st.Texts())
	}
	if err := st.Click("Clear Output"); err != nil {
		t.Fatal(err)
	}
	if len(h.a.logs.snapshot()) != 0 {
		t.Fatal("output not cleared")
	}
}
