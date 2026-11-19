package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/locales"
)

// pinDebounce lets typing settle before the pin asks for symbols again.
const pinDebounce = 350 * time.Millisecond

// pinMinLines is the shortest range a scope is worth reporting: two-line
// func literals and fields would keep the row flickering while it says
// nothing a glance at the gutter would not.
const pinMinLines = 8

// pinScope is one declaration of the row's path, outer scopes first.
type pinScope struct {
	index int          // the symbol's place in the flat list
	pos   lsp.Position // where a click puts the caret
	hops  int          // collapsed levels between this scope and the next inner one
}

// readingPin keeps, per group, the symbols its row was drawn from, the
// request on its way and the row's last answer, as outlineState does for
// the outline.
type readingPin struct {
	requested outlineKey
	seq       int
	cancel    context.CancelFunc
	loading   bool
	symbols   []lsp.DocumentSymbol
	enc       string
	first     int        // the top line the last answer was for
	shown     int        // the innermost scope it named, -1 when the row hid
	path      []pinScope // reusable buffer of the last answer
}

// pinScopes is the path to the scope of line, as pinScopes of symbols,
// outer first and the innermost last; nil when no scope is worth a row.
// Among the symbols containing line it keeps the deepest one at least
// pinMinLines long, the one the row names, with one enclosing level
// above it. Deeper nesting collapses to an ellipsis in front, the hops
// of the outermost scope shown counting the levels it stands for.
//
// Ranges are compared by line alone: symbols share the flat list the
// outline shows, whose nesting is by line, so a character-exact compare
// cannot disagree with it.
func pinScopes(buf []pinScope, symbols []lsp.DocumentSymbol, line int) []pinScope {
	buf = buf[:0]
	if len(symbols) == 0 {
		return nil
	}
	inner, innerDepth := -1, -1
	for i, s := range symbols {
		if s.Range.Start.Line > line || s.Range.End.Line <= line || s.Range.End.Line-s.Range.Start.Line < pinMinLines {
			continue
		}
		if s.Depth > innerDepth {
			inner, innerDepth = i, s.Depth
		}
	}
	if inner < 0 {
		return nil
	}
	// The ancestors of inner are the symbols before it, nearest last,
	// each shallower than the one before it; the row keeps the nearest
	// of them, the rest of the nesting an ellipsis in front of it.
	depth := symbols[inner].Depth
	for i := inner - 1; i >= 0 && depth > 0; i-- {
		s := symbols[i]
		if s.Depth >= depth {
			continue
		}
		depth = s.Depth
		if s.Range.End.Line-s.Range.Start.Line >= pinMinLines {
			buf = append(buf, pinScope{index: i, pos: s.Position, hops: symbols[inner].Depth - 1})
			break
		}
	}
	return append(buf, pinScope{index: inner, pos: symbols[inner].Position})
}

// refreshPin asks for the symbols of g's document when it, its text or
// its server changed. When the outline already shows them, as it does
// for the focused editor whose jump bar asked, the pin answers from them
// and asks nothing.
func (a *App) refreshPin(g *group, d *doc) {
	p := &g.pin
	o := &a.outline
	if o.shown.path == d.path && o.shown.doc == d && len(o.symbols) > 0 && (o.shown.version == 0 || d.buf == nil || o.shown.version == d.buf.Version()) {
		p.cancelRequest()
		p.requested = outlineKey{path: d.path, doc: d, server: o.shown.server}
		if d.buf != nil {
			p.requested.version = d.buf.Version()
		}
		p.symbols, p.enc, p.loading = o.symbols, o.enc, false
		return
	}
	key := outlineKey{path: d.path, doc: d}
	if d.buf != nil {
		key.version = d.buf.Version()
		key.server = a.serverFor(d.path)
	}
	if key == p.requested {
		return
	}
	old := p.requested
	p.cancelRequest()
	p.requested, p.seq = key, p.seq+1
	if old.doc != d || old.server != key.server {
		p.symbols = nil
	}
	// Asking would start the server: only a trusted workspace runs one,
	// as refreshOutline's check does.
	if d.buf == nil || key.server == nil || a.wsCtx == nil || !a.cfg.IsTrusted(a.root) {
		p.loading = false
		return
	}
	m, snap, docURI, gen, seq := key.server, d.buf.Snapshot(), d.uri, a.gen, p.seq
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	p.cancel, p.loading = cancel, true
	unsupported := outlineText("fileSymbolsUnsupported")
	go func() {
		defer cancel()
		var symbols []lsp.DocumentSymbol
		var enc string
		err := ctx.Err()
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(pinDebounce):
		}
		var client langserver.Client
		if err == nil {
			client, err = m.Get(ctx)
		}
		if err == nil {
			err = m.Sync(ctx, client, docURI, snap)
		}
		if err == nil {
			enc = client.Encoding()
			if provider, ok := client.(interface {
				DocumentSymbols(context.Context, string) ([]lsp.DocumentSymbol, error)
			}); ok {
				symbols, err = provider.DocumentSymbols(ctx, docURI)
			} else {
				err = errors.New(unsupported)
			}
		}
		a.post.Post(func() {
			if a.gen != gen || p.seq != seq || p.requested != key || errors.Is(err, context.Canceled) {
				return
			}
			p.cancel, p.loading = nil, false
			if err != nil {
				p.symbols = nil
				return
			}
			p.symbols, p.enc = symbols, enc
		})
	}()
}

// cancelRequest drops the pin's request on its way.
func (p *readingPin) cancelRequest() {
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
}

// readingPinRow is the row above g's editor naming the declaration the
// viewport is inside, or nothing when there is no answer to give: at the
// top of a scope, in a flat list, before the first request answered.
func (a *App) readingPinRow(c *ui.Context, g *group, d *doc, st *editor.State) {
	if a.cfg.HideReadingPin {
		return
	}
	a.refreshPin(g, d)
	p := &g.pin
	if p.loading || p.requested.doc != d {
		return
	}
	first, _ := st.VisibleLines()
	p.path = pinScopes(p.path, p.symbols, first)
	// With the file's first line at the top nothing has scrolled off,
	// and a scope whose own line is still at the top is not yet above
	// the viewport: the row falls back to its parent, or hides. One row
	// of hysteresis while the row shows keeps a scroll back to the
	// boundary from flickering it.
	boundary := first
	if first > 0 && p.shown >= 0 && first <= p.first {
		boundary = first + 1
	}
	path := p.path
	for len(path) > 0 && p.symbols[path[len(path)-1].index].Range.Start.Line >= boundary {
		path = path[:len(path)-1]
	}
	if len(path) == 0 {
		p.shown, p.first = -1, first
		p.path = p.path[:0]
		return
	}
	p.first, p.shown = first, path[len(path)-1].index
	ui.Row(c).FillWidth().Height(22).Padding(0, 10).Gap(4).AlignItems(ui.Center).
		Background(colors(c).Editor).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).
		Label(locales.ReadingPositionPin()).Children(func() {
		if path[0].hops > 0 {
			ui.Text(c, "…").FontSize(12).TextColor(colors(c).Dim).SingleLine()
		}
		for k, scope := range path {
			if k > 0 {
				symbolIcon(c, icChevron, 9, colors(c).Dim)
			}
			s := p.symbols[scope.index]
			text := colors(c).Muted
			if k == len(path)-1 {
				text = colors(c).Text
			}
			b := ui.ButtonBase(c.Key(fmt.Sprint("pin-", scope.index))).Label(s.Name).Padding(1, 3).Radius(4).Shrink(0)
			if b.Hovered() || b.Pressed() {
				b.Background(colors(c).Pill)
			}
			b.Children(func() { symbolCrumbContent(c, s, text, 12) })
			pos := scope.pos
			b.OnClick(func() { a.openPinSymbol(g, pos) })
		}
	})
}

// openPinSymbol goes to the declaration the row's segment names, as a
// jump bar symbol's click does.
func (a *App) openPinSymbol(g *group, pos lsp.Position) {
	p := &g.pin
	if p.requested.path == "" {
		return
	}
	a.focus = g
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	a.openIn(g, p.requested.path, goTo{lsp: &pos, enc: p.enc})
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}
