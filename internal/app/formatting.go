package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/locales"
)

// formatClient is a language client that answers the formatting
// requests, whole document and a range of it.
type formatClient interface {
	HasDocumentFormatting() bool
	HasDocumentRangeFormatting() bool
	DocumentFormatting(ctx context.Context, uri string, opts lsp.FormattingOptions) ([]lsp.TextEdit, error)
	DocumentRangeFormatting(ctx context.Context, uri string, r lsp.Range, opts lsp.FormattingOptions) ([]lsp.TextEdit, error)
}

// formatEnabled reports what kind of formatting the file's server
// answers, for the commands and the context menu items: the document's
// only, or a range's too.
func (a *App) formatEnabled(path string) (document, rng bool) {
	m := a.serverFor(path)
	if m == nil {
		return false, false
	}
	c := m.Current()
	if c == nil {
		return false, false
	}
	f, ok := c.(formatClient)
	if !ok {
		return false, false
	}
	return f.HasDocumentFormatting(), f.HasDocumentRangeFormatting()
}

// hasSelection reports whether the editor holds selected text, as
// Format Selection needs.
func hasSelection(st *editor.State) bool {
	for _, s := range st.Selections() {
		if s.Anchor != s.Head {
			return true
		}
	}
	return false
}

// formatDocument formats the focused editor's whole document.
func (a *App) formatDocument() {
	st := a.activeEditor()
	if st == nil {
		return
	}
	d := a.docs[a.activePath()]
	if d == nil {
		return
	}
	a.formatAt(a.focus, d, st, false)
}

// formatSelection formats the lines the focused editor's selection
// covers, enabled only when the server answers rangeFormatting.
func (a *App) formatSelection() {
	st := a.activeEditor()
	if st == nil {
		return
	}
	d := a.docs[a.activePath()]
	if d == nil {
		return
	}
	a.formatAt(a.focus, d, st, true)
}

// formatAt asks the document's server to format, of the whole document
// or of the lines the selection covers, and applies the edits it
// returns through the buffer, the same path a code action's edits take:
// the undo history folds them into one step and the server hears the
// change.
func (a *App) formatAt(g *group, d *doc, st *editor.State, selection bool) {
	if d.buf == nil {
		return
	}
	a.observeFocusedGroup(g)
	m := a.serverFor(d.path)
	if m == nil {
		a.flash(locales.MsgNoLanguageServerIsConfiguredForThisFile())
		return
	}
	if !a.workspaceTrusted() {
		a.trustAsk = true
		a.afterTrust = func() { a.formatAt(g, d, st, selection) }
		return
	}
	c := m.Current()
	if c == nil {
		a.flash(locales.MsgCodeActionUnavailable())
		return
	}
	f, ok := c.(formatClient)
	if !ok || selection && !f.HasDocumentRangeFormatting() || !selection && !f.HasDocumentFormatting() {
		a.flash(locales.FormattingUnsupported())
		return
	}
	a.cancelNav()
	snap := d.buf.Snapshot()
	opts := formattingOptionsOf(st, d)
	var rng lsp.Range
	if selection {
		rng = selectionRange(snap, st, c.Encoding())
	}
	version := d.buf.Version()
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = locales.FormattingFinding()
	go func() {
		defer cancel()
		if err := m.Sync(ctx, c, d.uri, snap); err != nil {
			a.post.Post(func() {
				if a.gen == gen && a.navSeq == seq {
					a.finding = false
					a.lspFailure(err)
				}
			})
			return
		}
		var edits []lsp.TextEdit
		var err error
		if selection {
			edits, err = f.DocumentRangeFormatting(ctx, d.uri, rng, opts)
		} else {
			edits, err = f.DocumentFormatting(ctx, d.uri, opts)
		}
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq || a.docs[d.path] != d || d.buf.Version() != version {
				return
			}
			a.finding = false
			a.navCancel = nil
			if err != nil {
				a.lspFailure(err)
				return
			}
			if len(edits) == 0 {
				return // the document is already formatted
			}
			bufEdits := lsp.ApplyTextEdits(d.buf.Snapshot(), edits, c.Encoding())
			if len(bufEdits) == 0 {
				return
			}
			d.buf.EditRemote(bufEdits...)
			m.Changed(d.uri, d.buf.Snapshot())
			a.flash(locales.FormattingApplied())
		})
	}()
}

// selectionRange is the LSP range of the lines the editor's selections
// cover, first to last. Range formatting always works on whole lines:
// a selection of part of a line asks for that line's format.
func selectionRange(snap editor.Snapshot, st *editor.State, enc string) lsp.Range {
	start, end := -1, -1
	for _, s := range st.Selections() {
		lo, hi := s.Anchor, s.Head
		if lo > hi {
			lo, hi = hi, lo
		}
		if start < 0 || lo < start {
			start = lo
		}
		if hi > end {
			end = hi
		}
	}
	if start < 0 {
		return lsp.Range{}
	}
	sp, ep := snap.Pos(start), snap.Pos(end)
	return lsp.Range{
		Start: lsp.Position{Line: sp.Line, Character: 0},
		End:   lsp.Position{Line: ep.Line, Character: lsp.ColumnToLSP(snap.Line(ep.Line), len(snap.Line(ep.Line)), enc)},
	}
}

// formattingOptionsOf is the indentation the formatting request
// carries: the editor's tab size, and whether its indenting inserts
// spaces. With no user preference these are the document language's, as
// the editor computes them.
func formattingOptionsOf(st *editor.State, d *doc) lsp.FormattingOptions {
	opts := lsp.FormattingOptions{TabSize: 4, InsertSpaces: true}
	if lang := d.buf.Language(); lang != nil {
		if lang.TabSize > 0 {
			opts.TabSize = lang.TabSize
		}
		opts.InsertSpaces = !lang.HardTabs
	}
	if st.TabSize > 0 {
		opts.TabSize = st.TabSize
	}
	switch st.Indent {
	case editor.IndentTabs:
		opts.InsertSpaces = false
	case editor.IndentSpaces:
		opts.InsertSpaces = true
	}
	return opts
}
