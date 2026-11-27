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

type completionResolveClient interface {
	HasCompletionItemResolve() bool
	CompletionItemResolve(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error)
}

type completionState struct {
	seq                uint64
	cancel             context.CancelFunc
	visible            bool
	incomplete         bool
	editor             *editor.State
	doc                *doc
	path               string
	manager            *langserver.Server
	resultVersion      uint64
	resultOffset       int
	resultStart        int
	resultGeneration   int
	resultSeq          uint64
	displayedSeq       uint64
	originals          map[editor.Completion]lsp.CompletionItem
	ambiguous          map[editor.Completion]bool
	resolved           map[editor.Completion]completionMetadata
	resolveAttempted   map[editor.Completion]bool
	resolveSeq         uint64
	resolveCancel      context.CancelFunc
	resolvePending     bool
	resolveSelected    editor.Completion
	hasResolveSelected bool
	refreshAfterEdit   *completionEditRefresh
}

type completionMetadata struct {
	detail         string
	fallbackDetail string
	documentation  string
}

type completionEditRefresh struct {
	start   int
	version uint64
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
	a.cancelCompletionResolve(cs)
}

func (a *App) cancelCompletions(g *group, st *editor.State) {
	a.cancelCompletionRequest(g)
	cs := a.completionFor(g)
	cs.visible, cs.incomplete = false, false
	cs.editor, cs.doc, cs.path, cs.manager = nil, nil, "", nil
	cs.resultVersion, cs.resultOffset, cs.resultGeneration, cs.resultSeq = 0, 0, 0, 0
	cs.resultStart, cs.refreshAfterEdit = 0, nil
	a.resetCompletionMetadata(cs)
	if st != nil {
		st.ShowCompletions(-1, nil)
	}
}

func (a *App) cancelCompletionResolve(cs *completionState) {
	cs.resolveSeq++
	if cs.resolveCancel != nil {
		cs.resolveCancel()
		cs.resolveCancel = nil
	}
	cs.resolvePending = false
}

func (a *App) resetCompletionMetadata(cs *completionState) {
	a.cancelCompletionResolve(cs)
	cs.originals = nil
	cs.ambiguous = nil
	cs.resolved = nil
	cs.resolveAttempted = nil
	cs.resolveSelected = editor.Completion{}
	cs.hasResolveSelected = false
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

func completionInputBackspace(ev ui.InputEvent) bool {
	return ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyBackspace ||
		ev.Kind == ui.InputCommand && ev.Text == "delete"
}

func refreshCompletionsAfterDeletion(st *editor.State, cs *completionState) {
	refresh := cs.refreshAfterEdit
	cs.refreshAfterEdit = nil
	if refresh == nil || st == nil || st.Buffer == nil || st.OnCompletion == nil ||
		st.Buffer.Version() == refresh.version {
		return
	}
	snap := st.Buffer.Snapshot()
	offset := st.Cursor()
	if refresh.start < 0 || refresh.start >= offset || offset > snap.Len() ||
		snap.Pos(refresh.start).Line != snap.Pos(offset).Line {
		return
	}
	if sels := st.Selections(); len(sels) != 1 || !sels[0].Empty() {
		return
	}
	prefix := snap.Slice(refresh.start, offset)
	if prefix != "" {
		st.OnCompletion(editor.CompletionRequest{Offset: offset, Prefix: prefix, Version: snap.Version()})
	}
}

func completionNavigationInput(ev ui.InputEvent) bool {
	if ev.Kind != ui.InputKeyDown {
		return false
	}
	switch ev.Key {
	case ui.KeyUp, ui.KeyDown, ui.KeyPageUp, ui.KeyPageDown, ui.KeyHome, ui.KeyEnd:
		return true
	default:
		return false
	}
}

func completionAcceptInput(ev ui.InputEvent) bool {
	return ev.Kind == ui.InputKeyDown && (ev.Key == ui.KeyEnter || ev.Key == ui.KeyTab)
}

func (a *App) requestCompletions(g *group, d *doc, st *editor.State, req editor.CompletionRequest) {
	a.cancelCompletionRequest(g)
	cs := a.completionFor(g)
	cs.resultStart = 0
	cs.refreshAfterEdit = nil
	a.resetCompletionMetadata(cs)
	if g != a.focus || g.active != d.path || a.docs[d.path] != d || d.buf == nil ||
		st == nil || st.Buffer != d.buf || st.ReadOnly || !a.workspaceTrusted() {
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
	cs.manager = m
	cs.resultVersion, cs.resultOffset, cs.resultGeneration, cs.resultSeq = version, req.Offset, generation, seq
	cs.resultStart = start
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
		!st.ReadOnly && a.workspaceTrusted()
	if valid {
		sels := st.Selections()
		valid = len(sels) == 1 && sels[0].Empty()
	}
	if !valid {
		if st != nil {
			st.ShowCompletions(-1, nil)
		}
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path, cs.manager = nil, nil, "", nil
		cs.resultVersion, cs.resultOffset, cs.resultGeneration, cs.resultSeq = 0, 0, 0, 0
		cs.resultStart = 0
		a.resetCompletionMetadata(cs)
		return
	}
	if err != nil || result == nil {
		st.ShowCompletions(start, nil)
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path, cs.manager = nil, nil, "", nil
		cs.resultStart = 0
		a.resetCompletionMetadata(cs)
		return
	}
	a.resetCompletionMetadata(cs)
	items, originals, ambiguous := completionItemsWithSources(result.Items, d.buf, req, encoding)
	st.ShowCompletions(start, items)
	cs.originals, cs.ambiguous = originals, ambiguous
	cs.resolved = make(map[editor.Completion]completionMetadata)
	cs.resolveAttempted = make(map[editor.Completion]bool)
	cs.resultVersion, cs.resultOffset, cs.resultGeneration, cs.resultSeq = version, req.Offset, generation, seq
	cs.incomplete = result.IsIncomplete && len(items) > 0
	if len(items) == 0 {
		cs.visible, cs.incomplete = false, false
		cs.editor, cs.doc, cs.path, cs.manager = nil, nil, "", nil
		cs.resultStart = 0
		a.resetCompletionMetadata(cs)
	}
}

func completionItems(items []lsp.CompletionItem, buf *editor.Buffer, req editor.CompletionRequest, encoding string) []editor.Completion {
	completions, _, _ := completionItemsWithSources(items, buf, req, encoding)
	return completions
}

func completionItemsWithSources(items []lsp.CompletionItem, buf *editor.Buffer, req editor.CompletionRequest, encoding string) ([]editor.Completion, map[editor.Completion]lsp.CompletionItem, map[editor.Completion]bool) {
	snap := buf.Snapshot()
	start := req.Offset - len(req.Prefix)
	if start < 0 || req.Offset > snap.Len() {
		return nil, nil, nil
	}
	pos, startPos := snap.Pos(req.Offset), snap.Pos(start)
	if pos.Line != startPos.Line {
		return nil, nil, nil
	}
	line := snap.Line(pos.Line)
	want := lsp.Range{
		Start: lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, startPos.Col, encoding)},
		End:   lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, encoding)},
	}
	out := make([]editor.Completion, 0, len(items))
	originals := make(map[editor.Completion]lsp.CompletionItem, len(items))
	ambiguous := make(map[editor.Completion]bool)
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
			Label: item.Label, Detail: item.DisplayDetail(), Documentation: item.Documentation,
			Insert: insert, FilterText: filter, Kind: completionKind(item.Kind),
		})
		completion := out[len(out)-1]
		if _, exists := originals[completion]; exists {
			delete(originals, completion)
			ambiguous[completion] = true
		} else if !ambiguous[completion] {
			originals[completion] = item
		}
	}
	return out, originals, ambiguous
}

func (a *App) renderCompletions(c *ui.Context, panel ui.Element, menu *editor.CompletionMenu, g *group, st *editor.State) {
	cs := a.completionFor(g)
	items := menu.Items()
	selected := menu.Selected()
	canResolve := a.completionResolveCurrent(g, cs, st) && selected >= 0 && selected < len(items)
	var selectedItem editor.Completion
	if canResolve {
		selectedItem = items[selected]
		a.setCompletionResolveSelection(cs, selectedItem, true)
		a.startCompletionResolve(g, cs, selectedItem)
	} else {
		a.setCompletionResolveSelection(cs, editor.Completion{}, false)
	}

	t := c.Theme()
	ct := menu.Theme()
	font := menu.Font()
	lh := menu.LineHeight()
	if lh <= 0 {
		lh = font.Size * 1.5
	}
	rows := min(len(items), 200)
	details := make([]string, rows)
	labelWidths := make([]float32, rows)
	detailWidths := make([]float32, rows)
	badgeSize, rowGap := max(font.Size, 1), t.Space(2)
	rowPadding := 2 * t.Space(1.5)
	panelPadding := 2*t.Space(1) + 2
	contentWidth := float32(0)
	for i := 0; i < rows; i++ {
		item := items[i]
		details[i] = item.Detail
		if i == selected && canResolve {
			if original, exists := cs.originals[item]; exists && original.Detail == "" {
				if resolved, ok := cs.resolved[item]; ok {
					if resolved.detail != "" {
						details[i] = resolved.detail
					} else if details[i] == "" {
						details[i] = resolved.fallbackDetail
					}
				}
			}
		}
		labelWidths[i], _ = c.MeasureText(0, ui.Span{Text: item.Label, Font: font.Family, Size: font.Size})
		gaps := rowGap
		if details[i] != "" {
			detailWidths[i], _ = c.MeasureText(0, ui.Span{Text: details[i], Font: t.Font, Size: max(font.Size-1, 1)})
			detailWidths[i] += t.Space(0.5)
			gaps += rowGap
		}
		contentWidth = max(contentWidth, rowPadding+badgeSize+gaps+labelWidths[i]+detailWidths[i])
	}
	windowWidth, _ := c.Size()
	_, safeRight, _, safeLeft := c.SafeArea()
	maxPopupWidth := min(float32(600), max(float32(1), windowWidth-safeLeft-safeRight-t.Space(4)))
	minPopupWidth := min(t.Space(60), maxPopupWidth)
	popupWidth := min(max(minPopupWidth, contentWidth+panelPadding), maxPopupWidth)
	for i := range detailWidths {
		maxDetailWidth := popupWidth - panelPadding - rowPadding - badgeSize - 2*rowGap - min(labelWidths[i], t.Space(20))
		detailWidths[i] = min(detailWidths[i], max(float32(0), maxDetailWidth))
	}
	panel.Background(t.Surface).Border(1, t.Border).Radius(t.Radius).
		Shadow(0, 4, 16, 0, ui.RGBA(0, 0, 0, 0.2)).Padding(t.Space(1)).
		Width(popupWidth).MinWidth(minPopupWidth).MaxWidth(maxPopupWidth).MaxHeight(lh*13 + t.Space(4))
	panel.Children(func() {
		ui.Scroll(c).Key("rows").MaxHeight(lh * 9).Children(func() {
			for i, item := range items {
				if i >= rows {
					break
				}
				row := menu.Item(i).Row().FillWidth().Justify(ui.Start).Gap(t.Space(2)).
					AlignItems(ui.Center).Padding(t.Space(0.5), t.Space(1.5)).
					Radius(t.Radius - 2).Height(lh + 2)
				if i == selected {
					row.Background(t.Accent.Alpha(0.18))
				}
				row.Children(func() {
					completionBadgeView(c, badgeSize, item.Kind)
					ui.Text(c, item.Label).Font(font.Family).FontSize(font.Size).
						TextColor(ct.Text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
					detail := details[i]
					if detail != "" {
						ui.Text(c, detail).Font(t.Font).FontSize(max(font.Size-1, 1)).
							TextColor(t.TextMuted).SingleLine().Ellipsis("…").
							Width(detailWidths[i]).Shrink(0).TextAlign(ui.End)
					}
				})
			}
		})
		if selected >= 0 && selected < len(items) {
			documentation := items[selected].Documentation
			if canResolve {
				if resolved, ok := cs.resolved[selectedItem]; ok && documentation == "" {
					documentation = resolved.documentation
				}
			}
			if documentation != "" {
				ui.Box(c).Height(1).Background(t.Border).Margin(t.Space(1), 0)
				ui.Text(c, documentation).FontSize(max(font.Size-1, 1)).
					TextColor(t.TextMuted).MaxWidth(t.Space(130)).
					MaxHeight(lh*3).Padding(t.Space(0.5), t.Space(1.5))
			}
		}
	})
}

func (a *App) completionResolveCurrent(g *group, cs *completionState, st *editor.State) bool {
	if g == nil || cs == nil || st == nil || !cs.visible || g != a.focus ||
		g.active != cs.path || a.docs[cs.path] != cs.doc || cs.doc == nil ||
		cs.doc.buf == nil || cs.editor != st || st.Buffer != cs.doc.buf ||
		cs.manager == nil || a.serverFor(cs.path) != cs.manager ||
		cs.resultSeq == 0 || cs.resultSeq != cs.seq ||
		cs.resultGeneration != a.gen || cs.doc.buf.Version() != cs.resultVersion ||
		st.Cursor() != cs.resultOffset || st.ReadOnly || !a.workspaceTrusted() {
		return false
	}
	selections := st.Selections()
	return len(selections) == 1 && selections[0].Empty()
}

func (a *App) setCompletionResolveSelection(cs *completionState, selected editor.Completion, ok bool) {
	if cs.hasResolveSelected == ok && (!ok || cs.resolveSelected == selected) {
		return
	}
	a.cancelCompletionResolve(cs)
	cs.resolveSelected, cs.hasResolveSelected = selected, ok
}

func (a *App) startCompletionResolve(g *group, cs *completionState, selected editor.Completion) {
	if cs.resolvePending || cs.resolveAttempted[selected] || cs.ambiguous[selected] {
		return
	}
	item, ok := cs.originals[selected]
	if !ok || len(item.Raw) == 0 || (item.Detail != "" && item.Documentation != "") {
		return
	}
	m := cs.manager
	if m == nil {
		return
	}
	ctx, cancel := context.WithTimeout(a.wsCtx, 10*time.Second)
	cs.resolveSeq++
	resolveSeq, resultSeq := cs.resolveSeq, cs.resultSeq
	groupID, path, generation := g.id, cs.path, cs.resultGeneration
	version, offset := cs.resultVersion, cs.resultOffset
	item.Raw = append(item.Raw[:0:0], item.Raw...)
	cs.resolveCancel, cs.resolvePending = cancel, true
	post := a.post.Post
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		var resolved *lsp.CompletionItem
		if err == nil {
			resolver, supported := client.(completionResolveClient)
			if !supported || !resolver.HasCompletionItemResolve() {
				err = fmt.Errorf("completion item resolve is not supported")
			} else {
				resolved, err = resolver.CompletionItemResolve(ctx, item)
			}
		}
		post(func() {
			a.finishCompletionResolve(groupID, path, generation, version, offset, resultSeq, resolveSeq, selected, resolved, err)
		})
	}()
}

func (a *App) finishCompletionResolve(groupID int, path string, generation int, version uint64, offset int, resultSeq, resolveSeq uint64, selected editor.Completion, resolved *lsp.CompletionItem, err error) {
	cs := a.completionForID(groupID)
	if cs == nil || cs.seq != resultSeq || cs.resolveSeq != resolveSeq || !cs.resolvePending ||
		!cs.hasResolveSelected || cs.resolveSelected != selected {
		return
	}
	cs.resolveCancel, cs.resolvePending = nil, false
	if a.focus == nil || a.focus.id != groupID || !a.completionResolveCurrent(a.focus, cs, cs.editor) ||
		cs.path != path || cs.resultGeneration != generation || cs.resultVersion != version || cs.resultOffset != offset {
		return
	}
	cs.resolveAttempted[selected] = true
	if err == nil && resolved != nil {
		if cs.resolved == nil {
			cs.resolved = make(map[editor.Completion]completionMetadata)
		}
		cs.resolved[selected] = completionMetadata{
			detail: resolved.Detail, fallbackDetail: resolved.DisplayDetail(),
			documentation: resolved.Documentation,
		}
	}
}

func completionBadge(kind editor.CompletionKind) (string, ui.Color) {
	purple, blue, orange, green, teal := ui.RGB(0xb0, 0x7c, 0xf0),
		ui.RGB(0x4a, 0x9e, 0xf5), ui.RGB(0xf0, 0x9a, 0x3e),
		ui.RGB(0x4c, 0xbf, 0x7a), ui.RGB(0x2f, 0xb8, 0xc0)
	switch kind {
	case editor.CompletionKeyword:
		return "K", green
	case editor.CompletionFunction:
		return "f", purple
	case editor.CompletionMethod:
		return "m", purple
	case editor.CompletionVariable:
		return "v", green
	case editor.CompletionField:
		return "F", teal
	case editor.CompletionType:
		return "T", blue
	case editor.CompletionModule:
		return "M", ui.RGB(0x8a, 0x8d, 0x98)
	case editor.CompletionConstant:
		return "K", green
	case editor.CompletionSnippet:
		return "S", orange
	default:
		return "•", ui.RGB(0x8a, 0x8d, 0x98)
	}
}

func completionBadgeView(c *ui.Context, size float32, kind editor.CompletionKind) ui.Element {
	glyph, color := completionBadge(kind)
	return ui.Box(c).Size(size, size).Shrink(0).Background(color.Alpha(0.16)).
		Border(1, color.Alpha(0.38)).Radius(3).Center().Children(func() {
		ui.Text(c, glyph).Font("monospace").FontSize(max(size-1, 1)).
			Bold().TextColor(color).SingleLine()
	})
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
