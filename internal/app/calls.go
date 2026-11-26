package app

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/uri"
	"see/internal/workspace"
	"see/locales"
)

// callDir is the tree's direction: Incoming grows toward callers,
// Outgoing toward callees, a switch of which restarts the tree.
type callDir int

const (
	callsIncoming callDir = iota
	callsOutgoing
)

// calls is the call hierarchy panel's state: the tree of items, which
// direction it grows, and the requests whose answers fill it.
type calls struct {
	types      bool
	tree       ui.OutlineState[int]
	row        int
	sel        int // direction the segmented control shows
	split      float32
	dir        callDir
	items      []lsp.CallItem // items prepared at the anchor
	roots      []int
	nodes      []callNode
	seq        int64
	cancel     context.CancelFunc
	server     *langserver.Server
	anchor     callAnchor
	gen        int                       // the workspace the tree belongs to
	preview    *editor.State             // the peek code view of the selected row
	previewFor int                       // the node it shows
	buffers    map[string]*editor.Buffer // files read for previews
	docVersion uint64                    // the anchor buffer the tree is of
}

type callAnchor struct {
	path string
	uri  string
	pos  lsp.Position
	enc  string
	name string // what the root row shows
}

// A callNode is one row of the tree: an item, its depth, the edge that
// put it under its parent, and what its children know so far.
type callNode struct {
	item        lsp.CallItem
	parent      int // index in nodes of the node that expanded it, -1 roots
	depth       int
	edge        *lsp.CallEdge // nil on the root
	children    []int
	status      int   // its status row's index, -1 until asked
	fetched     bool  // the server answered this level
	pending     bool  // a request for it runs
	err         error // its request failed
	empty       bool  // the server answered with no calls
	leaf        bool  // it can have no calls, as the anchor file's item
	unsupported bool  // the direction's row, when the server has none
}

// callClient is the part of a language client call hierarchy needs.
type callClient interface {
	PrepareCallHierarchy(context.Context, string, lsp.Position) ([]lsp.CallItem, error)
	IncomingCalls(context.Context, lsp.CallItem) ([]lsp.CallEdge, error)
	OutgoingCalls(context.Context, lsp.CallItem) ([]lsp.CallEdge, error)
	HasCallHierarchy() bool
}

// errCallsUnsupported is a server that cannot answer call hierarchy.
var errCallsUnsupported = errors.New("call hierarchy unsupported")

// callNodeIDs give each tree node a stable identity across appends.
var callNodeID atomic.Int64

func (a *App) callsInit() {
	cs := &a.calls
	cs.row = -1
	cs.split = 40
	cs.tree.List.Selected = &cs.row
}

// showCallHierarchy opens the panel's Calls view on the symbol under
// the active editor's caret, as the command, key and menu entry run it.
func (a *App) showCallHierarchy() {
	a.showCallHierarchyAt(a.focus)
}

// showCallHierarchyAt asks the hierarchy of the group g's anchor:
// where its editor's caret is, from the command; where the pointer
// last was, from the Related Items menu.
func (a *App) showCallHierarchyAt(g *group) {
	a.showHierarchyAt(g, false)
}

func (a *App) showHierarchyAt(g *group, types bool) {
	if g == nil {
		return
	}
	d := a.docs[g.active]
	st := g.eds[g.active]
	if d == nil || d.buf == nil || st == nil || d.uri == "" {
		return
	}
	m := a.serverFor(d.path)
	if m == nil {
		a.note(locales.MsgNoLanguageServerIsConfiguredForThisFile(), false)
		return
	}
	if !a.cfg.IsTrusted(a.root) {
		a.trustAsk = true
		a.afterTrust = func() { a.showHierarchyAt(g, types) }
		return
	}
	cs := &a.calls
	a.cancelCalls()
	cs.types = types
	a.term.maximized = false
	a.term.open = true
	a.term.view = panelCalls
	snap := d.buf.Snapshot()
	pos := st.CursorPos()
	anchor := callAnchor{path: d.path, uri: d.uri}
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	cs.cancel = cancel
	cs.seq++
	cs.server = m
	cs.anchor = anchor
	cs.gen = a.gen
	cs.tree.Open = ui.Selection[int]{}
	cs.nodes = nil
	cs.roots = nil
	cs.items = nil
	cs.preview = nil
	cs.buffers = nil
	cs.docVersion = 0
	cs.dir = callDir(cs.sel)
	a.finding = true
	a.findingLabel = a.hierarchyText("Preparing")
	seq, gen := cs.seq, a.gen
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		var items []lsp.CallItem
		if err == nil {
			err = m.Sync(ctx, c, d.uri, snap)
		}
		if err == nil {
			anchor.enc = c.Encoding()
			anchor.pos = lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(snap.Line(pos.Line), pos.Col, anchor.enc)}
			cc, ok := hierarchyClient(c, types)
			if !ok || !cc.HasCallHierarchy() {
				err = errCallsUnsupported
			} else {
				items, err = cc.PrepareCallHierarchy(ctx, d.uri, anchor.pos)
			}
		}
		a.post.Post(func() {
			// A buffer that moved on makes the answer describe text that
			// is gone, so it drops with a moved generation or request.
			if a.gen != gen || cs.seq != seq || a.docs[d.path] == nil || d.buf == nil || d.buf.Version() != snap.Version() {
				return
			}
			cs.docVersion = snap.Version()
			a.finding = false
			cs.cancel = nil
			cs.anchor = anchor
			if err != nil {
				a.callsMessage(err)
				return
			}
			a.callsBuilt(items)
		})
	}()
}

// callsBuilt roots the tree at the prepared items and expands the
// first level, so a hierarchy opens showing calls, not a bare symbol.
func (a *App) callsBuilt(items []lsp.CallItem) {
	cs := &a.calls
	cs.items = items
	var root callNode
	if len(items) == 1 {
		cs.anchor.name = items[0].Name
		root.item = items[0]
	} else {
		cs.anchor.name = filepath.Base(cs.anchor.path)
	}
	root.parent, root.status = -1, -1
	cs.roots = []int{a.addCallNode(root)}
	a.expandCall(cs.roots[0])
}

// callsMessage puts a whole-panel message where the tree would be: no
// item at the anchor, a failed prepare, or no call hierarchy support.
func (a *App) callsMessage(err error) {
	cs := &a.calls
	n := callNode{parent: -1, status: -1, fetched: true, empty: true, err: err, unsupported: errors.Is(err, errCallsUnsupported)}
	cs.nodes = nil
	cs.roots = []int{a.addCallNode(n)}
}

// addCallNode appends a node and returns its key: the node's index in
// nodes, stable as nodes only ever grows.
func (a *App) addCallNode(n callNode) int {
	cs := &a.calls
	cs.nodes = append(cs.nodes, n)
	return len(cs.nodes) - 1
}

// expandCall asks the children of a row whose expansion the user
// opened; one already answered, loading or failed returns, a failure
// only retries through its row's Retry.
func (a *App) expandCall(k int) {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := &cs.nodes[k]
	if n.fetched || n.pending || n.leaf || n.empty || n.err != nil {
		return
	}
	m := cs.server
	if m == nil {
		return
	}
	n.pending = true
	cs.tree.Open.Add(k)
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	seq, gen := cs.seq, a.gen
	dir := cs.dir
	types := cs.types
	item := n.item
	items := cs.items
	go func() {
		var edges []lsp.CallEdge
		var err error
		c, err := m.Get(ctx)
		if err == nil {
			cc, ok := hierarchyClient(c, types)
			switch {
			case !ok || !cc.HasCallHierarchy():
				err = errCallsUnsupported
			case item.Name == "" && item.URI == "":
				// The root row's children are its prepared items' calls.
				if len(items) == 0 {
					err = lsp.ErrNoCallItem
				} else {
					for _, it := range items {
						var es []lsp.CallEdge
						es, err = callsOf(ctx, cc, dir, it)
						if err != nil {
							break
						}
						for i := range es {
							es[i].Item.ID = it.ID
						}
						edges = append(edges, es...)
					}
				}
			default:
				edges, err = callsOf(ctx, cc, dir, item)
			}
		}
		cancel()
		a.post.Post(func() {
			if a.gen != gen || cs.seq != seq || a.callsDocMoved() {
				return
			}
			n := &cs.nodes[k]
			n.pending = false
			switch {
			case err != nil:
				n.err = err
				n.unsupported = errors.Is(err, errCallsUnsupported)
			case len(edges) == 0:
				n.fetched, n.empty = true, true
			default:
				depth := n.depth + 1
				for i := range edges {
					e := edges[i]
					// addCallNode grows nodes, so index back in each time.
					cs.nodes[k].children = append(cs.nodes[k].children, a.addCallNode(callNode{item: e.Item, parent: k, status: -1, depth: depth, edge: &e}))
				}
				cs.nodes[k].fetched = true
			}
		})
	}()
}

// callsDocMoved reports whether the anchor's buffer has changed since
// the tree's prepare, after which the answers no longer describe it.
func (a *App) callsDocMoved() bool {
	cs := &a.calls
	if cs.docVersion == 0 {
		return false
	}
	d := a.docs[cs.anchor.path]
	return d == nil || d.buf == nil || d.buf.Version() != cs.docVersion
}

// callsOf answers a direction's calls of an item.
func callsOf(ctx context.Context, cc callClient, dir callDir, item lsp.CallItem) ([]lsp.CallEdge, error) {
	if dir == callsOutgoing {
		return cc.OutgoingCalls(ctx, item)
	}
	return cc.IncomingCalls(ctx, item)
}

// callsChildren are the rows under a row, as the outline asks: its
// items once answered, or one status row while it loads, fails or is
// empty.
func (a *App) callsChildren(k int) []int {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return nil
	}
	n := &cs.nodes[k]
	if cs.types && !n.fetched && !n.pending && !n.leaf && n.err == nil && !n.empty {
		return []int{a.callStatus(k)}
	}
	if n.pending || n.err != nil || n.empty {
		return []int{a.callStatus(k)}
	}
	return n.children
}

// callStatus is the status row under a node: a spinner while it loads,
// its error and Retry on failure, a muted no-calls line when empty. It
// lives outside the children so an answer does not have to remove it.
func (a *App) callStatus(k int) int {
	cs := &a.calls
	if cs.nodes[k].status >= 0 {
		return cs.nodes[k].status
	}
	i := a.addCallNode(callNode{parent: k, status: -1, depth: cs.nodes[k].depth + 1, leaf: true, item: lsp.CallItem{Name: statusRow}})
	cs.nodes[k].status = i
	return i
}

const statusRow = "\x00status"

// switchCallDir turns the tree to the other direction: the same anchor
// answers it, so only the expansions start over.
func (a *App) switchCallDir(dir callDir) {
	cs := &a.calls
	if cs.dir == dir {
		return
	}
	a.cancelCalls()
	cs.dir = dir
	cs.tree.Open = ui.Selection[int]{}
	cs.preview = nil
	if len(cs.items) > 0 {
		items := cs.items
		cs.items = nil
		cs.nodes = nil
		cs.roots = nil
		a.callsBuilt(items)
	} else {
		n := callNode{parent: -1, status: -1, fetched: true, empty: true}
		cs.nodes = nil
		cs.roots = []int{a.addCallNode(n)}
	}
}

func (a *App) cancelCalls() {
	cs := &a.calls
	if cs.cancel != nil {
		cs.cancel()
		cs.cancel = nil
	}
	cs.seq++
}

// callsPanel is the panel's Calls view: the segmented direction switch
// over a tree of calls beside a code preview of the selected row.
func (a *App) callsPanel(c *ui.Context) {
	cs := &a.calls
	ui.Row(c).FillWidth().Padding(6, 10, 4, 10).Gap(8).AlignItems(ui.Center).Children(func() {
		seg := ui.SegmentedBase(c, &cs.sel, 2)
		seg.Track.Children(func() {
			a.callSegment(c, seg.Segment(0), a.hierarchyText("Incoming"), cs.sel == 0)
			a.callSegment(c, seg.Segment(1), a.hierarchyText("Outgoing"), cs.sel == 1)
		})
		if cs.sel != int(cs.dir) {
			a.switchCallDir(callDir(cs.sel))
		}
		if a.finding && a.findingLabel != "" {
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				ui.Spinner(c)
				ui.Text(c, a.findingLabel).FontSize(11).TextColor(colors(c).Muted)
			})
		}
		ui.Box(c).Grow(1)
		ui.Text(c, a.callAnchorLabel()).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
	})
	if len(cs.roots) == 0 {
		return
	}
	cs.tree.Indent = 12
	ui.SplitPercent(c.Key("call-hierarchy-panes"), &cs.split, func() {
		ui.Column(c).FillHeight().Clip().Children(func() {
			if cs.preview != nil {
				editor.View(c.Key("call-code"), cs.preview).Grow(1).FillWidth()
			} else {
				ui.Column(c).Grow(1).FillWidth().Center().Children(func() {
					ui.Text(c, a.hierarchyText("SelectToPreview")).FontSize(12).TextColor(colors(c).Muted)
				})
			}
		})
	}, func() {
		ui.Column(c).FillHeight().BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Children(func() {
			outline := ui.Outline(c, &cs.tree, cs.roots, a.callsChildren, func(k int) { a.callRow(c, k) }).Grow(1).FillWidth().Label(a.hierarchyText(""))
			if outline.Submitted() && cs.row >= 0 && cs.row < cs.tree.Rows() {
				a.openCallRow(cs.tree.Item(cs.row))
			}
			if cs.types {
				for k := 0; k < len(cs.nodes); k++ {
					if cs.tree.Open.Has(k) {
						a.expandCall(k)
					}
				}
			}
		})
	}).Grow(1).FillHeight()
	a.callSelection()
}

// callSegment is one direction's label in the switch.
func (a *App) callSegment(c *ui.Context, seg ui.Element, label string, on bool) {
	seg.Padding(2, 10).Radius(4)
	if on {
		seg.Background(colors(c).SelectedRow)
	}
	seg.Children(func() {
		ui.Text(c, label).FontSize(12)
	})
}

// callAnchorLabel is the anchor's name and file the tree answers of.
func (a *App) callAnchorLabel() string {
	cs := &a.calls
	if cs.anchor.name == "" {
		return ""
	}
	base := filepath.Base(cs.anchor.path)
	name := cs.anchor.name
	if cs.types && len(cs.items) == 1 {
		item := cs.items[0]
		name = hierarchyTypeName(item)
		if p, err := uri.ToPath(item.URI); err == nil {
			base = filepath.Base(p)
		}
	}
	if cs.anchor.name == base {
		return base
	}
	return name + " · " + base
}

// callSelection previews the selected row's call site, as relatedView
// previews a reference: the file's buffer with the call line lit.
func (a *App) callSelection() {
	cs := &a.calls
	if cs.row < 0 || cs.row >= cs.tree.Rows() {
		return
	}
	k := cs.tree.Item(cs.row)
	if k == cs.previewFor && cs.preview != nil {
		return
	}
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := &cs.nodes[k]
	if n.item.Name == statusRow || n.item.URI == "" {
		return
	}
	p, err := uri.ToPath(n.item.URI)
	if err != nil {
		return
	}
	cs.previewFor = k
	buf := cs.buffers[p]
	if buf == nil {
		a.loadCallBuffer(p)
		return
	}
	if cs.preview == nil || cs.preview.Buffer != buf {
		cs.preview = &editor.State{Buffer: buf, ReadOnly: true, Label: a.hierarchyText("Preview"), Theme: a.edTheme}
		a.applyEditorPrefs(cs.preview)
	}
	rng := a.callSiteRange(n)
	line := min(max(rng.Start.Line, 0), buf.LineCount()-1)
	col := lsp.ColumnFromLSP(buf.Line(line), rng.Start.Character, cs.anchor.enc)
	cs.preview.LineBackgrounds = []editor.LineBackground{{First: line, Last: line, Color: a.colors().Pill}}
	cs.preview.GoTo(editor.Pos{Line: line, Col: col})
	if rng.End.Line == rng.Start.Line && rng.End.Character > rng.Start.Character {
		end := lsp.ColumnFromLSP(buf.Line(line), rng.End.Character, cs.anchor.enc)
		cs.preview.TextBackgrounds = []editor.TextBackground{{Start: buf.Offset(editor.Pos{Line: line, Col: col}), End: buf.Offset(editor.Pos{Line: line, Col: end}), Color: a.colors().Match}}
	} else {
		cs.preview.TextBackgrounds = nil
	}
}

// callSiteRange is where a row's call happens: the first range its
// edge names, the item's own selection range on the root.
func (a *App) callSiteRange(n *callNode) lsp.Range {
	if n.edge != nil && len(n.edge.FromRanges) > 0 {
		return n.edge.FromRanges[0]
	}
	return n.item.SelectionRange
}

// loadCallBuffer reads a file for a preview, from its open document
// when it is one, else from disk, as loadRelatedPreviews does.
func (a *App) loadCallBuffer(path string) {
	cs := &a.calls
	if cs.buffers == nil {
		cs.buffers = map[string]*editor.Buffer{}
	}
	if d := a.docs[workspace.DocPath(a.root, a.realRoot, path)]; d != nil && d.buf != nil {
		cs.buffers[path] = d.buf
		return
	}
	gen, seq := a.gen, cs.seq
	go func() {
		f, err := workspace.ReadFile(path)
		if err != nil || f.Kind != workspace.Text {
			return
		}
		a.post.Post(func() {
			if a.gen != gen || cs.seq != seq {
				return
			}
			buf := editor.NewBuffer(f.Text)
			buf.SetLanguage(languageFor(path, f.Text))
			cs.buffers[path] = buf
		})
	}()
}

// callRow draws one tree row: a kind glyph, the item's name, its file
// and line muted at the right, or a status line under the parent.
func (a *App) callRow(c *ui.Context, k int) {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := &cs.nodes[k]
	if n.item.Name == statusRow {
		a.callStatusRow(c, k)
		return
	}
	row := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Margin(-2, 0).Padding(2, 0)
	row.Children(func() {
		a.callItemRow(c, n)
	})
	if cs.types {
		row.OnClick(func() {
			for i := 0; i < cs.tree.Rows(); i++ {
				if cs.tree.Item(i) == k {
					cs.row = i
					break
				}
			}
			a.openCallRow(k)
		})
	}
	if row.RowDoubleClicked() {
		a.openCallRow(k)
	}
	row.ContextMenu(func(m *ui.Menu) {
		if m.Item(a.hierarchyText("MakeRoot")).Chosen() {
			a.makeCallRoot(k)
		}
		if m.Item(a.hierarchyText("CopyLocation")).Chosen() {
			a.copyCallLocation(k)
		}
	})
}

// callStatusRow is the one child of a loading, failed or empty node:
// a spinner, the error with Retry, or a muted no-calls line.
func (a *App) callStatusRow(c *ui.Context, k int) {
	cs := &a.calls
	parent := &cs.nodes[cs.nodes[k].parent]
	row := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Margin(-2, 0).Padding(2, 0)
	row.Children(func() {
		switch {
		case parent.pending:
			ui.Spinner(c)
			ui.Text(c, a.hierarchyText("Loading")).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(1)
		case parent.err != nil:
			ui.Text(c, a.callErrorText(parent)).FontSize(12).TextColor(colors(c).Err).SingleLine().Shrink(1)
			if ui.Button(c, locales.Retry()).Padding(0, 6).Clicked() {
				parent.err = nil
				a.expandCall(cs.nodes[k].parent)
			}
		default:
			ui.Text(c, a.callEmptyText(cs.dir)).FontSize(12).TextColor(colors(c).Muted).SingleLine().Shrink(1)
		}
	})
}

// callErrorText is what a failed level says: that the server does not
// answer the direction, or the error's text.
func (a *App) callErrorText(n *callNode) string {
	if n.unsupported {
		if a.calls.dir == callsOutgoing {
			return a.hierarchyText("NoOutgoing")
		}
		return a.hierarchyText("NoIncoming")
	}
	if errors.Is(n.err, lsp.ErrNoCallItem) {
		return a.hierarchyText("NoItem")
	}
	return n.err.Error()
}

// callEmptyText is the no-calls line of the tree's direction.
func (a *App) callEmptyText(dir callDir) string {
	if dir == callsOutgoing {
		return a.hierarchyText("NoCallees")
	}
	return a.hierarchyText("NoCallers")
}

// callItemRow is a row's kind glyph, name and muted file:line; the
// root reads as one more row, as the spec asks.
func (a *App) callItemRow(c *ui.Context, n *callNode) {
	kind, color := symbolKind(c, n.item.Kind)
	ui.Box(c).Width(16).Height(16).Radius(4).Shrink(0).Background(color.Alpha(0.16)).Center().Children(func() {
		ui.Text(c, kind).FontSize(10).Bold().TextColor(color).SingleLine()
	})
	name := n.item.Name
	if a.calls.types {
		name = hierarchyTypeName(n.item)
	}
	ui.Text(c, name).FontSize(12).SingleLine().Shrink(0)
	if label := a.callItemPlace(n); label != "" {
		ui.Text(c, label).FontSize(11).TextColor(colors(c).Muted).SingleLine().Shrink(1)
	}
}

// callItemPlace is a row's file and line, relative to the workspace as
// the related results show them.
func (a *App) callItemPlace(n *callNode) string {
	if n.item.URI == "" {
		return ""
	}
	p, err := uri.ToPath(n.item.URI)
	if err != nil {
		return filepath.Base(n.item.URI)
	}
	rel := filepath.Base(p)
	for _, other := range a.calls.nodes {
		otherPath, err := uri.ToPath(other.item.URI)
		if err == nil && otherPath != p && filepath.Base(otherPath) == rel {
			rel = filepath.Join(filepath.Base(filepath.Dir(p)), rel)
			break
		}
	}
	return fmt.Sprintf("%s:%d", rel, a.callSiteRange(n).Start.Line+1)
}

// openCallRow jumps to the row's call site in the focused group, as a
// definition target does, recording where the reader came from.
func (a *App) openCallRow(k int) {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := &cs.nodes[k]
	if n.item.Name == statusRow || n.item.URI == "" {
		return
	}
	p, err := uri.ToPath(n.item.URI)
	if err != nil {
		return
	}
	rng := a.callSiteRange(n)
	a.openIn(a.focus, p, goTo{lsp: &rng.Start, enc: cs.anchor.enc})
}

// makeCallRoot re-centers the tree on a row's item: the item prepares
// no further, its calls build the first level again.
func (a *App) makeCallRoot(k int) {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := cs.nodes[k]
	if n.item.Name == statusRow || n.item.URI == "" {
		return
	}
	a.cancelCalls()
	cs.items = []lsp.CallItem{n.item}
	cs.anchor.name = n.item.Name
	cs.docVersion = 0 // the new anchor's version is its own question
	if p, err := uri.ToPath(n.item.URI); err == nil {
		cs.anchor.path = p
		cs.anchor.uri = n.item.URI
		if d := a.docs[p]; d != nil && d.buf != nil {
			cs.docVersion = d.buf.Version()
		}
	}
	cs.tree.Open = ui.Selection[int]{}
	cs.nodes = nil
	cs.roots = nil
	cs.preview = nil
	cs.roots = []int{a.addCallNode(callNode{parent: -1, status: -1, item: n.item})}
	a.expandCall(cs.roots[0])
}

// copyCallLocation puts the row's file:line on the clipboard.
func (a *App) copyCallLocation(k int) {
	cs := &a.calls
	if k < 0 || k >= len(cs.nodes) {
		return
	}
	n := &cs.nodes[k]
	if n.item.URI == "" {
		return
	}
	p, err := uri.ToPath(n.item.URI)
	if err != nil {
		return
	}
	a.services.WriteClipboard(fmt.Sprintf("%s:%d", p, a.callSiteRange(n).Start.Line+1))
}

// callsStop ends the panel's request: the workspace closed or the
// server stopped.
func (a *App) callsStop() {
	a.cancelCalls()
	cs := &a.calls
	cs.items = nil
	cs.nodes = nil
	cs.roots = nil
	cs.server = nil
	cs.preview = nil
	cs.buffers = nil
}
