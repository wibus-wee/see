package app

import (
	"context"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
)

// foldingRangeClient is a language client that answers folding range
// requests.
type foldingRangeClient interface {
	HasFoldingRange() bool
	FoldingRanges(ctx context.Context, uri string) ([]lsp.FoldingRange, error)
}

// foldSettle is how long edits must rest before folds refresh; a new
// range set lands between them rather than under the caret.
const foldSettle = 150 * time.Millisecond

// syncFoldingRanges asks the document's server for its folding ranges
// when the buffer's version has moved past the one they are of. It runs
// every frame the document builds, as syncInlayHints does; a reply that
// arrives after the buffer moved on is dropped, and one that fails
// leaves the last good ranges in place.
func (a *App) syncFoldingRanges(d *doc, st *editor.State) {
	m := a.serverFor(d.path)
	served := m != nil && a.cfg.IsTrusted(a.root)
	if !served || d.buf == nil || d.lossy || d.plain || st == nil || a.wsCtx == nil {
		// Clear only the lane the requests filled: foldSeq > 0 marks it.
		if d.foldSeq > 0 {
			a.clearFoldingRanges(d)
		}
		return
	}
	version := d.buf.Version()
	if version == d.foldVersion && d.foldSeq > 0 {
		return // fetched, fetching, or already tried for this text
	}
	d.foldSeq++
	if d.foldTimer != nil {
		d.foldTimer.Stop()
	}
	if d.foldCancel != nil {
		d.foldCancel()
	}
	d.foldVersion = version
	seq, gen := d.foldSeq, a.gen
	ctx, cancel := context.WithCancel(a.wsCtx)
	d.foldCancel = cancel
	snap, docURI := d.buf.Snapshot(), d.uri
	d.foldTimer = time.AfterFunc(foldSettle, func() {
		a.post.Post(func() {
			if ctx.Err() != nil || a.gen != gen || a.docs[d.path] != d || d.foldSeq != seq || d.buf.Version() != version {
				return
			}
			go a.fetchFoldingRanges(ctx, d, m, snap, docURI, seq, gen)
		})
	})
}

// fetchFoldingRanges requests the ranges and posts them, all guards
// holding: same workspace generation, document, request and text.
func (a *App) fetchFoldingRanges(ctx context.Context, d *doc, m *langserver.Server, snap editor.Snapshot, docURI string, seq, gen int) {
	defer func() {
		a.post.Post(func() {
			if a.docs[d.path] == d && d.foldSeq == seq {
				d.foldCancel = nil
			}
		})
	}()
	c, err := m.Get(ctx)
	if err != nil {
		return
	}
	foldClient, ok := c.(foldingRangeClient)
	if !ok || !foldClient.HasFoldingRange() {
		return
	}
	if err := m.Sync(ctx, c, docURI, snap); err != nil {
		return
	}
	ranges, err := foldClient.FoldingRanges(ctx, docURI)
	if err != nil {
		return // a failed range is invisible, not an error the reader asked for
	}
	a.post.Post(func() {
		if a.gen != gen || a.docs[d.path] != d || d.foldSeq != seq || d.buf == nil || d.buf.Version() != snap.Version() {
			return // the text the ranges describe is gone
		}
		d.foldRanges = foldLines(ranges, snap)
		d.foldBufVer = snap.Version()
		a.upgradeLanguage(d, m, c) // the gutter folds by them once wrapped
	})
}

// foldLines indexes the server's ranges by their start line, keeping
// the widest when starts collide, as a line folds to one end. Ranges
// that fold nothing — their end is their start — are left out.
func foldLines(ranges []lsp.FoldingRange, snap editor.Snapshot) map[int]int {
	out := make(map[int]int, len(ranges))
	for _, r := range ranges {
		start, end := r.StartLine, r.EndLine
		if start < 0 || end <= start {
			continue
		}
		if start >= snap.LineCount() {
			continue
		}
		if end > snap.LineCount()-1 {
			end = snap.LineCount() - 1
		}
		if cur, ok := out[start]; !ok || end > cur {
			out[start] = end
		}
	}
	return out
}

// clearFoldingRanges drops a document's folding ranges and whatever
// was fetching more, as clearDocumentLinks does its lane, and gives
// the gutter back to the language it wrapped — the indentation guess
// the server was taking over.
func (a *App) clearFoldingRanges(d *doc) {
	d.foldSeq = 0
	if d.foldTimer != nil {
		d.foldTimer.Stop()
		d.foldTimer = nil
	}
	if d.foldCancel != nil {
		d.foldCancel()
		d.foldCancel = nil
	}
	d.foldRanges, d.foldVersion, d.foldBufVer = nil, 0, 0
	if d.wrappedLang != nil && d.buf != nil && d.buf.Language() == d.wrappedLang {
		d.buf.SetLanguage(d.baseLang)
	}
	d.foldFolder, d.baseLang, d.wrappedLang, d.wrappedFold = nil, nil, nil, false
	d.wrappedInlays = false
}

// foldFolder is a buffer's highlighter answering FoldEnd from the
// document's folding ranges, which fetchFoldingRanges keeps current:
// the gutter's markers and the fold commands take what the server says
// folds rather than what indentation guesses. Its other methods are the
// base highlighter's, so tokens — the language's own or the semantic
// layer's — are untouched.
type foldFolder struct {
	base editor.Highlighter
	doc  *doc
}

func (f *foldFolder) Edit(old, new editor.Snapshot, changes []editor.Change) {
	f.base.Edit(old, new, changes)
}

func (f *foldFolder) Highlight(last int, budget time.Duration) bool {
	return f.base.Highlight(last, budget)
}

func (f *foldFolder) Tokens(i int, text string, dst []editor.Token) ([]editor.Token, bool) {
	return f.base.Tokens(i, text, dst)
}

// FoldEnd is the last line folding line hides, from the ranges the
// server gave; a line none start on folds by the base's own Folder
// when it has one — a highlighter that folds by its parse, as a
// tree-sitter's, loses nothing — else by nothing, as the server's word
// stands over indentation.
func (f *foldFolder) FoldEnd(line int) (int, bool) {
	d := f.doc
	if d != nil && d.buf != nil && d.foldRanges != nil && d.buf.Version() == d.foldBufVer {
		if end, ok := d.foldRanges[line]; ok {
			return end, true
		}
	}
	if b, ok := f.base.(editor.Folder); ok {
		return b.FoldEnd(line)
	}
	return 0, false
}
