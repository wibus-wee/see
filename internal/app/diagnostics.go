package app

import (
	"path/filepath"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/uri"
	"see/internal/workspace"
)

func (a *App) diagnosticsHandler(gen int) langserver.DiagnosticsHandler {
	return func(m *langserver.Server, report langserver.DiagnosticReport) {
		path, err := uri.ToPath(report.URI)
		if err != nil {
			return
		}
		// A server spells a path as it was given it, which need not be the
		// disk's spelling; aligning it here, off the UI thread, is what
		// makes its diagnostics land on the same document the user edits.
		path = workspace.DiskSpelling(path)
		encoding := report.Client.Encoding()
		var out []editor.Diagnostic
		if report.Buffer != nil {
			out = editorDiagnostics(*report.Buffer, report.Items, encoding)
		}
		a.post.Post(func() {
			if a.gen != gen || m.Current() != report.Client {
				return
			}
			path = a.docPath(path)
			if report.Buffer == nil {
				if !a.withinWorkspace(path) || a.isWorkspaceFolder(path) || a.diagnosticsServerFor(path) != m {
					return
				}
				if len(report.Items) == 0 {
					delete(a.problems.disk, path)
				} else {
					if a.problems.disk == nil {
						a.problems.disk = make(map[string]*diskProblems)
					}
					a.problems.disk[path] = &diskProblems{server: m, client: report.Client, encoding: encoding, items: report.Items}
				}
				return
			}
			d := a.docs[path]
			if a.serverFor(path) != m || d == nil || d.buf == nil || d.buf.Version() != report.Buffer.Version() {
				return
			}
			d.diagnostics, d.diagnosticVersion = out, report.Buffer.Version()
			for _, g := range a.groups() {
				if st := g.eds[path]; st != nil {
					st.SetDiagnostics(d.diagnosticVersion, out)
				}
			}
		})
	}
}

func (a *App) diagnosticsServerFor(path string) *langserver.Server {
	switch filepath.Base(path) {
	case "go.mod", "go.work":
		return a.lsp
	}
	return a.serverFor(path)
}

func editorDiagnostics(snap editor.Snapshot, ds []lsp.Diagnostic, encoding string) []editor.Diagnostic {
	out := make([]editor.Diagnostic, 0, len(ds))
	offset := func(p lsp.Position) int {
		line := min(max(p.Line, 0), snap.LineCount()-1)
		return snap.LineStart(line) + lsp.ColumnFromLSP(snap.Line(line), p.Character, encoding)
	}
	for _, d := range ds {
		start, end := offset(d.Range.Start), offset(d.Range.End)
		out = append(out, editor.Diagnostic{Start: start, End: max(start, end), Severity: diagnosticSeverity(d.Severity), Message: d.Message, Source: d.Source})
	}
	return out
}

func diagnosticSeverity(severity int) editor.Severity {
	switch severity {
	case 2:
		return editor.SeverityWarning
	case 3:
		return editor.SeverityInfo
	case 4:
		return editor.SeverityHint
	default:
		return editor.SeverityError
	}
}

// goToProblem uses only diagnostics for the current buffer version.
func (a *App) goToProblem(dir int) {
	st := a.activeEditor()
	if st == nil {
		return
	}
	d := a.docs[a.activePath()]
	if d == nil || d.buf == nil || d.diagnosticVersion != d.buf.Version() {
		return
	}
	cursor := st.Cursor()
	next, wrap := -1, -1
	for _, problem := range d.diagnostics {
		start := problem.Start
		if start < 0 || start > d.buf.Len() {
			continue
		}
		if dir > 0 {
			if wrap < 0 || start < wrap {
				wrap = start
			}
			if start > cursor && (next < 0 || start < next) {
				next = start
			}
		} else {
			if wrap < 0 || start > wrap {
				wrap = start
			}
			if start < cursor && (next < 0 || start > next) {
				next = start
			}
		}
	}
	if next < 0 {
		next = wrap
	}
	if next < 0 || next == cursor {
		return
	}
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	st.GoTo(d.buf.Pos(next))
	a.focusEditor = st
}
