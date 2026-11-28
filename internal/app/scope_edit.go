package app

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/workspace"
	"see/locales"
)

type documentHighlightsProvider interface {
	HasDocumentHighlights() bool
	DocumentHighlights(context.Context, string, lsp.Position) ([]lsp.DocumentHighlight, error)
}

var errDocumentHighlightsUnsupported = errors.New("document highlights unsupported")

type scopeRange struct {
	start, end int
}

func scopeEditText(key string, args map[string]any) string {
	return locales.Catalog.Format(i18n.Locale(), key, args)
}

func (a *App) scopeEditDocument() (*doc, *editor.State) {
	if a.focus == nil {
		return nil, nil
	}
	return a.scopeEditDocumentFor(a.focus, a.docs[a.activePath()], a.activeEditor())
}

func (a *App) scopeEditDocumentFor(g *group, d *doc, st *editor.State) (*doc, *editor.State) {
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

func (a *App) editAllInScope() {
	g := a.focus
	if g == nil {
		a.editAllInScopeFor(nil, nil, nil)
		return
	}
	a.editAllInScopeFor(g, a.docs[a.activePath()], a.activeEditor())
}

func (a *App) scopeEditCommandEnabled(g *group, d *doc, st *editor.State) bool {
	d, st = a.scopeEditDocumentFor(g, d, st)
	return d != nil && st != nil && a.serverFor(d.path) != nil
}

func (a *App) editAllInScopeFor(g *group, d *doc, st *editor.State) {
	if a.scopeCancel != nil {
		a.cancelScopeEditRequest()
	}
	a.scopeSeq++
	request := a.scopeSeq
	d, st = a.scopeEditDocumentFor(g, d, st)
	if d == nil || st == nil {
		a.flash(scopeEditText("scopeEditSourceOnly", nil))
		return
	}
	m := a.serverFor(d.path)
	if m == nil {
		a.flash(locales.MsgNoLanguageServerIsConfiguredForThisFile())
		return
	}
	if !a.workspaceTrusted() {
		a.trustAsk = true
		a.afterTrust = func() { a.editAllInScopeFor(g, d, st) }
		return
	}
	snap := d.buf.Snapshot()
	docURI := d.uri
	cursor := st.Cursor()
	name, offset, ok := scopeIdentifierAt(snap, cursor)
	if !ok {
		a.flash(scopeEditText("scopeEditIdentifier", nil))
		return
	}
	selections := slices.Clone(st.Selections())
	gen, root, path, active := a.gen, a.root, d.path, g.active
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.scopeCancel = cancel
	a.scopePendingState, a.scopePendingGroup, a.scopePendingPath = st, g, path
	go func() {
		defer cancel()
		client, err := m.Get(ctx)
		var ranges []editor.Selection
		if err == nil {
			provider, supported := client.(documentHighlightsProvider)
			if !supported || !provider.HasDocumentHighlights() {
				err = errDocumentHighlightsUnsupported
			} else if err = m.Sync(ctx, client, docURI, snap); err == nil {
				pos := scopeLSPPosition(snap, offset, client.Encoding())
				var highlights []lsp.DocumentHighlight
				highlights, err = provider.DocumentHighlights(ctx, docURI, pos)
				if err == nil {
					ranges, err = scopeSelections(snap, highlights, name, cursor, client.Encoding())
				}
			}
		}
		a.post.Post(func() {
			if a.scopeSeq != request {
				return
			}
			a.scopeCancel = nil
			a.scopePendingState, a.scopePendingGroup, a.scopePendingPath = nil, nil, ""
			if a.gen != gen || a.root != root || a.layout == nil || a.layout.find(g) == nil ||
				g.active != active || g.eds[path] != st || a.docs[path] != d || d.path != path ||
				d.uri != docURI || st.Buffer != d.buf ||
				d.buf.Version() != snap.Version() || !slices.Equal(st.Selections(), selections) ||
				a.serverFor(path) != m || !a.cfg.IsTrusted(root) ||
				client != nil && m.Current() != client {
				return
			}
			switch {
			case errors.Is(err, errDocumentHighlightsUnsupported):
				a.flash(scopeEditText("scopeEditUnsupported", nil))
			case errors.Is(err, context.Canceled):
				return
			case err != nil:
				a.fail(scopeEditText("scopeEditFailed", map[string]any{"error": err.Error()}))
			case len(ranges) == 0:
				a.flash(scopeEditText("scopeEditEmpty", nil))
			default:
				st.SetSelections(ranges...)
				a.scopeModeState, a.scopeModeGroup, a.scopeModePath = st, g, path
			}
		})
	}()
}

func (a *App) cancelScopeEditRequest() {
	if a.scopeCancel == nil {
		return
	}
	a.scopeSeq++
	a.scopeCancel()
	a.scopeCancel = nil
	a.scopePendingState, a.scopePendingGroup, a.scopePendingPath = nil, nil, ""
}

func (a *App) scopeEditStatus(st *editor.State) string {
	if a.scopeCancel != nil && a.scopePendingState == st &&
		a.scopePendingGroup == a.focus && a.scopePendingPath == a.activePath() {
		return scopeEditText("scopeEditSearching", nil)
	}
	if a.scopeModeState != st || a.scopeModeGroup != a.focus || a.scopeModePath != a.activePath() {
		return ""
	}
	count := len(st.Selections())
	if count < 2 {
		a.scopeModeState, a.scopeModeGroup, a.scopeModePath = nil, nil, ""
		return ""
	}
	return scopeEditText("scopeEditActive", map[string]any{"count": count})
}

func scopeLSPPosition(snap editor.Snapshot, offset int, encoding string) lsp.Position {
	pos := snap.Pos(offset)
	return lsp.Position{
		Line:      pos.Line,
		Character: lsp.ColumnToLSP(snap.Line(pos.Line), pos.Col, encoding),
	}
}

func scopeIdentifierAt(snap editor.Snapshot, cursor int) (string, int, bool) {
	if cursor < 0 || cursor > snap.Len() {
		return "", 0, false
	}
	pos := snap.Pos(cursor)
	text := snap.Line(pos.Line)
	name, start, ok := lineIdentifierAt(text, pos.Col)
	return name, cursor - pos.Col + start, ok
}

func lineIdentifierAt(text string, cursor int) (string, int, bool) {
	if cursor < 0 || cursor > len(text) || !runeBoundary(text, cursor) {
		return "", 0, false
	}
	at := cursor
	if at == len(text) || at < len(text) && !identifierContinueAt(text, at) {
		if at == 0 {
			return "", 0, false
		}
		at = previousRuneStart(text, at)
		if !identifierContinueAt(text, at) {
			return "", 0, false
		}
	}
	start := at
	for start > 0 {
		prev := previousRuneStart(text, start)
		if !identifierContinueAt(text, prev) {
			break
		}
		start = prev
	}
	end := at
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !identifierContinue(r) {
			break
		}
		end += size
	}
	first, _ := utf8.DecodeRuneInString(text[start:end])
	if !identifierStart(first) {
		return "", 0, false
	}
	return text[start:end], start, true
}

func identifierContinueAt(text string, offset int) bool {
	if offset < 0 || offset >= len(text) || !runeBoundary(text, offset) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text[offset:])
	return identifierContinue(r)
}

func identifierStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r) || unicode.Is(unicode.Nl, r)
}

func identifierContinue(r rune) bool {
	return identifierStart(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r) ||
		unicode.Is(unicode.Mc, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Pc, r)
}

func previousRuneStart(text string, offset int) int {
	_, size := utf8.DecodeLastRuneInString(text[:offset])
	return offset - size
}

func runeBoundary(text string, offset int) bool {
	return offset >= 0 && offset <= len(text) && (offset == len(text) || utf8.RuneStart(text[offset]))
}

func scopeSelections(snap editor.Snapshot, highlights []lsp.DocumentHighlight, name string, cursor int, encoding string) ([]editor.Selection, error) {
	if len(highlights) == 0 {
		return nil, nil
	}
	ranges := make([]scopeRange, 0, len(highlights))
	seen := make(map[scopeRange]struct{}, len(highlights))
	for _, highlight := range highlights {
		start, ok := scopeOffset(snap, highlight.Range.Start, encoding)
		if !ok {
			return nil, fmt.Errorf("invalid start position")
		}
		end, ok := scopeOffset(snap, highlight.Range.End, encoding)
		if !ok || start >= end {
			return nil, fmt.Errorf("invalid end position")
		}
		r := scopeRange{start, end}
		if _, ok := seen[r]; ok {
			continue
		}
		if !utf8.ValidString(snap.Slice(start, end)) || snap.Slice(start, end) != name {
			return nil, fmt.Errorf("highlight does not match the identifier")
		}
		seen[r] = struct{}{}
		ranges = append(ranges, r)
	}
	sort.Slice(ranges, func(i, j int) bool {
		if ranges[i].start != ranges[j].start {
			return ranges[i].start < ranges[j].start
		}
		return ranges[i].end < ranges[j].end
	})
	primary := -1
	for i, r := range ranges {
		if i > 0 && ranges[i-1].end > r.start {
			return nil, fmt.Errorf("overlapping highlights")
		}
		if r.start <= cursor && cursor <= r.end {
			if primary >= 0 {
				return nil, fmt.Errorf("multiple highlights contain the cursor")
			}
			primary = i
		}
	}
	if primary < 0 {
		return nil, fmt.Errorf("no highlight contains the cursor")
	}
	out := make([]editor.Selection, 0, len(ranges))
	for i, r := range ranges {
		if i != primary {
			out = append(out, editor.Selection{Anchor: r.start, Head: r.end})
		}
	}
	r := ranges[primary]
	out = append(out, editor.Selection{Anchor: r.start, Head: r.end})
	return out, nil
}

func scopeOffset(snap editor.Snapshot, pos lsp.Position, encoding string) (int, bool) {
	if pos.Line < 0 || pos.Line >= snap.LineCount() || pos.Character < 0 {
		return 0, false
	}
	line := snap.Line(pos.Line)
	col := lsp.ColumnFromLSP(line, pos.Character, encoding)
	if col < 0 || col > len(line) || !runeBoundary(line, col) ||
		lsp.ColumnToLSP(line, col, encoding) != pos.Character {
		return 0, false
	}
	offset := snap.Offset(editor.Pos{Line: pos.Line, Col: col})
	if offset < 0 || offset > snap.Len() {
		return 0, false
	}
	return offset, true
}
