package app

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/workspace"
)

var (
	errSelectionRangesUnsupported = errors.New("selection ranges unsupported")
	errSelectionRangesCount       = errors.New("selection range count mismatch")
)

// selectionRangeClient is a language client that answers selection
// range requests, as gopls does: the syntax nodes a position's
// selection grows through.
type selectionRangeClient interface {
	HasSelectionRanges() bool
	SelectionRanges(ctx context.Context, uri string, positions []lsp.Position) ([]*lsp.SelectionRange, error)
}

// selectionDocument returns the editor of the group's active document
// that selection ranges work on: a text buffer, as editAllInScope
// wants, but not necessarily an editable one — expanding a read-only
// view is harmless.
func (a *App) selectionDocumentFor(g *group, d *doc, st *editor.State) (*doc, *editor.State) {
	if g == nil || a.layout == nil || a.layout.find(g) == nil || d == nil || st == nil ||
		d.path == "" || g.active != d.path || g.eds[d.path] != st || a.docs[d.path] != d ||
		d.buf == nil || st.Buffer != d.buf ||
		d.untitled || d.loading || d.err != nil || d.gone || d.kind != workspace.Text ||
		d.plain || d.lossy || d.video || d.image != nil || g.hexPreview[d.path] ||
		isMarkdown(d.path) || isMarkdownTab(g.active) {
		return nil, nil
	}
	return d, st
}

// expandSelection grows each selection one level. It asks the language
// server for the selection ranges of the cursors and steps to the range
// covering the current selection; without the capability or on a failed
// answer, the editor's own step — word, string, brackets, line — takes
// over. Selections the answer grows push onto a.selStack, for
// shrinkSelection to walk back through.
func (a *App) expandSelection() {
	g := a.focus
	d := a.docs[a.activePath()]
	d, st := a.selectionDocumentFor(g, d, a.activeEditor())
	if st == nil {
		return
	}
	m := a.serverFor(d.path)
	if m == nil || !a.workspaceTrusted() || a.wsCtx == nil {
		st.Run(editor.SelectLarger)
		return
	}
	snap := d.buf.Snapshot()
	sels := st.Selections()
	docURI, path := d.uri, d.path
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	a.selCancel = cancel
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		var expanded []editor.Selection
		if err == nil {
			provider, ok := client.(selectionRangeClient)
			if !ok || !provider.HasSelectionRanges() {
				err = errSelectionRangesUnsupported
			} else if err = m.Sync(ctx, client, docURI, snap); err == nil {
				positions := make([]lsp.Position, len(sels))
				for i, s := range sels {
					positions[i] = scopeLSPPosition(snap, s.End(), client.Encoding())
				}
				var chains []*lsp.SelectionRange
				chains, err = provider.SelectionRanges(ctx, docURI, positions)
				if err == nil {
					expanded, err = selectionExpansions(snap, sels, chains, client.Encoding())
				}
			}
		}
		a.post.Post(func() {
			a.selCancel = nil
			if err != nil || a.docs[path] != d || d.buf == nil || d.buf.Version() != snap.Version() ||
				!slices.Equal(st.Selections(), sels) {
				// A failure leaves the editor's own step, as does a
				// selection or text that moved since the request.
				if err == nil || a.docs[path] == d {
					st.Run(editor.SelectLarger)
				}
				return
			}
			a.selStack = append(a.selStack, sels...)
			st.SetSelections(expanded...)
		})
	}()
}

// shrinkSelection takes back the selections Expand Selection grew,
// one stacked selection a cursor, last grown first taken back. With
// none, the editor's Select Smaller restores its own expansions.
func (a *App) shrinkSelection() {
	st := a.activeEditor()
	if st == nil {
		return
	}
	sels := st.Selections()
	// The stack holds each expand's selections in cursor order, so the
	// last one a cursor restores it.
	n := min(len(a.selStack), len(sels))
	base := len(a.selStack) - n
	for i := 0; i < n; i++ {
		sels[i] = a.selStack[base+i]
	}
	a.selStack = a.selStack[:base]
	if !slices.Equal(sels, st.Selections()) {
		st.SetSelections(sels...)
		return
	}
	st.Run(editor.SelectSmaller)
}

// selectionExpansions maps every selection's chain to the first range
// covering it: the next step out of what it already selects, as LSP's
// selectionRange grows a selection from where it is. A chain is the
// server's view of where the position is; ranges past the current
// selection never apply, which keeps a stale answer from shrinking a
// selection the user grew.
func selectionExpansions(snap editor.Snapshot, sels []editor.Selection, chains []*lsp.SelectionRange, encoding string) ([]editor.Selection, error) {
	if len(chains) != len(sels) {
		return nil, errSelectionRangesCount
	}
	out := make([]editor.Selection, len(sels))
	for i, s := range sels {
		next := s
		for _, r := range lsp.RangesOf(chains[i]) {
			start, ok1 := scopeOffset(snap, r.Start, encoding)
			end, ok2 := scopeOffset(snap, r.End, encoding)
			if !ok1 || !ok2 {
				break // an unconvertible range ends the walk, as a missing one does
			}
			if start <= s.Anchor && end >= s.Head && (start < s.Anchor || end > s.Head) {
				next = editor.Selection{Anchor: start, Head: end}
				break
			}
		}
		out[i] = next
	}
	return out, nil
}
