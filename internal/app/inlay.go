package app

import (
	"context"
	"strings"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
)

// inlayHintClient is a language client that answers inlay hint requests.
type inlayHintClient interface {
	HasInlayHints() bool
	InlayHints(ctx context.Context, uri string, r lsp.Range) ([]lsp.InlayHint, error)
}

// inlaySettle is how long scrolling or editing must stop before hints refresh.
const inlaySettle = 100 * time.Millisecond

// syncInlayHints refreshes the visible range after scrolling or editing settles.
// Cached hints follow edits through the highlighter until a fresh reply arrives.
func (a *App) syncInlayHints(d *doc, st *editor.State) {
	if st != nil {
		// Synchronize the highlighter before inspecting or replaying cached offsets.
		st.CursorPos()
	}
	if d.buf != nil && d.inlayKey.first >= 0 && d.inlayVersion != d.buf.Version() {
		a.clearInlayHints(d)
	}
	m := a.serverFor(d.path)
	served := m != nil && a.cfg.IsTrusted(a.root)
	if !served || d.buf == nil || d.lossy || d.plain || st == nil || a.wsCtx == nil {
		// Clear only the lane the requests filled: inlayKey set marks it,
		// and nothing unserved reaches here having asked for hints.
		if d.inlayKey.first >= 0 {
			a.clearInlayHints(d)
		}
		return
	}
	version := d.buf.Version()
	first, last := st.VisibleLines()
	if last < first {
		last = first
	}
	first = max(first, 0)
	last = min(last, d.buf.LineCount()-1)
	key := inlayKey{first: first, last: last, version: version}
	if key == d.inlayKey {
		return // covered: fetched, fetching, or already tried for this view and text
	}
	d.inlaySeq++
	if d.inlayTimer != nil {
		d.inlayTimer.Stop()
	}
	if d.inlayCancel != nil {
		d.inlayCancel()
	}
	d.inlayKey = key
	seq, gen := d.inlaySeq, a.gen
	ctx, cancel := context.WithCancel(a.wsCtx)
	d.inlayCancel = cancel
	snap, docURI := d.buf.Snapshot(), d.uri
	d.inlayTimer = time.AfterFunc(inlaySettle, func() {
		a.post.Post(func() {
			if ctx.Err() != nil || a.gen != gen || a.docs[d.path] != d || d.inlaySeq != seq || d.buf.Version() != version {
				return
			}
			go a.fetchInlayHints(ctx, d, m, snap, docURI, first, last, seq, gen)
		})
	})
}

// fetchInlayHints requests the hints and posts them, all guards still
// holding: same workspace generation, document, request and text.
func (a *App) fetchInlayHints(ctx context.Context, d *doc, m *langserver.Server, snap editor.Snapshot, docURI string, first, last, seq, gen int) {
	defer func() {
		a.post.Post(func() {
			if a.docs[d.path] == d && d.inlaySeq == seq {
				d.inlayCancel = nil
			}
		})
	}()
	c, err := m.Get(ctx)
	if err != nil {
		return
	}
	hintsClient, ok := c.(inlayHintClient)
	if !ok || !hintsClient.HasInlayHints() {
		return
	}
	if err := m.Sync(ctx, c, docURI, snap); err != nil {
		return
	}
	enc := c.Encoding()
	r := lsp.Range{
		Start: lsp.Position{Line: first, Character: 0},
		End:   lsp.Position{Line: last, Character: lsp.ColumnToLSP(snap.Line(last), len(snap.Line(last)), enc)},
	}
	hints, err := hintsClient.InlayHints(ctx, docURI, r)
	if err != nil {
		return // a failed hint is invisible, not an error the reader asked for
	}
	a.post.Post(func() {
		if a.gen != gen || a.docs[d.path] != d || d.inlaySeq != seq || d.buf == nil || d.buf.Version() != snap.Version() {
			return
		}
		if !a.cfg.IsTrusted(a.root) {
			return
		}
		a.upgradeLanguage(d, m, c)
		d.languageInlays, d.parameterInlays = toInlayLanes(hints, snap, enc)
		d.inlayVersion = snap.Version()
	})
}

// inlayKey is what a request covers; a settled frame for it is not asked again.
type inlayKey struct {
	first, last int
	version     uint64
}

// toInlayLanes separates parameter hints from type and unknown hints.
func toInlayLanes(hints []lsp.InlayHint, snap editor.Snapshot, enc string) (language, parameters []editor.Inlay) {
	for _, h := range hints {
		line := h.Position.Line
		if line < 0 || line >= snap.LineCount() {
			continue
		}
		text := strings.TrimRight(h.Label, "\n")
		if text == "" {
			continue
		}
		if h.PaddingLeft {
			text = " " + text
		}
		if h.PaddingRight {
			text += " "
		}
		inlay := editor.Inlay{
			Offset: snap.LineStart(line) + lsp.ColumnFromLSP(snap.Line(line), h.Position.Character, enc),
			Text:   text,
		}
		if h.Kind == 2 {
			parameters = append(parameters, inlay)
		} else {
			language = append(language, inlay)
		}
	}
	return language, parameters
}

func (a *App) toggleTypeHints() {
	a.cfg.HideTypeHints = !a.cfg.HideTypeHints
	a.settingsChanged()
}

// clearInlayHints drops a document's hints and whatever was fetching more.
func (a *App) clearInlayHints(d *doc) {
	d.inlaySeq++
	if d.inlayTimer != nil {
		d.inlayTimer.Stop()
		d.inlayTimer = nil
	}
	if d.inlayCancel != nil {
		d.inlayCancel()
		d.inlayCancel = nil
	}
	d.languageInlays, d.parameterInlays, d.inlayKey = nil, nil, inlayKey{first: -1}
	if d.buf != nil {
		d.inlayVersion = d.buf.Version()
	}
}
