package app

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/locales"
)

// pinDebounce lets typing settle before the pin asks for symbols again.
const pinDebounce = 350 * time.Millisecond

const maxStickyHeaders = 3

type readingPin struct {
	requested outlineKey
	seq       int
	cancel    context.CancelFunc
	loading   bool
	symbols   []lsp.DocumentSymbol
	enc       string
}

func stickyKind(kind int) bool {
	switch kind {
	case 2, 3, 5, 6, 9, 10, 11, 12, 23: // module, namespace, class, method, constructor, enum, interface, function, struct
		return true
	default:
		return false
	}
}

func enclosesSticky(outer, inner lsp.DocumentSymbol) bool {
	return outer.Position.Line < inner.Position.Line &&
		!positionBefore(inner.Range.Start, outer.Range.Start) &&
		!positionBefore(outer.Range.End, inner.Range.End)
}

// Each slot pins declarations above that slot only if their body reaches
// below it. Check ranges rather than Depth so flat LSP results nest too.
func stickyScopes(symbols []lsp.DocumentSymbol, lines []int) []lsp.DocumentSymbol {
	var headers []lsp.DocumentSymbol
	for slot := 0; slot+1 < len(lines); slot++ {
		best := -1
		for i, s := range symbols {
			if !stickyKind(s.Kind) || s.Position.Line < 0 || s.Position.Line >= lines[slot] ||
				!positionBefore(lsp.Position{Line: lines[slot+1]}, s.Range.End) {
				continue
			}
			if slot > 0 && !enclosesSticky(headers[slot-1], s) {
				continue
			}
			if best < 0 || slot+2 < len(lines) && enclosesSticky(s, symbols[best]) ||
				slot+2 == len(lines) && enclosesSticky(symbols[best], s) {
				best = i
			}
		}
		if best < 0 {
			break
		}
		headers = append(headers, symbols[best])
	}
	return headers
}

// refreshPin asks for the symbols of g's document when it, its text or
// its server changed. When the outline already shows them, as it does
// for the focused editor whose jump bar asked, the pin answers from them
// and asks nothing.
func (a *App) refreshPin(g *group, d *doc) {
	p := &g.pin
	o := &a.outline
	key := outlineKey{path: d.path, doc: d, version: d.buf.Version(), server: a.serverFor(d.path)}
	if !a.workspaceTrusted() {
		p.cancelRequest()
		p.requested, p.symbols, p.loading = outlineKey{}, nil, false
		return
	}
	if o.shown == key && o.err == nil {
		p.cancelRequest()
		p.requested = key
		p.symbols, p.enc, p.loading = o.symbols, o.enc, false
		return
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
	if key.server == nil || a.wsCtx == nil {
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
			if a.gen != gen || p.seq != seq || p.requested != key || !p.loading || errors.Is(err, context.Canceled) {
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

func (a *App) stickyScopeHeaders(c *ui.Context, g *group, d *doc, st *editor.State) {
	if a.cfg.HideReadingPin || st.ViewportHeight() == 0 {
		return
	}
	a.refreshPin(g, d)
	p := &g.pin
	if p.loading || len(p.symbols) == 0 || st.ScrollY() <= 0 {
		return
	}
	font := st.Font
	if font.Family == "" {
		font.Family = "monospace"
	}
	if font.Size <= 0 {
		font.Size = 13
	}
	lineHeight := st.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	height := font.Size * lineHeight
	count := st.Buffer.LineCount()
	slots := min(maxStickyHeaders, int(st.ViewportHeight()/float64(height))-1)
	if slots < 1 {
		return
	}
	var lines [maxStickyHeaders + 1]int
	for i := 0; i <= slots; i++ {
		y := st.ScrollY() + float64(float32(i)*height)
		lines[i] = sort.Search(count, func(line int) bool { return st.LineTop(line+1) > y })
	}
	headers := stickyScopes(p.symbols, lines[:slots+1])
	if len(headers) == 0 {
		return
	}
	charWidth, _ := c.MeasureText(0, ui.Span{Text: "0", Font: font.Family, Size: font.Size, Weight: font.Weight})
	numberWidth := float32(0)
	if st.LineNumbers != editor.LineNumbersOff {
		numberWidth = float32(max(3, len(fmt.Sprint(count)))) * charWidth
	}
	// Match MyGo's number, change and fold gutters; leave its scrollbar reachable.
	textX := numberWidth + 14
	if numberWidth > 0 {
		textX += 1.5 * charWidth
	}
	if !st.NoFolding {
		textX += 0.8 * height
	}
	textX = float32(math.Ceil(float64(textX)))
	numberRight := float32(0)
	if numberWidth > 0 {
		numberRight = 4 + numberWidth
	}
	right := float32(0)
	if !st.NoScrollbars {
		right = st.Scrollbar.TrackWidth
		if right == 0 {
			right = c.Theme().ScrollbarStyle.TrackWidth
		}
		if right == 0 {
			right = 10
		}
	}
	palette := colors(c)
	edTheme := st.Theme
	if edTheme == nil {
		edTheme = editor.DefaultTheme(c.Theme())
	}
	background := edTheme.Background.Mix(palette.TabBar, 0.45)
	lang := st.Buffer.Language()
	tabSize := st.TabSize
	if tabSize <= 0 && lang != nil {
		tabSize = lang.TabSize
	}
	if tabSize <= 0 {
		tabSize = 4
	}
	ui.Column(c).Key("sticky scopes").Absolute().Top(0).Left(0).Right(right).ZIndex(-1).
		// Keep pointer presses on the buttons; wheel events still bubble to the editor.
		HandleInput(func(ui.InputEvent) bool { return false }).
		Label(locales.ReadingPositionPin()).Children(func() {
		for _, s := range headers {
			line := s.Position.Line
			source := stickySource(st.Buffer.Line(line), tabSize)
			b := ui.ButtonBase(c.Key(line)).Row().FillWidth().Height(height).Shrink(0).
				Justify(ui.Start).AlignItems(ui.Center).Clip().Cursor(ui.CursorPointer).Label(strings.TrimSpace(source)).
				Background(background).BorderWidth(0, 0, 1, 0).BorderColor(palette.Separator)
			if b.Hovered() || b.Pressed() {
				b.Background(background.Mix(palette.Text, 0.07))
			}
			b.Children(func() {
				if numberRight > 0 {
					ui.Text(c, fmt.Sprint(line+1)).Font(font.Family).FontSize(font.Size).FontWeight(font.Weight).
						TextColor(edTheme.LineNumber).Width(numberRight).TextAlign(ui.End).SingleLine().Shrink(0)
				}
				ui.Box(c).Width(textX - numberRight).Shrink(0)
				ui.RichText(c, stickySpans(source, lang, edTheme)...).Font(font.Family).FontSize(font.Size).
					FontWeight(font.Weight).FontFeatures(font.Features).SingleLine().Ellipsis("…").Shrink(1)
			})
			b.OnClick(func() { a.openPinSymbol(g, s.Position) })
		}
	})
}

func stickySource(source string, tabSize int) string {
	if !strings.ContainsRune(source, '\t') {
		return source
	}
	var b strings.Builder
	col := 0
	for _, r := range source {
		if r == '\t' {
			n := tabSize - col%tabSize
			b.WriteString(strings.Repeat(" ", n))
			col += n
		} else {
			b.WriteRune(r)
			col++
		}
	}
	return b.String()
}

func stickySpans(source string, lang *editor.Language, theme *editor.Theme) []ui.Span {
	var spans []ui.Span
	start := 0
	if lang != nil && lang.Tokenizer != nil {
		tokens, _ := lang.Tokenizer.Tokenize(source, 0, nil)
		for _, token := range tokens {
			end := min(token.End, len(source))
			if end > start {
				spans = append(spans, ui.Span{Text: source[start:end], Color: theme.TokenColor(token.Kind)})
				start = end
			}
		}
	}
	if start < len(source) {
		spans = append(spans, ui.Span{Text: source[start:], Color: theme.Text})
	}
	return spans
}

// openPinSymbol goes to the declaration the row's segment names, as a
// jump bar symbol's click does.
func (a *App) openPinSymbol(g *group, pos lsp.Position) {
	p := &g.pin
	if p.requested.path == "" {
		return
	}
	a.observeFocusedGroup(g)
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	a.openIn(g, p.requested.path, goTo{lsp: &pos, enc: p.enc})
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}
