package app

import (
	"fmt"
	"path/filepath"
	"see/locales"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/workspace"
)

// peekState is a definition peeked at from the editor where its use is:
// the file and lines the language server gave, read without opening the
// file, with the definition's own line lit.
type peekState struct {
	group   *group
	doc     *doc          // the editor the peek is anchored to
	from    nav.Entry     // where the cursor was, to anchor the peek
	t       target        // the definition peeked at
	enc     string        // the encoding t.pos is in
	preview *editor.State // the definition's file, read-only at its line
}

// peekedDefinition buffers the target of a definition navigation as a
// peek in the editor of d: a snapshot of an open document, which keeps
// unsaved edits, or the file as it reads from disk.
func (a *App) peekedDefinition(t target) *editor.State {
	path := workspace.DocPath(a.root, a.realRoot, t.path)
	if d := a.docs[path]; d != nil && d.buf != nil {
		return peekEditor(a, editor.NewBuffer(d.buf.Snapshot().String()), t)
	}
	f, err := workspace.ReadFile(t.path)
	if err != nil || f.Kind != workspace.Text {
		return nil
	}
	return peekEditor(a, editor.NewBuffer(f.Text), t)
}

// peekEditor is buf read-only for a peek, at t's definition.
func peekEditor(a *App, buf *editor.Buffer, t target) *editor.State {
	buf.SetLanguage(languageFor(t.path, buf.Line(0)))
	st := &editor.State{Buffer: buf, ReadOnly: true, Label: locales.CommandPeekDefinition(), Theme: a.edTheme}
	a.applyEditorPrefs(st)
	return st
}

// peekFound ends a peek navigation the way locationFound ends a
// definition one, but shows a single target in a peek of the editor it
// was asked from rather than going there.
func (a *App) peekFound(g *group, from nav.Entry, targets []target, enc string, skipped int, err error) {
	switch {
	case err != nil:
		a.lspFailure(err)
	case len(targets) == 0 && skipped > 0:
		a.note(locales.MsgTheDefinitionIsInAPlaceThatIs(), false)
	case len(targets) == 0:
		a.note(locales.MsgNoDefinitionFound(), false)
	case len(targets) == 1:
		a.peekDefinitionFound(g, from, targets[0], enc)
	default:
		// A symbol with several definitions gets the choice list, as Go
		// to Definition gives it.
		a.choices = &defChoices{g: g, from: from, enc: enc, locs: targets, title: "definitions"}
		a.git.peek = nil
		a.loadRelatedPreviews(a.choices)
	}
}

// peekDefinitionFound shows the single definition a peek navigation
// found in the editor it was asked from, or leaves a peek without a
// preview when the file cannot be read.
func (a *App) peekDefinitionFound(g *group, from nav.Entry, t target, enc string) {
	p := &peekState{group: g, doc: a.docs[from.Path], from: from, t: t, enc: enc}
	p.preview = a.peekedDefinition(t)
	if p.preview != nil {
		buf := p.preview.Buffer
		line := min(max(t.pos.Line, 0), buf.LineCount()-1)
		col := lsp.ColumnFromLSP(buf.Line(line), t.pos.Character, enc)
		p.preview.LineBackgrounds = []editor.LineBackground{{First: line, Last: line, Color: a.colors().Pill}}
		// Scroll the definition to the popover's top, the doc comment
		// above it included.
		p.preview.ScrollTo(max(0, line-1))
		p.preview.GoTo(editor.Pos{Line: line, Col: col})
		if t.end.Line == t.pos.Line && t.end.Character > t.pos.Character {
			end := lsp.ColumnFromLSP(buf.Line(line), t.end.Character, enc)
			p.preview.TextBackgrounds = []editor.TextBackground{{Start: buf.Offset(editor.Pos{Line: line, Col: col}), End: buf.Offset(editor.Pos{Line: line, Col: end}), Color: a.colors().Match}}
		}
	}
	a.choices = nil
	a.git.peek = nil
	a.peek = p
}

// peekTitle is the header of a definition peek: the file it is in and
// the line the definition starts on.
func (a *App) peekTitle(t target) string {
	name := fmt.Sprintf("%s:%d", workspace.RelPath(a.root, t.path), t.pos.Line+1)
	if !workspace.Within(a.root, t.path) {
		name = fmt.Sprintf("%s:%d", t.path, t.pos.Line+1)
	}
	return fmt.Sprintf("%s — %s", filepath.Base(t.path), name)
}

// peekContentHeight is the height a peek's code wants: the definition's
// file capped at a windowful of lines, or a message's height when the
// file cannot be read.
func peekContentHeight(st *editor.State, p *peekState) float32 {
	lines := 3
	if p.preview != nil {
		lines = min(p.preview.Buffer.LineCount(), 16)
	}
	fontSize, lineHeight := st.Font.Size, st.LineHeight
	if fontSize <= 0 {
		fontSize = 13
	}
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	return float32(lines)*fontSize*lineHeight + 42 // the jump row
}

// peekView is the body of a definition peek: the definition's code at
// its line, and a button that jumps to it for real.
func (a *App) peekView(c *ui.Context, p *peekState) {
	ui.Column(c).Grow(1).FillWidth().Children(func() {
		if p.preview == nil {
			ui.Text(c, locales.PeekDefinitionUnavailable()).FontSize(12).TextColor(colors(c).Muted).Padding(12)
		} else {
			editor.View(c.Key("definition-peek"), p.preview).Grow(1).FillWidth()
		}
		ui.Row(c).FillWidth().Shrink(0).Padding(6, 12).Gap(8).AlignItems(ui.Center).
			BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Children(func() {
			loc := fmt.Sprintf("%s:%d", workspace.RelPath(a.root, p.t.path), p.t.pos.Line+1)
			if !workspace.Within(a.root, p.t.path) {
				loc = fmt.Sprintf("%s:%d", p.t.path, p.t.pos.Line+1)
			}
			ui.Text(c, loc).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			if ui.Button(c, commandLabel("definition.goto")).Clicked() {
				a.jump(p.group, p.from, p.t, p.enc)
				a.peek = nil
			}
		})
	})
}
