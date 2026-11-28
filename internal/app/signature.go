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
	group     *group
	editor    *editor.State
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
	manual    bool   // explicitly opened; follows the caret within this call
	visible   bool   // the strip fit in the focused editor at the last draw
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
	if g != a.focus {
		if s.group == g {
			a.cancelSignature()
			s.help = nil
		}
		return
	}
	if a.completionFor(g).visible {
		if s.group == g && (s.timer != nil || s.cancel != nil || s.asked || s.help != nil) {
			a.cancelSignature()
			s.help = nil
			s.offset = signatureOff
		}
		return
	}
	if s.doc != nil && (s.doc != d || s.group != g) {
		triggered := s.triggered
		a.clearSignature()
		s.triggered = triggered
	}
	if !a.signatureEnabled() || d.buf == nil {
		a.clearSignature()
		return
	}
	offset := st.Cursor()
	start := callStartAt(d.buf, offset)
	if start == signatureOff {
		a.clearSignature()
		return
	}
	line := d.buf.Pos(offset).Line
	version := d.buf.Version()
	sameCall := s.group == g && s.doc == d && s.callStart == start
	if sameCall {
		s.editor = st
		moved := s.offset != offset || s.version != version
		s.caretLine = line
		if moved {
			a.cancelSignature()
			s.offset, s.version = offset, version
			s.help = nil
		}
		if s.triggered {
			s.triggered = false
			s.dismissed = false
			s.manual = true
			a.cancelSignature()
			s.offset, s.version = offset, version
			a.askSignature(g, d)
			return
		}
		if s.dismissed {
			return
		}
		if moved && (s.manual || a.cfg.ParameterHelp == "always") {
			a.deferSignature(g, d)
		}
		return
	}
	a.cancelSignature()
	s.group, s.doc = g, d
	s.editor = st
	s.callStart, s.offset, s.version = start, offset, version
	s.caretLine = line
	s.help = nil
	s.active = 0
	s.dismissed = false
	s.asked = false
	s.manual = false
	if s.triggered {
		s.triggered = false
		s.manual = true
		a.askSignature(g, d)
		return
	}
	// Always asks once the caret rests; On Trigger —
	// default — waits for Control-Shift+Space, so a new call
	// here does nothing yet.
	if a.cfg.ParameterHelp != "always" {
		return
	}
	a.deferSignature(g, d)
}

// deferSignature asks for signature help after the caret rests for
// signatureDelay; moving again cancels the timer or invalidates its reply.
func (a *App) deferSignature(g *group, d *doc) {
	s := &a.signature
	if s.timer != nil {
		s.timer.Stop()
		s.timer = nil
	}
	if s.cancel != nil || s.asked {
		return
	}
	s.seq++
	seq, gen, start, offset, version := s.seq, a.gen, s.callStart, s.offset, s.version
	post := a.post.Post
	s.timer = time.AfterFunc(signatureDelay, func() {
		post(func() {
			if s.seq != seq {
				return
			}
			s.timer = nil
			if a.gen != gen || s.group != g || s.doc != d || s.callStart != start ||
				s.offset != offset || s.version != version || s.dismissed || g != a.focus || a.completionFor(g).visible {
				return
			}
			a.askSignature(g, d)
		})
	})
}

// askSignature requests the signature of the call at s.callStart of d.
// The server marks the active signature and parameter; See shows
// whichever it names.
func (a *App) askSignature(g *group, d *doc) {
	s := &a.signature
	m := a.serverFor(d.path)
	if m == nil || d.buf == nil || !a.workspaceTrusted() {
		s.asked = false
		return
	}
	if state, _ := m.Status(); state == langserver.Failed {
		s.asked = false
		return
	}
	if c := m.Current(); c != nil {
		p, ok := c.(signatureClient)
		if !ok || !p.HasSignatureHelp() {
			s.asked = false
			return
		}
	}
	snap := d.buf.Snapshot()
	s.asked = true
	pos := snap.Pos(min(s.offset, snap.Len()))
	line := snap.Line(pos.Line)
	s.seq++
	seq, gen, start, offset, version := s.seq, a.gen, s.callStart, s.offset, snap.Version()
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	s.cancel = cancel
	post := a.post.Post
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		var help *lsp.SignatureHelp
		if err == nil {
			p, ok := c.(signatureClient)
			if !ok || !p.HasSignatureHelp() {
				err = fmt.Errorf("signature help is not supported")
			} else if err = m.Sync(ctx, c, d.uri, snap); err == nil {
				at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, c.Encoding())}
				help, err = p.SignatureHelp(ctx, d.uri, at)
			}
		}
		post(func() {
			if s.seq != seq {
				return
			}
			s.cancel = nil
			s.timer = nil
			if a.gen != gen || s.group != g || s.doc != d || s.callStart != start ||
				s.offset != offset || s.version != version || d.buf == nil || d.buf.Version() != version ||
				s.dismissed || g != a.focus || a.completionFor(g).visible || s.editor == nil ||
				a.activeEditor() != s.editor || s.editor.Cursor() != offset {
				s.asked = false
				return
			}
			if err != nil || help == nil || len(help.Signatures) == 0 {
				s.help = nil
				s.asked = false
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
	s.asked = false
}

// clearSignature hides the strip and forgets the call: the caret left
// the argument list or the document is gone.
func (a *App) clearSignature() {
	a.cancelSignature()
	s := &a.signature
	s.group = nil
	s.editor = nil
	s.doc = nil
	s.callStart = signatureOff
	s.offset = 0
	s.asked = false
	s.help = nil
	s.active = 0
	s.dismissed = false
	s.triggered = false
	s.manual = false
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
func (a *App) signatureInput(g *group, st *editor.State, ev ui.InputEvent) bool {
	s := &a.signature
	if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
		if g == a.focus && !a.completionFor(g).visible && s.group == g && s.doc != nil &&
			(s.help != nil || s.cancel != nil || s.timer != nil) {
			a.dismissSignature()
			return true
		}
		return false
	}
	if g != a.focus || a.completionFor(g).visible || s.group != g || s.doc == nil || s.dismissed ||
		!s.visible || s.help == nil || len(s.help.Signatures) < 2 {
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
func (a *App) signatureHelp(g *group, d *doc) *lsp.SignatureHelp {
	s := &a.signature
	if g != a.focus || a.completionFor(g).visible || s.group != g || s.doc != d || s.dismissed || s.help == nil || len(s.help.Signatures) == 0 ||
		d.buf == nil || d.buf.Version() != s.version {
		return nil
	}
	if s.editor == nil || s.editor.Cursor() != s.offset || a.activeEditor() != s.editor {
		return nil
	}
	return s.help
}

// signatureStrip draws the strip under the line holding the caret: the
// active signature in the editor's font at 12 pt, its active parameter
// emphasised, a "1/2" of overloads, and the parameter's one line of
// documentation below when the server sends it. It borrows the peek's
// material — a footnote to the code, not a window.
func (a *App) signatureStrip(c *ui.Context, bounds ui.Rect, st *editor.State, g *group, d *doc, gutter float32) {
	if g == a.focus {
		a.signature.visible = false
	}
	help := a.signatureHelp(g, d)
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
	lineTop := bounds.Y + float32(st.LineTop(s.caretLine)-st.ScrollY())
	caretY := lineTop + float32(fontSize*lineHeight)
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
	labelWidth, _ := c.MeasureText(0, spans...)
	contentWidth := labelWidth
	if len(help.Signatures) > 1 {
		counterWidth, _ := c.MeasureText(0, ui.Span{Text: fmt.Sprintf("%d/%d", s.active+1, len(help.Signatures)), Size: 11})
		contentWidth += counterWidth + 8
	}
	if doc != "" {
		docWidth, _ := c.MeasureText(0, ui.Span{Text: doc, Size: 11})
		contentWidth = max(contentWidth, docWidth)
	}
	caretX := max(bounds.X+gutter, min(bounds.X+bounds.W-4, g.caretAnchor.X))
	strip, visible := signatureStripBounds(bounds, ui.Point{X: caretX, Y: caretY}, lineTop, float32(fontSize*lineHeight), contentWidth+22, height)
	if !visible {
		return
	}
	s.visible = true
	ui.Overlay(c, func() {
		col := ui.Column(c).Absolute().Left(strip.X).Top(strip.Y).Width(strip.W).Height(strip.H).
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

func signatureStripBounds(bounds ui.Rect, caret ui.Point, lineTop, lineHeight, width, height float32) (ui.Rect, bool) {
	const pad = float32(4)
	if bounds.W <= 2*pad || bounds.H <= 2*pad || lineTop+lineHeight <= bounds.Y || lineTop >= bounds.Y+bounds.H {
		return ui.Rect{}, false
	}
	width = min(width, float32(600), bounds.W-2*pad)
	if width <= 0 || height > bounds.H-2*pad {
		return ui.Rect{}, false
	}
	left := caret.X + 8
	if left+width > bounds.X+bounds.W-pad {
		left = caret.X - width - 8
	}
	left = max(bounds.X+pad, min(left, bounds.X+bounds.W-pad-width))
	top := lineTop + lineHeight
	if top+height > bounds.Y+bounds.H-pad {
		top = lineTop - pad - height
	}
	if top < bounds.Y+pad || top+height > bounds.Y+bounds.H-pad {
		return ui.Rect{}, false
	}
	return ui.Rect{X: left, Y: top, W: width, H: height}, true
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

// callStartAt finds the innermost unmatched '(' before offset, skipping
// strings and comments. An unmatched '{' or '[' takes precedence, so an
// enclosing call is not shown while editing an object, array, or block.
// A nested call in one of those constructs still wins. It returns
// signatureOff outside a call. The scan runs over the
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
				if depth > 0 && matchingBracket(stack[depth-1], b) {
					depth--
				}
			}
		}
	}
	if depth > 0 && stack[depth-1] == '(' {
		return at[depth-1]
	}
	return signatureOff
}

func matchingBracket(open, close byte) bool {
	return open == '(' && close == ')' || open == '[' && close == ']' || open == '{' && close == '}'
}
