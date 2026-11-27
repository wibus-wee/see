package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/locales"
)

// outlineDebounce lets typing settle before the outline asks for symbols again.
const outlineDebounce = 350 * time.Millisecond

type outlineKey struct {
	path    string
	doc     *doc
	version uint64
	server  *langserver.Server
}

type outlineState struct {
	query string
	list  ui.ListState

	// requested is the document the latest request is for; shown is what
	// symbols describe, kept on screen while a newer request runs.
	requested, shown outlineKey
	seq              int
	cancel           context.CancelFunc
	loading          bool
	err              error
	symbols          []lsp.DocumentSymbol
	enc              string
	followed         int
}

type outlineRow struct {
	index  int
	parent string
}

func outlineText(key string) string { return locales.Catalog.Format(i18n.Locale(), key, nil) }

// refreshOutline requests the active document's symbols when it, its text or
// its language server differ from the latest request.
func (a *App) refreshOutline() {
	o := &a.outline
	path := a.activePath()
	d := a.docs[path]
	key := outlineKey{path: path, doc: d}
	if d != nil && d.buf != nil {
		key.version = d.buf.Version()
	}
	if d != nil && d.buf != nil && a.activeEditor() != nil {
		key.server = a.serverFor(path)
	}
	if key == o.requested {
		return
	}
	if o.cancel != nil {
		o.cancel()
		o.cancel = nil
	}
	o.requested, o.seq = key, o.seq+1
	if o.shown.path != path || o.shown.doc != d || o.shown.server != key.server {
		o.shown, o.symbols, o.err, o.followed = outlineKey{}, nil, nil, -1
		o.list = ui.ListState{}
	}
	// Asking for symbols would start the server: only a trusted
	// workspace runs one, as hoverAt's check does.
	if d == nil || d.buf == nil || a.activeEditor() == nil || key.server == nil || a.wsCtx == nil || !a.workspaceTrusted() {
		o.loading = false
		return
	}
	delay := time.Duration(0)
	if o.shown.doc == d {
		delay = outlineDebounce
	}
	m, snap, docURI, gen, seq := key.server, d.buf.Snapshot(), d.uri, a.gen, o.seq
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	o.cancel, o.loading = cancel, true
	unsupported := outlineText("fileSymbolsUnsupported")
	go func() {
		defer cancel()
		var symbols []lsp.DocumentSymbol
		var enc string
		err := ctx.Err()
		if delay > 0 {
			select {
			case <-ctx.Done():
				err = ctx.Err()
			case <-time.After(delay):
			}
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
			if a.gen != gen || o.seq != seq || o.requested != key || errors.Is(err, context.Canceled) {
				return
			}
			o.cancel, o.loading = nil, false
			o.shown, o.err = key, err
			if err == nil {
				o.symbols, o.enc = symbols, enc
			}
		})
	}()
}

// outlineRows lists the symbols the filter matches, each with the name of
// the symbol it is in, which a filtered row shows in place of its nesting.
func (a *App) outlineRows() []outlineRow {
	o := &a.outline
	query := strings.ToLower(strings.TrimSpace(o.query))
	var rows []outlineRow
	var parents []string
	for i, s := range o.symbols {
		parents = append(parents[:min(s.Depth, len(parents))], s.Name)
		if query != "" && !strings.Contains(strings.ToLower(s.Name), query) {
			continue
		}
		parent := ""
		if len(parents) > 1 {
			parent = parents[len(parents)-2]
		}
		rows = append(rows, outlineRow{index: i, parent: parent})
	}
	return rows
}

// currentSymbol is the innermost symbol around the active editor's cursor.
func (a *App) currentSymbol() int {
	o := &a.outline
	st := a.activeEditor()
	d := a.docs[a.activePath()]
	if st == nil || d == nil || o.shown.doc != d || len(o.symbols) == 0 {
		return -1
	}
	pos := st.CursorPos()
	at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(st.Buffer.Line(pos.Line), pos.Col, o.enc)}
	current := -1
	for i, s := range o.symbols {
		if !positionBefore(at, s.Range.Start) && positionBefore(at, s.Range.End) && (current < 0 || s.Depth >= o.symbols[current].Depth) {
			current = i
		}
	}
	return current
}

func positionBefore(a, b lsp.Position) bool {
	return a.Line < b.Line || a.Line == b.Line && a.Character < b.Character
}

func (a *App) openOutlineSymbol(i int) {
	o := &a.outline
	if i < 0 || i >= len(o.symbols) || a.docs[o.shown.path] != o.shown.doc || a.focus == nil {
		return
	}
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	pos := o.symbols[i].Position
	a.openIn(a.focus, o.shown.path, goTo{lsp: &pos, enc: o.enc})
}

func (a *App) outlineNavigator(c *ui.Context) {
	a.refreshOutline()
	o := &a.outline
	rows := a.outlineRows()
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, outlineText("outlineTitle")).Bold().SingleLine().Shrink(0)
			if o.shown.path != "" {
				ui.Text(c, filepath.Base(o.shown.path)).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
			}
			ui.Box(c).Grow(1)
			if o.loading && len(o.symbols) > 0 {
				ui.Text(c, outlineText("outlineUpdating")).FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(0)
			} else if len(o.symbols) > 0 {
				ui.Text(c, fmt.Sprint(len(o.symbols))).FontSize(11).TextColor(colors(c).Dim).SingleLine().Shrink(0)
			}
		})
		ui.SearchField(c, &o.query).Label(outlineText("filterSymbols")).Placeholder(outlineText("filterSymbols")).FillWidth()
	})
	if len(rows) == 0 {
		ui.Text(c, a.outlineEmptyText()).TextColor(colors(c).Muted).FontSize(12).Wrap().Padding(6, 10).FillWidth()
		return
	}
	current := a.currentSymbol()
	if current != o.followed {
		o.followed = current
		if strings.TrimSpace(o.query) == "" && current >= 0 {
			o.list.ScrollIntoView(current)
		}
	}
	filtering := strings.TrimSpace(o.query) != ""
	ui.List(c, &o.list, len(rows)).ItemKey(func(i int) any { return rows[i].index }).Grow(1).FillWidth().Padding(0, 4).Label(outlineText("outlineTitle")).Rows(func(item ui.ListRow) {
		c := item.Context
		r := rows[item.Index]
		s := o.symbols[r.index]
		depth := s.Depth
		if filtering {
			depth = 0
		}
		line := fmt.Sprint(s.Position.Line + 1)
		kind, color := symbolKind(c, s.Kind)
		row := ui.ButtonBase(c).FillWidth().Padding(4, 6, 4, 6+float32(min(depth, 8))*14).Radius(5).
			Label(s.Name + " — " + outlineKindName(s.Kind) + ", " + locales.Catalog.Format(i18n.Locale(), "outlineLine", map[string]any{"line": line})).Tooltip(s.Name + " " + s.Detail)
		if r.index == current {
			row.Background(colors(c).SelectedRow)
		} else if row.Hovered() {
			row.Background(colors(c).Pill)
		}
		i := r.index
		row.OnClick(func() { a.openOutlineSymbol(i) })
		row.Children(func() {
			ui.Row(c).FillWidth().Gap(7).AlignItems(ui.Center).Children(func() {
				ui.Box(c).Width(16).Height(16).Radius(4).Shrink(0).Background(color.Alpha(0.16)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
					ui.Text(c, kind).FontSize(10).Bold().TextColor(color).SingleLine()
				})
				ui.Text(c, s.Name).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
				if filtering && r.parent != "" {
					ui.Text(c, r.parent).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").MinWidth(0).Shrink(100)
				} else if s.Detail != "" && !strings.Contains(s.Detail, "\n") && !callableKind(s.Kind) {
					ui.Text(c, s.Detail).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").MinWidth(0).Shrink(100)
				}
				ui.Box(c).Grow(1)
				ui.Text(c, line).FontSize(10).TextColor(colors(c).Dim).SingleLine().Shrink(0)
			})
		})
	})
}

func (a *App) outlineEmptyText() string {
	o := &a.outline
	switch {
	case o.requested.doc == nil || o.requested.doc.buf == nil || a.activeEditor() == nil:
		return outlineText("outlineOpenText")
	case o.requested.server == nil:
		return locales.MsgNoLanguageServerIsConfiguredForThisFile()
	case len(o.symbols) > 0:
		return outlineText("noMatchingSymbols")
	case o.loading:
		return outlineText("outlineLoading")
	case o.err != nil:
		return o.err.Error()
	}
	return outlineText("outlineEmpty")
}

// symbolKind gives a symbol kind's badge letter and color.
func symbolKind(c *ui.Context, kind int) (string, ui.Color) {
	purple, blue, orange, green, teal := ui.RGB(0xb0, 0x7c, 0xf0), ui.RGB(0x4a, 0x9e, 0xf5), ui.RGB(0xf0, 0x9a, 0x3e), ui.RGB(0x4c, 0xbf, 0x7a), ui.RGB(0x2f, 0xb8, 0xc0)
	switch kind {
	case 2, 3, 4: // module, namespace, package
		return "M", colors(c).Muted
	case 5: // class
		return "C", orange
	case 6: // method
		return "m", purple
	case 7, 8: // property, field
		return "F", teal
	case 9: // constructor
		return "c", purple
	case 10: // enum
		return "E", orange
	case 11: // interface
		return "I", blue
	case 12: // function
		return "f", purple
	case 13: // variable
		return "v", green
	case 14: // constant
		return "K", green
	case 22: // enum member
		return "e", teal
	case 23: // struct
		return "S", orange
	case 24: // event
		return "!", orange
	case 25: // operator
		return "o", purple
	case 26: // type parameter
		return "T", blue
	}
	return "•", colors(c).Muted
}

func outlineKindName(kind int) string {
	names := map[int]string{2: "outlineKindModule", 3: "outlineKindModule", 4: "outlineKindModule", 5: "outlineKindClass", 6: "outlineKindMethod", 7: "outlineKindField", 8: "outlineKindField", 9: "outlineKindConstructor", 10: "outlineKindEnum", 11: "outlineKindInterface", 12: "outlineKindFunction", 13: "outlineKindVariable", 14: "outlineKindConstant", 22: "outlineKindEnumMember", 23: "outlineKindStruct", 26: "outlineKindTypeParameter"}
	if key, ok := names[kind]; ok {
		return outlineText(key)
	}
	return outlineText("outlineKindSymbol")
}

// callableKind reports a method, constructor or function, whose signature
// would crowd out its name.
func callableKind(kind int) bool { return kind == 6 || kind == 9 || kind == 12 }
