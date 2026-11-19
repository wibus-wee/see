package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/locales"
)

// signatureState follows the caret for signature help: the call the
// caret is inside and what the language server last answered for it.
// Nothing it owns is an error state — no server, no support, a slow
// reply or a parse failure all leave the strip hidden, because the
// reader did not ask a question they can see.
type signatureState struct {
	seq       uint64
	cancel    context.CancelFunc
	timer     *time.Timer
	doc       *doc // the document callStart is of
	callStart int  // byte offset of the open paren of the call, -1 outside one
	offset    int  // the caret byte offset the request was last made or scheduled for
	caretLine int  // the line the strip anchors under
	help      *lsp.SignatureHelp
	version   uint64 // the buffer version the help shown or asked is of
	asked     bool   // help or a request exists for this call already
	active    int    // the signature shown of help.Signatures
	dismissed bool   // Escape closed it here until the caret leaves the call
	triggered bool   // Control-Shift+Space asked for it now
}

// signatureClients answer textDocument/signatureHelp.
type signatureClient interface {
	HasSignatureHelp() bool
	SignatureHelp(context.Context, string, lsp.Position) (*lsp.SignatureHelp, error)
}

// signatureDelay is how long the caret must rest inside a call before
// its signature is asked for: a reader clicking through a file should
// not pay a request per stop, and a fast reader should see nothing.
const signatureDelay = 150 * time.Millisecond

// signatureOff is callStart's value outside an argument list.
const signatureOff = -1

// syncSignature watches the caret of d's editor: inside a call's
// argument list it asks for signature help, outside one it closes the
// strip. The editor reports no caret move, so this runs once a frame
// and costs a bracket scan over the text above the caret.
func (a *App) syncSignature(g *group, d *doc, st *editor.State) {
	s := &a.signature
	if s.doc != nil && s.doc != d {
		a.clearSignature()
	}
	if !a.signatureEnabled() || d.buf == nil {
		if s.doc != nil {
			a.clearSignature()
		}
		return
	}
	offset := st.Cursor()
	start := callStartAt(d.buf, offset)
	if start == signatureOff {
		a.clearSignature()
		return
	}
	s.caretLine = d.buf.Pos(offset).Line
	version := d.buf.Version()
	if s.doc == d && s.callStart == start && s.version == version {
		if s.triggered {
			s.triggered = false
			if s.dismissed {
				s.dismissed = false
			} else if !s.asked && s.timer == nil && s.cancel == nil {
				a.askSignature(d, st)
			}
		}
		return // same call and version: the answer stands
	}
	a.cancelSignature()
	s.doc = d
	s.callStart, s.offset, s.version = start, offset, version
	s.help = nil
	s.active = 0
	s.dismissed = false
	s.asked = false
	if s.triggered {
		s.triggered = false
		a.askSignature(d, st)
		return
	}
	// Always asks once the caret rests; On Trigger waits for
	// Control-Shift+Space, so a new call here does nothing yet.
	if a.cfg.ParameterHelp == "trigger" {
		return
	}
	a.deferSignature(d, st)
}

// deferSignature asks for signature help after the caret rests
// signatureDelay: typing through a file should not pay a request per
// stop. A flight is not cancelled — the server folds an outdated
// position into its own answer — but a new one does not start while it
// runs.
func (a *App) deferSignature(d *doc, st *editor.State) {
	s := &a.signature
	if s.timer != nil {
		s.timer.Stop()
	}
	if s.cancel != nil || s.asked {
		return
	}
	s.seq++
	seq, gen := s.seq, a.gen
	s.timer = time.AfterFunc(signatureDelay, func() {
		a.post.Post(func() {
			if a.gen != gen || s.seq != seq || s.doc != d {
				return
			}
			a.askSignature(d, st)
		})
	})
}

// askSignature requests the signature of the call at s.callStart of d.
// The server marks the active signature and parameter; See shows
// whichever it names.
func (a *App) askSignature(d *doc, st *editor.State) {
	s := &a.signature
	m := a.serverFor(d.path)
	if m == nil || d.buf == nil || !a.cfg.IsTrusted(a.root) {
		return
	}
	if state, _ := m.Status(); state == langserver.Failed {
		return
	}
	if c := m.Current(); c != nil {
		p, ok := c.(signatureClient)
		if !ok || !p.HasSignatureHelp() {
			return
		}
	}
	snap := d.buf.Snapshot()
	s.asked = true
	pos := snap.Pos(min(s.offset, snap.Len()))
	line := snap.Line(pos.Line)
	s.seq++
	seq, gen, version := s.seq, a.gen, snap.Version()
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	s.cancel = cancel
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		if err != nil {
			return
		}
		p, ok := c.(signatureClient)
		if !ok || !p.HasSignatureHelp() {
			return
		}
		if m.Sync(ctx, c, d.uri, snap) != nil {
			return
		}
		at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, c.Encoding())}
		help, err := p.SignatureHelp(ctx, d.uri, at)
		a.post.Post(func() {
			if a.gen != gen || s.seq != seq || s.doc != d || d.buf == nil || d.buf.Version() != version {
				return // the caret moved on or the text changed
			}
			s.cancel = nil
			if err != nil || help == nil || len(help.Signatures) == 0 {
				s.help = nil
				return
			}
			s.help = help
			s.version = version
			s.active = 0
			if help.ActiveSignature != nil && *help.ActiveSignature >= 0 && *help.ActiveSignature < len(help.Signatures) {
				s.active = *help.ActiveSignature
			}
		})
	}()
}

// cancelSignature stops a pending request or debounce without clearing
// what was asked about.
func (a *App) cancelSignature() {
	s := &a.signature
	s.seq++
	if s.cancel != nil {
		s.cancel()
		s.cancel = nil
	}
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
}

// clearSignature hides the strip and forgets the call: the caret left
// the argument list or the document is gone.
func (a *App) clearSignature() {
	a.cancelSignature()
	s := &a.signature
	s.doc = nil
	s.callStart = signatureOff
	s.offset = 0
	s.asked = false
	s.help = nil
	s.active = 0
	s.dismissed = false
}

// showSignatureAtCaret is Control-Shift+Space: signature help at the
// caret right now, whatever Parameter Help is set to.
func (a *App) showSignatureAtCaret() {
	g := a.focus
	if g == nil || g.active == "" {
		return
	}
	d, st := a.docs[g.active], a.activeEditor()
	if d == nil || st == nil || d.buf == nil {
		return
	}
	s := &a.signature
	s.triggered = true
	s.dismissed = false
	a.syncSignature(g, d, st)
}

// dismissSignature hides the strip for the call the caret is inside;
// leaving the call or asking for help again undoes it.
func (a *App) dismissSignature() {
	s := &a.signature
	if s.doc == nil {
		return
	}
	s.dismissed = true
	s.help = nil
	a.cancelSignature()
}

// signatureInput is the editor's view of signature-help keys: Escape
// dismisses, and Up and Down walk overloads. It runs before the
// editor's own keys and reports whether it took the event.
func (a *App) signatureInput(st *editor.State, ev ui.InputEvent) bool {
	s := &a.signature
	if s.doc == nil || s.dismissed {
		return false
	}
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape && (s.help != nil || s.cancel != nil || s.timer != nil) {
		a.dismissSignature()
		return true
	}
	if s.help == nil || len(s.help.Signatures) < 2 {
		return false
	}
	if ev.Kind == ui.InputKeyDown {
		switch ev.Key {
		case ui.KeyUp:
			s.active = (s.active + len(s.help.Signatures) - 1) % len(s.help.Signatures)
			return true
		case ui.KeyDown:
			s.active = (s.active + 1) % len(s.help.Signatures)
			return true
		}
	}
	return false
}

// signatureHelp returns the help to draw for d, or nil: dismissed at
// this call, no answer yet, a different document, or text the answer
// no longer matches.
func (a *App) signatureHelp(d *doc) *lsp.SignatureHelp {
	s := &a.signature
	if s.doc != d || s.dismissed || s.help == nil || len(s.help.Signatures) == 0 ||
		d.buf == nil || d.buf.Version() != s.version {
		return nil
	}
	return s.help
}

// signatureStrip draws the strip under the line holding the caret: the
// active signature in the editor's font at 12 pt, its active parameter
// emphasised, a "1/2" of overloads, and the parameter's one line of
// documentation below when the server sends it. It borrows the peek's
// material — a footnote to the code, not a window.
func (a *App) signatureStrip(c *ui.Context, bounds ui.Rect, st *editor.State, d *doc, gutter float32) {
	help := a.signatureHelp(d)
	if help == nil {
		return
	}
	s := &a.signature
	sig := help.Signatures[min(s.active, len(help.Signatures)-1)]
	fontSize := st.Font.Size
	if fontSize <= 0 {
		fontSize = 13
	}
	lineHeight := st.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	pal := colors(c)
	// The spans of the signature: plain before and after the active
	// parameter's label, emphasised inside it.
	var spans []ui.Span
	label := sig.Label
	active := 0
	if help.ActiveParameter != nil && *help.ActiveParameter >= 0 && *help.ActiveParameter < len(sig.Parameters) {
		active = *help.ActiveParameter
	}
	low, high := -1, -1
	if active < len(sig.Parameters) {
		low, high = sig.Parameters[active].ParamBounds(label)
		if low < 0 || high > len(label) {
			low, high = -1, -1
		}
	}
	font := st.Font.Family
	plain := pal.Text
	muted := pal.Muted
	strong := pal.Folder
	mk := func(text string, weight int, color ui.Color, underline bool) ui.Span {
		return ui.Span{Text: text, Font: font, Size: 12, Weight: weight, Color: color, Underline: underline}
	}
	if low >= 0 {
		if low > 0 {
			spans = append(spans, mk(label[:low], 0, plain, false))
		}
		spans = append(spans, mk(label[low:high], 700, strong, true))
		if high < len(label) {
			spans = append(spans, mk(label[high:], 0, plain, false))
		}
	} else {
		spans = append(spans, mk(label, 0, plain, false))
	}
	top := bounds.Y + float32(st.LineTop(s.caretLine)-st.ScrollY()) + float32(fontSize*lineHeight)
	const stripH, docH, gap = 22, 15, 4
	height := float32(stripH)
	doc := ""
	if active < len(sig.Parameters) {
		doc = oneLine(sig.Parameters[active].Documentation)
	}
	if doc == "" {
		doc = oneLine(sig.Documentation)
	}
	if doc != "" {
		height += docH
	}
	if top+height > bounds.Y+bounds.H-gap {
		above := bounds.Y + float32(st.LineTop(s.caretLine)-st.ScrollY()) - gap - height
		if above >= bounds.Y+gap {
			top = above
		}
	}
	left := bounds.X + gutter + 4
	width := bounds.W - gutter - 8
	ui.Overlay(c, func() {
		col := ui.Column(c).Absolute().Left(left).Top(top).Width(width).Height(height).
			Padding(0, 10).Radius(6).Clip().Background(pal.Popover).Border(1, pal.PopoverEdge).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.08)).Label(locales.SignatureHelp())
		near, far := ui.RGBA(0, 0, 0, 0.08), ui.RGBA(0, 0, 0, 0.16)
		if c.Theme().Dark {
			near, far = ui.RGBA(0, 0, 0, 0.30), ui.RGBA(0, 0, 0, 0.45)
		}
		col.Shadow(0, 12, 32, 0, far).Shadow(0, 1, 3, 0, near)
		col.Children(func() {
			ui.Row(c).FillWidth().Height(stripH).AlignItems(ui.Center).Children(func() {
				ui.RichText(c, spans...).SingleLine().Shrink(1)
				if len(help.Signatures) > 1 {
					ui.Text(c, fmt.Sprintf("%d/%d", s.active+1, len(help.Signatures))).FontSize(11).TextColor(muted).SingleLine().Padding(0, 0, 0, 8)
				}
			})
			if doc != "" {
				ui.Text(c, doc).FontSize(11).TextColor(muted).SingleLine().Shrink(1)
			}
		})
	})
}

// oneLine is the first non-empty line of documentation: the strip has
// one row for it.
func oneLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

// signatureEnabled reports whether signature help is on at all.
func (a *App) signatureEnabled() bool {
	return a.cfg.ParameterHelp != "off"
}

// callStartAt finds the argument list offset is inside: the byte of
// the nearest unmatched '(', '[' or '{' before it, skipping strings and
// comments. It returns signatureOff outside one. The scan runs over the
// text up to the caret with just enough state to skip // comments,
// /* */ comments and quoted runs of ", ' and `.
func callStartAt(buf *editor.Buffer, offset int) int {
	snap := buf.Snapshot()
	offset = min(offset, snap.Len())
	var stack [64]byte
	var at [64]int
	depth := 0
	quote := byte(0)
	escaped := false
	comment := byte(0) // '/' for //, '*' for /* */; 0 outside
	text := snap.Slice(0, offset)
	for i := 0; i < len(text); i++ {
		b := text[i]
		switch {
		case comment == '/':
			if b == '\n' {
				comment = 0
			}
		case comment == '*':
			if b == '/' && i > 0 && text[i-1] == '*' {
				comment = 0
			}
		case quote != 0:
			if escaped {
				escaped = false
			} else if b == '\\' && quote != '`' {
				escaped = true
			} else if b == quote {
				quote = 0
			}
		default:
			switch b {
			case '"', '\'', '`':
				quote = b
			case '/':
				if i+1 < len(text) {
					switch text[i+1] {
					case '/':
						comment = '/'
						i++
					case '*':
						comment = '*'
						i++
					}
				}
			case '(', '[', '{':
				if depth < len(stack) {
					stack[depth], at[depth] = b, i
					depth++
				}
			case ')', ']', '}':
				if depth > 0 {
					depth--
				}
			}
		}
	}
	// The innermost unmatched '(': '[', and '{' match their closers
	// without being calls — an index or a literal inside an argument
	// list still belongs to the '()' enclosing it, and a function body
	// alone is no call.
	for i := depth - 1; i >= 0; i-- {
		if stack[i] == '(' {
			return at[i]
		}
	}
	return signatureOff
}
