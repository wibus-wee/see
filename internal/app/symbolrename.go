package app

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/workspace"
	"see/locales"
)

// renameClient is a language client that renames symbols: it may answer
// textDocument/prepareRename before textDocument/rename.
type renameClient interface {
	HasRename() bool
	HasPrepareRename() bool
	PrepareRename(context.Context, string, lsp.Position) (*lsp.PrepareRenameResult, error)
	Rename(context.Context, string, lsp.Position, string) (*lsp.WorkspaceEdit, error)
}

// renameState is a symbol rename awaiting the new name: the request's
// staleness checks, the resolved position and range, and the field the
// user types into.
type renameState struct {
	doc      *doc
	group    *group
	st       *editor.State
	server   *langserver.Server
	client   renameClient
	pos      lsp.Position // the position asked at
	line     int          // its document line, for the strip
	enc      string       // the encoding positions are in
	rng      *lsp.Range   // what prepareRename validated; nil when unchecked
	name     string       // the field's text
	selected bool         // the field selected the seed once
	seq      uint64       // request generation, for staleness
	applying bool         // a rename request is on its way
}

// renameDocument checks that g's d and st still show one editable text
// buffer, the way scopeEditDocumentFor does.
func (a *App) renameDocumentFor(g *group, d *doc, st *editor.State) (*doc, *editor.State) {
	if g == nil || a.layout == nil || a.layout.find(g) == nil || d == nil || st == nil ||
		d.path == "" || g.active != d.path || g.eds[d.path] != st || a.docs[d.path] != d ||
		d.buf == nil || st.Buffer != d.buf || st.ReadOnly ||
		d.untitled || d.loading || d.err != nil || d.gone || d.kind != workspace.Text ||
		d.plain || d.lossy || d.video || d.image != nil || g.hexPreview[d.path] ||
		isMarkdown(d.path) || isMarkdownTab(g.active) {
		return nil, nil
	}
	return d, st
}

// renameDocument is renameDocumentFor of the focused group's active
// document.
func (a *App) renameDocument() (*doc, *editor.State) {
	if a.focus == nil {
		return nil, nil
	}
	return a.renameDocumentFor(a.focus, a.docs[a.activePath()], a.activeEditor())
}

// renameEnabled reports whether the file's server renames symbols, for
// the command's enabled check and the context menu item.
func (a *App) renameEnabled(path string) bool {
	m := a.serverFor(path)
	if m == nil {
		return false
	}
	c := m.Current()
	if c == nil {
		return false
	}
	r, ok := c.(renameClient)
	return ok && r.HasRename()
}

// renameSymbol asks to rename the symbol under the caret of the focused
// editor, from the command palette or its shortcut.
func (a *App) renameSymbol() {
	g := a.focus
	if g == nil {
		return
	}
	a.renameSymbolAt(g, a.docs[a.activePath()], a.activeEditor())
}

// renameSymbolAt starts a rename for the caret of st, or for the pointer
// the context menu opened on.
func (a *App) renameSymbolAt(g *group, d *doc, st *editor.State) {
	d, st = a.renameDocumentFor(g, d, st)
	if d == nil || st == nil {
		return
	}
	a.focus = g
	m := a.serverFor(d.path)
	if m == nil {
		a.note(locales.MsgNoLanguageServerIsConfiguredForThisFile(), false)
		return
	}
	if !a.cfg.IsTrusted(a.root) {
		a.trustAsk = true
		a.afterTrust = func() { a.renameSymbolAt(g, d, st) }
		return
	}
	c := m.Current()
	if c == nil {
		a.note(locales.MsgCodeActionUnavailable(), false)
		return
	}
	r, ok := c.(renameClient)
	if !ok || !r.HasRename() {
		a.note(locales.RenameUnsupported(), false)
		return
	}
	a.cancelRename()
	a.cancelNav()
	snap := d.buf.Snapshot()
	pos := snap.Pos(st.Cursor())
	line := snap.Line(pos.Line)
	at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, c.Encoding())}
	version := d.buf.Version()
	a.renameSeq++
	seq, gen := a.renameSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.renameCancel = cancel
	a.finding = true
	a.findingLabel = locales.RenamePreparing()
	a.notice = ""
	go func() {
		defer cancel()
		if err := m.Sync(ctx, c, d.uri, snap); err != nil {
			a.post.Post(func() {
				if a.gen == gen && a.renameSeq == seq {
					a.finding = false
					a.renameCancel = nil
					a.lspFailure(err)
				}
			})
			return
		}
		var rng *lsp.Range
		var placeholder string
		if r.HasPrepareRename() {
			prep, err := r.PrepareRename(ctx, d.uri, at)
			if err != nil {
				a.post.Post(func() {
					if a.gen == gen && a.renameSeq == seq {
						a.finding = false
						a.renameCancel = nil
						if errors.Is(err, lsp.ErrNoRename) {
							a.note(locales.RenameNone(), false)
						} else {
							a.lspFailure(err)
						}
					}
				})
				return
			}
			if prep == nil || prep.Empty() {
				a.post.Post(func() {
					if a.gen == gen && a.renameSeq == seq {
						a.finding = false
						a.renameCancel = nil
						a.note(locales.RenameNone(), false)
					}
				})
				return
			}
			if !prep.DefaultBehavior {
				rng = &prep.Range
			}
			placeholder = prep.Placeholder
		}
		// Seed the field with the server's placeholder, or the word the
		// caret is on when the server names none.
		name := placeholder
		if name == "" {
			ws, we := wordAt(line, pos.Col)
			if ws != we {
				name = line[ws:we]
			}
		}
		a.post.Post(func() {
			if a.gen != gen || a.renameSeq != seq || a.docs[d.path] != d || d.buf.Version() != version {
				return
			}
			a.finding = false
			a.renameCancel = nil
			a.rename = &renameState{
				doc: d, group: g, st: st, server: m, client: r,
				pos: at, line: pos.Line, enc: c.Encoding(), rng: rng, name: name, seq: seq,
			}
		})
	}()
}

// cancelRename drops an in-flight or open rename: its request is stale.
func (a *App) cancelRename() {
	if a.renameCancel != nil {
		a.renameCancel()
		a.renameCancel = nil
	}
	a.rename = nil
}

// renameStrip draws the rename input under the symbol's line, borrowing
// the signature strip's material: a footnote to the code, not a window.
func (a *App) renameStrip(c *ui.Context, bounds ui.Rect, st *editor.State, d *doc, gutter float32) {
	r := a.rename
	if r == nil || r.st != st || r.doc != d || r.applying {
		return
	}
	fontSize := st.Font.Size
	if fontSize <= 0 {
		fontSize = 13
	}
	lineHeight := st.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	pal := colors(c)
	snap := d.buf.Snapshot()
	lineText := snap.Line(r.line)
	// The box goes where the symbol is: the range prepareRename gave,
	// else the word the caret is on.
	startCol := 0
	if r.rng != nil && r.rng.Start.Line == r.line {
		startCol = lsp.ColumnFromLSP(lineText, r.rng.Start.Character, r.enc)
	} else {
		ws, _ := wordAt(lineText, snap.Pos(st.Cursor()).Col)
		startCol = ws
	}
	top := bounds.Y + float32(st.LineTop(r.line)-st.ScrollY()) + float32(fontSize*lineHeight)
	const stripH, gap = 26, 4
	if top+stripH > bounds.Y+bounds.H-gap {
		above := bounds.Y + float32(st.LineTop(r.line)-st.ScrollY()) - gap - stripH
		if above >= bounds.Y+gap {
			top = above
		}
	}
	prefix := strings.ReplaceAll(lineText[:min(startCol, len(lineText))], "\t", "    ")
	prefixW, _ := c.MeasureText(0, ui.Span{Text: prefix, Font: st.Font.Family, Size: fontSize})
	left := bounds.X + gutter + 4 + prefixW
	width := float32(260)
	ui.Overlay(c, func() {
		col := ui.Column(c).Absolute().Left(left).Top(top).Width(width).Height(stripH).
			Padding(0, 6).Radius(6).Clip().Background(pal.Popover).Border(1, pal.PopoverEdge).
			Shadow(0, 1, 3, 0, ui.RGBA(0, 0, 0, 0.08)).Label(locales.RenameSymbol())
		near, far := ui.RGBA(0, 0, 0, 0.08), ui.RGBA(0, 0, 0, 0.16)
		if c.Theme().Dark {
			near, far = ui.RGBA(0, 0, 0, 0.30), ui.RGBA(0, 0, 0, 0.45)
		}
		col.Shadow(0, 12, 32, 0, far).Shadow(0, 1, 3, 0, near)
		col.Children(func() {
			row := ui.Row(c).FillWidth().Height(stripH).AlignItems(ui.Center).Gap(4)
			row.Children(func() {
				ui.Text(c, locales.RenameSymbol()).FontSize(11).TextColor(pal.Muted).SingleLine()
				in := ui.TextInput(c, &r.name).Label(locales.RenameSymbol()).FontSize(12).Grow(1).AutoFocus().Background(ui.Transparent).BorderWidth(0)
				if !r.selected {
					r.selected = true
					in.SetTextSelection(0, utf8.RuneCountInString(r.name))
				}
				switch {
				case in.Submitted():
					a.submitSymbolRename(r)
				case in.Shortcut(0, ui.KeyEscape):
					a.cancelRename()
				}
			})
		})
	})
}

// submitSymbolRename sends the field's name to the server and applies
// the edits it answers.
func (a *App) submitSymbolRename(r *renameState) {
	name := strings.TrimSpace(r.name)
	if name == "" {
		a.note(locales.RenameEmpty(), false)
		return
	}
	r.applying = true
	d, m, client, pos, seq := r.doc, r.server, r.client, r.pos, r.seq
	gen := a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	go func() {
		defer cancel()
		edit, err := client.Rename(ctx, d.uri, pos, name)
		a.post.Post(func() {
			if a.gen != gen || a.renameSeq != seq || a.rename != r {
				return
			}
			if err != nil {
				r.applying = false
				a.lspFailure(err)
				return
			}
			if edit == nil || len(edit.EditURIs()) == 0 {
				a.cancelRename()
				a.note(locales.RenameNone(), false)
				return
			}
			a.cancelRename()
			a.applyWorkspaceEdit(edit, m)
		})
	}()
}
