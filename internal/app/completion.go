package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
)

type completionClient interface {
	HasCompletion() bool
	Completion(context.Context, string, lsp.Position) (*lsp.CompletionResult, error)
}

type completionState struct {
	seq        uint64
	cancel     context.CancelFunc
	visible    bool
	incomplete bool
	editor     *editor.State
	doc        *doc
	path       string
}

func (a *App) completionFor(g *group) *completionState {
	if a.completions == nil {
		a.completions = make(map[int]*completionState)
	}
	if a.completions[g.id] == nil {
		a.completions[g.id] = &completionState{}
	}
	return a.completions[g.id]
}

func (a *App) completionForID(id int) *completionState {
	return a.completions[id]
}

func (a *App) cancelCompletionRequest(g *group) {
	cs := a.completionFor(g)
	cs.seq++
	if cs.cancel != nil {
		cs.cancel()
		cs.cancel = nil
	}
}

func (a *App) cancelCompletions(g *group, st *editor.State) {
	a.cancelCompletionRequest(g)
	cs := a.completionFor(g)
	cs.visible, cs.incomplete = false, false
	cs.editor, cs.doc, cs.path = nil, nil, ""
	if st != nil {
		st.ShowCompletions(-1, nil)
	}
}

func (a *App) releaseCompletionGroup(g *group) {
	if cs := a.completions[g.id]; cs != nil {
		a.cancelCompletions(g, cs.editor)
		delete(a.completions, g.id)
	}
}

func completionInputEdits(ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputText, ui.InputCompose:
		return ev.Text != ""
	case ui.InputCommand:
		return ev.Text == "paste" || ev.Text == "pasteAndMatchStyle" || ev.Text == "cut" ||
			ev.Text == "delete" || ev.Text == "undo" || ev.Text == "redo"
	case ui.InputKeyDown:
		return ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete
	default:
		return false
	}
}

func (a *App) requestCompletions(g *group, d *doc, st *editor.State, req editor.CompletionRequest) {
	a.cancelCompletionRequest(g)
	cs := a.completionFor(g)
	if g != a.focus || g.active != d.path || a.docs[d.path] != d || d.buf == nil ||
		st == nil || st.Buffer != d.buf || st.ReadOnly || !a.cfg.IsTrusted(a.root) {
		return
	}
	m := a.serverFor(d.path)
	if m == nil {
		return
	}
	if sels := st.Selections(); len(sels) != 1 || !sels[0].Empty() {
		return
	}
	snap := d.buf.Snapshot()
	start := req.Offset - len(req.Prefix)
	if req.Version != snap.Version() || req.Offset < 0 || req.Offset > snap.Len() || start < 0 ||
		st.Cursor() != req.Offset || snap.Slice(start, req.Offset) != req.Prefix {
		return
	}
	if state, _ := m.Status(); state == langserver.Failed {
		return
	}
	if current := m.Current(); current != nil {
		p, ok := current.(completionClient)
		if !ok || !p.HasCompletion() {
			return
		}
	}
	pos := snap.Pos(req.Offset)
	line := snap.Line(pos.Line)
	uri, path, groupID := d.uri, d.path, g.id
	version, generation := snap.Version(), a.gen
	cs.incomplete = false
	cs.seq++
	seq := cs.seq
	cs.editor, cs.doc, cs.path = st, d, path
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	cs.cancel = cancel
	post := a.post.Post
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		var result *lsp.CompletionResult
		if err == nil {
			p, ok := client.(completionClient)
			if !ok || !p.HasCompletion() {
				err = fmt.Errorf("completion is not supported")
			} else if err = m.Sync(ctx, client, uri, snap); err == nil {
				at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, client.Encoding())}
				result, err = p.Completion(ctx, uri, at)
			}
		}
		post(func() {
			a.finishCompletions(groupID, path, d, req, start, version, generation, seq, clientEncoding(client), result, err)
		})
	}()
}

func clientEncoding(client langserver.Client) string {
	if client == nil {
		return lsp.UTF16
	}
	return client.Encoding()
}

func (a *App) finishCompletions(groupID int, path string, d *doc, req editor.CompletionRequest, start int, version uint64, generation int, seq uint64, encoding string, result *lsp.CompletionResult, err error) {
	cs := a.completionForID(groupID)
	if cs == nil || cs.seq != seq {
		return
	}
	cs.cancel = nil
	st := cs.editor
	valid := generation == a.gen && a.focus != nil && a.focus.id == groupID && a.focus.active == path &&
		a.docs[path] == d && cs.doc == d && cs.path == path && st != nil && st == a.activeEditor() &&
		d.buf != nil && d.buf.Version() == version && st.Buffer == d.buf && st.Cursor() == req.Offset &&
		!st.ReadOnly && a.cfg.IsTrusted(a.root)
	if valid {
		sels := st.Selections()
		valid = len(sels) == 1 && sels[0].Empty()
	}
	if !valid {
		if st != nil {
			st.ShowCompletions(-1, nil)
		}
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path = nil, nil, ""
		return
	}
	if err != nil || result == nil {
		st.ShowCompletions(start, nil)
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path = nil, nil, ""
		return
	}
	items := completionItems(result.Items, d.buf, req, encoding)
	st.ShowCompletions(start, items)
	cs.incomplete = result.IsIncomplete && len(items) > 0
	if len(items) == 0 {
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path = nil, nil, ""
	}
}

func completionItems(items []lsp.CompletionItem, buf *editor.Buffer, req editor.CompletionRequest, encoding string) []editor.Completion {
	snap := buf.Snapshot()
	start := req.Offset - len(req.Prefix)
	if start < 0 || req.Offset > snap.Len() {
		return nil
	}
	pos, startPos := snap.Pos(req.Offset), snap.Pos(start)
	if pos.Line != startPos.Line {
		return nil
	}
	line := snap.Line(pos.Line)
	want := lsp.Range{
		Start: lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, startPos.Col, encoding)},
		End:   lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, encoding)},
	}
	out := make([]editor.Completion, 0, len(items))
	for _, item := range items {
		if item.Label == "" || item.HasAdditionalEdits || item.HasCommand || item.InsertTextFormat == 2 || item.InsertTextFormat > 2 {
			continue
		}
		insert := item.InsertText
		if item.TextEdit != nil {
			if item.TextEdit.Range == nil || item.TextEdit.InsertReplace || *item.TextEdit.Range != want {
				continue
			}
			insert = item.TextEdit.NewText
		} else if insert == "" {
			insert = item.Label
		}
		filter := item.FilterText
		if filter == "" {
			filter = item.Label
		}
		out = append(out, editor.Completion{
			Label: item.Label, Detail: item.Detail, Documentation: item.Documentation,
			Insert: insert, FilterText: filter, Kind: completionKind(item.Kind),
		})
	}
	return out
}

func completionKind(kind int) editor.CompletionKind {
	switch kind {
	case 2:
		return editor.CompletionMethod
	case 3, 4:
		return editor.CompletionFunction
	case 5, 10:
		return editor.CompletionField
	case 6:
		return editor.CompletionVariable
	case 7, 8, 22, 25:
		return editor.CompletionType
	case 9:
		return editor.CompletionModule
	case 12, 13, 20, 21:
		return editor.CompletionConstant
	case 14:
		return editor.CompletionKeyword
	case 15:
		return editor.CompletionSnippet
	default:
		return editor.CompletionText
	}
}
