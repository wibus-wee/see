package app

import (
	"bytes"
	"context"
	"errors"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/uri"
	"see/internal/workspace"
	"see/locales"
)

// codeActionClient is a language client that answers code action
// requests, resolves the answers it left incomplete, and runs their
// commands.
type codeActionClient interface {
	HasCodeActions() bool
	CodeActionKinds() []string
	CodeActions(ctx context.Context, uri string, r lsp.Range, diagnostics []lsp.Diagnostic) ([]lsp.CodeAction, error)
	ResolveCodeAction(ctx context.Context, action lsp.CodeAction) (*lsp.CodeAction, error)
	ExecuteCommand(ctx context.Context, command lsp.Command) error
}

// codeActions is the actions of one request, kept on the App so the
// menu a later frame builds has them. The anchor is where the menu
// opens: the caret's, or the pointer's where the context menu asked.
type codeActions struct {
	doc     *doc
	server  *langserver.Server
	client  codeActionClient
	line    int
	version uint64
	anchor  ui.Point
	list    []lsp.CodeAction
	open    bool // the menu of these is open
}

// codeActionsEnabled reports whether the file's server answers code
// actions, for the menu item and the command's shortcut.
func (a *App) codeActionsEnabled(path string) bool {
	m := a.serverFor(path)
	if m == nil {
		return false
	}
	c := m.Current()
	if c == nil {
		return false
	}
	ca, ok := c.(codeActionClient)
	return ok && ca.HasCodeActions()
}

// showCodeActions asks for the code actions of the focused editor's
// caret line, from the command palette or its shortcut.
func (a *App) showCodeActions() {
	st := a.activeEditor()
	if st == nil {
		return
	}
	d := a.docs[a.activePath()]
	if d == nil {
		return
	}
	a.codeActionAtPoint(a.focus, d, st, true)
}

// codeActionAt asks for the code actions of the line the caret is on in
// the focused editor, the menu going at the caret. The diagnostics it
// passes are the ones the server published for that line, which is what
// fixes answer to.
func (a *App) codeActionAt(g *group, d *doc, st *editor.State) {
	a.codeActionAtPoint(g, d, st, true)
}

// codeActionAtPoint is codeActionAt at the caret or at the pointer:
// from a shortcut or the context menu of the line the pointer is on.
func (a *App) codeActionAtPoint(g *group, d *doc, st *editor.State, caret bool) {
	if d.buf == nil {
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
		a.afterTrust = func() { a.codeActionAt(g, d, st) }
		return
	}
	c := m.Current()
	if c == nil {
		a.note(locales.MsgCodeActionUnavailable(), false)
		return
	}
	ca, ok := c.(codeActionClient)
	if !ok || !ca.HasCodeActions() {
		a.note(locales.CodeActionUnsupported(), false)
		return
	}
	a.cancelNav()
	snap := d.buf.Snapshot()
	var line int
	var anchor ui.Point
	if caret {
		line = st.CursorPos().Line
		anchor = g.caretAnchor
	} else {
		line = snap.Pos(st.Cursor()).Line
		anchor = g.pointerAnchor
	}
	version := d.buf.Version()
	diagnostics := a.lineDiagnostics(d, snap, line)
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = locales.CodeActionFinding()
	a.notice = ""
	go func() {
		defer cancel()
		if err := m.Sync(ctx, c, d.uri, snap); err != nil {
			a.post.Post(func() {
				if a.gen == gen && a.navSeq == seq {
					a.finding = false
					a.lspFailure(err)
				}
			})
			return
		}
		lineText := snap.Line(line)
		rng := lsp.Range{Start: lsp.Position{Line: line, Character: 0}, End: lsp.Position{Line: line, Character: lsp.ColumnToLSP(lineText, len(lineText), c.Encoding())}}
		actions, err := ca.CodeActions(ctx, d.uri, rng, diagnostics)
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq || a.docs[d.path] != d || d.buf.Version() != version {
				return
			}
			a.finding = false
			a.navCancel = nil
			if err != nil {
				a.lspFailure(err)
				return
			}
			if len(actions) == 0 {
				a.note(locales.CodeActionEmpty(), false)
				return
			}
			a.codeActions = &codeActions{doc: d, server: m, client: ca, line: line, version: version, anchor: anchor, list: actions, open: true}
		})
	}()
}

// lineDiagnostics is the diagnostics the server published for d that
// fall on line, re-mapped to LSP ranges of snap.
func (a *App) lineDiagnostics(d *doc, snap editor.Snapshot, line int) []lsp.Diagnostic {
	if d.diagnosticVersion != snap.Version() {
		return nil
	}
	m := a.serverFor(d.path)
	if m == nil {
		return nil
	}
	c := m.Current()
	if c == nil {
		return nil
	}
	enc := c.Encoding()
	var out []lsp.Diagnostic
	for _, e := range d.diagnostics {
		if e.Start < 0 || e.Start > snap.Len() {
			continue
		}
		p := snap.Pos(e.Start)
		if p.Line != line {
			continue
		}
		end := snap.Pos(e.End)
		out = append(out, lsp.Diagnostic{
			Range: lsp.Range{
				Start: lsp.Position{Line: p.Line, Character: lsp.ColumnToLSP(snap.Line(p.Line), p.Col, enc)},
				End:   lsp.Position{Line: end.Line, Character: lsp.ColumnToLSP(snap.Line(end.Line), end.Col, enc)},
			},
			Severity: severityLSP(e.Severity),
			Message:  e.Message,
			Source:   e.Source,
		})
	}
	return out
}

func severityLSP(s editor.Severity) int {
	switch s {
	case editor.SeverityWarning:
		return 2
	case editor.SeverityInfo:
		return 3
	case editor.SeverityHint:
		return 4
	default:
		return 1
	}
}

// wireApplyEdit makes m answer the workspace/applyEdit request of its
// server on the app's goroutine, where applying a WorkspaceEdit is
// safe: the request arrives on the client's, and only blocks it until
// the edit ran, not on its answer.
func (a *App) wireApplyEdit(m *langserver.Server) {
	if m == nil {
		return
	}
	m.SetApplyEdit(func(s *langserver.Server, edit *lsp.WorkspaceEdit) error {
		done := make(chan error, 1)
		a.post.Post(func() { done <- a.applyWorkspaceEdit(edit, s) })
		return <-done
	})
}

// codeActionLightbulb floats the lightbulb that offers the caret
// line's fixes, at the end of the line's text: the line must hold a
// diagnostic, the file's server must answer code actions, and the menu
// the click opens is the one a.codeActions built.
func (a *App) codeActionLightbulb(c *ui.Context, g *group, d *doc, st *editor.State, bounds ui.Rect, gutter, fontSize, lineHeight float32) {
	if a.codeActions != nil || !a.codeActionsEnabled(d.path) {
		return
	}
	pos := st.CursorPos()
	lineHasDiag := false
	for _, e := range d.diagnostics {
		if d.diagnosticVersion != d.buf.Version() {
			break
		}
		if p := d.buf.Snapshot().Pos(e.Start); p.Line == pos.Line {
			lineHasDiag = true
			break
		}
	}
	if !lineHasDiag {
		return
	}
	size := fontSize * lineHeight
	// At the caret's line end, right of the text the caret is on.
	line := d.buf.Line(pos.Line)
	width, _ := c.MeasureText(0, ui.Span{Text: strings.ReplaceAll(line, "\t", "    "), Font: st.Font.Family, Size: fontSize})
	x := min(bounds.X+bounds.W-size-4, bounds.X+gutter+width+8)
	y := bounds.Y + float32(st.LineTop(pos.Line)-st.ScrollY()) + (size-size*0.8)/2
	if y < bounds.Y || y+size > bounds.Y+bounds.H {
		return
	}
	ui.Overlay(c, func() {
		b := ui.ButtonBase(c.Key(struct{ line int }{pos.Line})).Absolute().Left(x).Top(y).Width(size * 0.8).Height(size * 0.8).Radius(4).
			Label(commandLabel("codeAction.show")).Tooltip(commandLabel("codeAction.show"))
		if b.Hovered() || b.Pressed() {
			b.Background(colors(c).Pill)
		}
		b.OnClick(func() { a.codeActionAtPoint(g, d, st, true) })
		symbolIcon(c, icLightbulb, size*0.7, c.Theme().Accent).Absolute().Left(x + size*0.05).Top(y + size*0.05).PassThrough()
	})
}

// codeActionMenu builds the menu of a.codeActions's actions, grouped by
// kind: quick fixes first, then refactorings, then source actions, in
// that order inside the groups the server gave. Choosing one applies
// its workspace edit or runs its command.
func (a *App) codeActionMenu(c *ui.Context) {
	cs := a.codeActions
	if cs == nil {
		return
	}
	m := ui.MenuBase(c, &cs.open)
	m.Trigger.Absolute().Left(cs.anchor.X).Top(cs.anchor.Y).Size(0, 0)
	m.Popup(func(panel ui.Element) {
		panel.Padding(4).Radius(8).Material(popoverGlass()).Border(1, colors(c).PopoverEdge)
		groups := codeActionGroups(cs.list)
		for gi, group := range groups {
			if gi > 0 {
				ui.Divider(c)
			}
			for _, action := range group {
				title := action.Title
				disabled := action.Disabled != ""
				if disabled {
					title += " — " + action.Disabled
				}
				item := m.Item().Padding(6, 10).Radius(5).Disabled(disabled)
				if item.Highlighted() {
					item.Background(colors(c).Pill)
				}
				item.Children(func() { ui.Text(c, title).FontSize(12).SingleLine() })
				if !disabled && item.Clicked() {
					a.runCodeAction(cs, action)
				}
			}
		}
	})
}

// codeActionGroups orders actions for the menu: quick fixes, then
// refactorings, then source actions, each in the server's order inside
// its kind. A preferred action leads its group.
func codeActionGroups(actions []lsp.CodeAction) [][]lsp.CodeAction {
	quickfix := filterKind(actions, "quickfix")
	refactor := filterKind(actions, "refactor")
	source := filterKind(actions, "source")
	rest := actions[:0]
	for _, a := range actions {
		if !strings.HasPrefix(a.Kind, "quickfix") && !strings.HasPrefix(a.Kind, "refactor") && !strings.HasPrefix(a.Kind, "source") {
			rest = append(rest, a)
		}
	}
	var out [][]lsp.CodeAction
	for _, g := range [][]lsp.CodeAction{quickfix, refactor, source, rest} {
		if len(g) == 0 {
			continue
		}
		sort.SliceStable(g, func(i, j int) bool { return g[i].IsPreferred && !g[j].IsPreferred })
		out = append(out, g)
	}
	return out
}

func filterKind(actions []lsp.CodeAction, prefix string) []lsp.CodeAction {
	var out []lsp.CodeAction
	for _, a := range actions {
		if strings.HasPrefix(a.Kind, prefix) {
			out = append(out, a)
		}
	}
	return out
}

// runCodeAction runs one picked action: an edit applies at once, a
// command goes to the server, and an action with neither is resolved
// first, the resolve running what it filled in.
func (a *App) runCodeAction(cs *codeActions, action lsp.CodeAction) {
	a.codeActions = nil
	if action.NeedsResolve() && cs.client != nil {
		a.resolveCodeAction(cs, action)
		return
	}
	a.applyCodeAction(cs, &action)
}

// resolveCodeAction asks the server to fill an action in, then runs it.
func (a *App) resolveCodeAction(cs *codeActions, action lsp.CodeAction) {
	d := cs.doc
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = locales.CodeActionResolving()
	go func() {
		defer cancel()
		resolved, err := cs.client.ResolveCodeAction(ctx, action)
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq || a.docs[d.path] != d {
				return
			}
			a.finding = false
			a.navCancel = nil
			if err != nil {
				a.lspFailure(err)
				return
			}
			a.applyCodeAction(cs, resolved)
		})
	}()
}

// applyCodeAction applies the action's workspace edit across the docs
// it touches, then runs its command if it has one.
func (a *App) applyCodeAction(cs *codeActions, action *lsp.CodeAction) {
	if action.Edit != nil {
		if n := action.Edit.ResourceChanges(); n > 0 {
			a.note(locales.CodeActionResourceChanges(strconv.Itoa(n)), true)
		}
		if err := a.applyWorkspaceEdit(action.Edit, cs.server); err != nil {
			a.lspFailure(err)
			return
		}
	}
	if action.Command != nil {
		if ec, ok := cs.client.(interface{ HasExecuteCommand() bool }); !ok || !ec.HasExecuteCommand() {
			a.note(locales.CodeActionNoCommands(), false)
			return
		}
		a.executeCommand(cs, *action.Command)
	}
}

// executeCommand sends the command to the server; the edit it makes in
// return comes as a workspace/applyEdit request.
func (a *App) executeCommand(cs *codeActions, command lsp.Command) {
	d := cs.doc
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	go func() {
		defer cancel()
		err := cs.client.ExecuteCommand(ctx, command)
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq || a.docs[d.path] != d {
				return
			}
			a.navCancel = nil
			if err != nil {
				a.lspFailure(err)
			}
		})
	}()
}

// applyWorkspaceEdit applies edit to the documents it names: an open
// one through its buffer, which the undo history folds into one step;
// one that is not open through its file on disk, which the next open or
// the file watcher's reload picks up.
func (a *App) applyWorkspaceEdit(edit *lsp.WorkspaceEdit, m *langserver.Server) error {
	for _, docURI := range edit.EditURIs() {
		path, err := uri.ToPath(docURI)
		if err != nil {
			continue // a URI that is not a file is not ours to edit
		}
		edits := edit.EditsFor(docURI)
		if len(edits) == 0 {
			continue
		}
		path = workspace.DocPath(a.root, a.realRoot, path)
		d := a.docs[path]
		if d == nil || d.buf == nil {
			if err := applyEditsToFile(path, edits); err != nil {
				return err
			}
			continue
		}
		snap := d.buf.Snapshot()
		enc := "utf-16"
		if c := m.Current(); c != nil {
			enc = c.Encoding()
		}
		bufEdits := lsp.ApplyTextEdits(snap, edits, enc)
		if len(bufEdits) > 0 {
			d.buf.EditRemote(bufEdits...)
			m.Changed(d.uri, d.buf.Snapshot())
		}
	}
	return nil
}

// applyEditsToFile edits a file that has no open buffer: the text read,
// TextEdits applied against it in reverse order so earlier offsets stay
// true, and written back with the BOM it had.
func applyEditsToFile(path string, edits []lsp.TextEdit) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	text := string(data)
	bom := strings.HasPrefix(text, "\xef\xbb\xbf")
	if bom {
		text = text[3:]
	}
	snap := editor.NewBuffer(text).Snapshot()
	enc := "utf-16"
	// ApplyTextEdits needs a snapshot, but its offsets are byte offsets of
	// the text itself: a fresh buffer of the same text has them.
	sortEdits := lsp.ApplyTextEdits(snap, edits, enc)
	for i := len(sortEdits) - 1; i >= 0; i-- {
		e := sortEdits[i]
		if e.Start > len(text) || e.End > len(text) || e.Start > e.End {
			return errors.New("an edit falls outside the file's text")
		}
		text = text[:e.Start] + e.Text + text[e.End:]
	}
	var out bytes.Buffer
	if bom {
		out.WriteString("\xef\xbb\xbf")
	}
	out.WriteString(text)
	return os.WriteFile(path, out.Bytes(), 0o644)
}
