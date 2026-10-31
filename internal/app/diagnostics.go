package app

import (
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
	"see/internal/uri"

	"see/internal/langserver"
	"see/internal/workspace"
)

// diagnosticsHandler shows a server's diagnostics in the editors of the
// document, if the server is still the one for it.
func (a *App) diagnosticsHandler(gen int) func(*langserver.Server, string, editor.Snapshot, []lsp.Diagnostic, string) {
	return func(m *langserver.Server, docURI string, snap editor.Snapshot, ds []lsp.Diagnostic, encoding string) {
		path, err := uri.ToPath(docURI)
		if err != nil {
			return
		}
		out := make([]editor.Diagnostic, 0, len(ds))
		offset := func(p lsp.Position) int {
			line := min(max(p.Line, 0), snap.LineCount()-1)
			return snap.LineStart(line) + lsp.ColumnFromLSP(snap.Line(line), p.Character, encoding)
		}
		for _, d := range ds {
			severity := editor.SeverityError
			switch d.Severity {
			case 2:
				severity = editor.SeverityWarning
			case 3:
				severity = editor.SeverityInfo
			case 4:
				severity = editor.SeverityHint
			}
			start, end := offset(d.Range.Start), offset(d.Range.End)
			out = append(out, editor.Diagnostic{Start: start, End: max(start, end), Severity: severity, Message: d.Message, Source: d.Source})
		}
		a.post(func() {
			if a.gen != gen || a.serverFor(path) != m {
				return
			}
			path = workspace.DocPath(a.root, a.realRoot, path)
			d := a.docs[path]
			if d == nil || d.buf == nil || d.buf.Version() != snap.Version() {
				return
			}
			d.diagnostics, d.diagnosticVersion = out, snap.Version()
			for _, g := range a.groups() {
				if st := g.eds[path]; st != nil {
					st.SetDiagnostics(snap.Version(), out)
				}
			}
		})
	}
}
