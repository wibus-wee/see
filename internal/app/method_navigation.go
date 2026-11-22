package app

import (
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

// methodShortcut is JetBrains' Next/Previous Method key off macOS; on
// macOS, where Ctrl+Shift+↑/↓ selects paragraphs, Ctrl+Option+↑/↓ is free.
func methodShortcut(goos string) (string, ui.Modifiers) {
	if goos == "darwin" {
		return "Ctrl+Alt+", ui.Ctrl | ui.Alt
	}
	return "Alt+", ui.Alt
}

// goToMethod moves the caret to the name of the next (dir > 0) or previous
// function, method or constructor declared in the active document, as the
// outline lists them. It stops at either end instead of wrapping.
func (a *App) goToMethod(dir int) {
	st := a.activeEditor()
	d := a.docs[a.activePath()]
	if st == nil || d == nil || d.buf == nil {
		return
	}
	a.refreshOutline()
	o := &a.outline
	if o.shown.doc != d || o.shown.version != d.buf.Version() {
		return
	}
	if p, ok := methodTarget(o.symbols, st.Buffer, st.CursorPos(), o.enc, dir); ok {
		a.cancelNav()
		st.GoTo(p)
		a.focusEditor = st
	}
}

// methodTarget is the position of the nearest callable symbol name after
// (dir > 0) or before cursor.
func methodTarget(symbols []lsp.DocumentSymbol, buf *editor.Buffer, cursor editor.Pos, enc string, dir int) (editor.Pos, bool) {
	var best editor.Pos
	found := false
	for _, s := range symbols {
		if !callableKind(s.Kind) || s.Position.Line < 0 || s.Position.Line >= buf.LineCount() {
			continue
		}
		p := editor.Pos{Line: s.Position.Line, Col: lsp.ColumnFromLSP(buf.Line(s.Position.Line), s.Position.Character, enc)}
		if dir > 0 && cursor.Less(p) && (!found || p.Less(best)) ||
			dir < 0 && p.Less(cursor) && (!found || best.Less(p)) {
			best, found = p, true
		}
	}
	return best, found
}
