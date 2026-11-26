package app

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/workspace"
)

// group is an editor area with tabs of its own, as Xcode's editors
// side by side: each shows its documents in editors of its own, which
// keep their own place, over the buffers all groups share.
type group struct {
	videos          map[string]*videoPreview
	id              int
	order           []string // the tabs, left to right
	mru             []string // most recently active last
	active          string
	cycle           []string     // the Ctrl+Tab walk left to do: tabs by recency, active first
	cycleMod        ui.Modifiers // the modifier the walk began under; letting it go lands the walk
	cycleActive     bool         // Ctrl+Tab is walking cycle: activating does not touch mru
	colorPreview    *colorPreviewState
	preview         string
	eds             map[string]*editor.State
	minimaps        map[string]*minimap
	pending         map[string]goTo // where to go once a document loads
	markdownPreview map[string]bool
	hexPreview      map[string]bool
	hexLists        map[string]*ui.ListState
	hexControls     map[string]*hexControl
	diffs           map[string]*gitDiffTab
	tabSel          int
	focusHandle     ui.Handle
	relatedHandle   ui.Handle
	relatedBounds   ui.Rect
	relatedOpen     bool
	counterpartMenu *counterpartMenu
	// jumpFiles and jumpSymbols open the jump bar's file and symbol menus.
	jumpFiles, jumpSymbols bool
	closing                bool // its tabs are going away with the group
	pointerAnchor          ui.Point
	pointerInEditor        bool
	// changeBars are the Git change bars the editor shows, in the window,
	// and changeBarPress is whether a press on one is being taken.
	changeBars     []changeBar
	changeBarPress bool
	caretAnchor    ui.Point
	regexAction    regexActionCache
	// pin is the reading-position pin of the editor's top line.
	pin readingPin
}

// goTo is a place to show in a document: a position of the buffer, or
// a server's, converted once the text is there, and how many bytes to
// select from it.
type goTo struct {
	folds  []int
	pos    *editor.Pos
	lsp    *lsp.Position
	enc    string
	selLen int
}

func (g *group) has(path string) bool { return slices.Contains(g.order, path) }

const markdownTabPrefix = "\x00preview:markdown:"
const diffTabPrefix = "\x00diff:"

func markdownTab(path string) string { return markdownTabPrefix + path }
func isMarkdownTab(tab string) bool  { return strings.HasPrefix(tab, markdownTabPrefix) }
func isDiffTab(tab string) bool      { return strings.HasPrefix(tab, diffTabPrefix) }
func diffTab(path string, staged bool) string {
	return fmt.Sprintf("%s%t:%s", diffTabPrefix, staged, path)
}
func tabPath(tab string) string { return strings.TrimPrefix(tab, markdownTabPrefix) }

// tabFile is the file of the workspace a tab of g shows, which the tree
// chooses: a diff's too, of the working tree, the index or a commit; ""
// for a commit's own tab.
func (a *App) tabFile(g *group, tab string) string {
	if tab == "" {
		return ""
	}
	if !isDiffTab(tab) {
		return tabPath(tab)
	}
	v := g.diffs[tab]
	if v == nil {
		return ""
	}
	if v.revision != nil {
		return v.revision.path
	}
	if v.localHistory != nil {
		return v.localHistory.path
	}
	root := a.gitDiffRoot(v)
	if v.page() || v.cli || root == "" {
		return ""
	}
	return workspace.DocPath(a.root, a.realRoot, filepath.Join(root, filepath.FromSlash(v.name)))
}
func (g *group) hasDocument(path string) bool { return g.has(path) || g.has(markdownTab(path)) }

// side is where a new group goes beside another.
type side int

const (
	sideNone side = iota // in the group itself
	sideLeft
	sideRight
	sideTop
	sideBottom
)

// layout is the tree the editor area is split into: a leaf shows a
// group; a split shows a and b side by side, or a above b, a taking pct
// of the room.
type layout struct {
	g        *group
	vertical bool
	pct      float32
	a, b     *layout
	parent   *layout
}

func (n *layout) leaves(out []*group) []*group {
	if n == nil {
		return out
	}
	if n.g != nil {
		return append(out, n.g)
	}
	return n.b.leaves(n.a.leaves(out))
}

func (n *layout) find(g *group) *layout {
	if n == nil {
		return nil
	}
	if n.g == g {
		return n
	}
	if f := n.a.find(g); f != nil {
		return f
	}
	return n.b.find(g)
}

// groups lists the groups, left to right and top to bottom.
func (a *App) groups() []*group { return a.layout.leaves(nil) }

// hasTabs is whether any group has a tab open — a document, a diff, or
// another kind. A folderless window that shows only `see --diff` tabs
// has no documents, and the main window would hide without this.
func (a *App) hasTabs() bool {
	for _, g := range a.groups() {
		if len(g.order) > 0 {
			return true
		}
	}
	return false
}

func (a *App) newGroup() *group {
	a.nextGroup++
	return &group{id: a.nextGroup, eds: map[string]*editor.State{}, pending: map[string]goTo{}, tabSel: -1}
}

// resetLayout leaves one empty group.
func (a *App) resetLayout() {
	a.maximized = nil
	for _, g := range a.groups() {
		a.releaseCompletionGroup(g)
	}
	g := a.newGroup()
	a.layout = &layout{g: g}
	a.focus = g
}

// splitBeside puts a new, empty group at s of g.
func (a *App) splitBeside(g *group, s side) *group {
	leaf := a.layout.find(g)
	if leaf == nil || s == sideNone {
		return g
	}
	a.maximized = nil
	ng := a.newGroup()
	old := &layout{g: g, parent: leaf}
	nw := &layout{g: ng, parent: leaf}
	leaf.g = nil
	leaf.vertical = s == sideTop || s == sideBottom
	leaf.pct = 50
	if s == sideLeft || s == sideTop {
		leaf.a, leaf.b = nw, old
	} else {
		leaf.a, leaf.b = old, nw
	}
	return ng
}

// removeGroup takes an empty group out of the layout: its sibling takes
// its room. The last group stays.
func (a *App) removeGroup(g *group) {
	leaf := a.layout.find(g)
	if leaf == nil || leaf.parent == nil {
		return
	}
	a.releaseCompletionGroup(g)
	if a.maximized == g {
		a.maximized = nil
	}
	for path := range g.videos {
		g.closeVideo(path)
	}
	p := leaf.parent
	sib := p.a
	if sib == leaf {
		sib = p.b
	}
	// The parent becomes the sibling.
	p.g, p.vertical, p.pct, p.a, p.b = sib.g, sib.vertical, sib.pct, sib.a, sib.b
	if p.a != nil {
		p.a.parent, p.b.parent = p, p
	}
	if a.focus == g {
		a.focus = a.groups()[0]
		if gs := p.leaves(nil); len(gs) > 0 {
			a.focus = gs[0]
		}
	}
	a.closeRegexPlaygroundForSource(g, "")
}

// addTab puts path in g at index at (its end when out of range), or
// moves it there if g has it.
func (g *group) addTab(path string, at int) {
	if i := slices.Index(g.order, path); i >= 0 {
		g.order = slices.Delete(g.order, i, i+1)
		if i < at {
			at--
		}
	}
	at = min(max(at, 0), len(g.order))
	g.order = slices.Insert(g.order, at, path)
}

// activateIn makes path the active tab of g, and g the focused group.
func (a *App) activateIn(g *group, path string) {
	if !g.has(path) {
		return
	}
	if a.maximized != g {
		a.maximized = nil
	}
	if a.focus != nil && a.focus != g {
		a.focus.dismissColorPreview()
	}
	if g.active != path {
		g.dismissColorPreview()
	}
	if g.cycleActive && !slices.Contains(g.cycle, path) {
		a.endTabCycle() // something else activated: the walk is over
	}
	a.focus = g
	g.active = path
	if !g.cycleActive {
		g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == path })
		g.mru = append(g.mru, path)
		if !isDiffTab(path) && !isMarkdownTab(path) {
			a.recents = slices.DeleteFunc(a.recents, func(p string) bool { return p == path })
			a.recents = append(a.recents, path)
			if len(a.recents) > maxRecents {
				a.recents = slices.Delete(a.recents, 0, len(a.recents)-maxRecents)
			}
		}
	}
	if p := a.tabFile(g, path); p != "" {
		a.revealInTree(p)
	}
	a.saveConfigSoon()
}

// closedTab is a closed file tab Reopen Closed Tab brings back, its
// place in the text kept when its group's editor had one.
type closedTab struct {
	path     string
	pos      *editor.Pos
	scrollY  float64
	preview  bool
	markdown bool
}

// maxClosedTabs is how many closed tabs a window remembers.
const maxClosedTabs = 20

// recordClosedTab remembers a file tab of g for Reopen Closed Tab; a
// Git tab or a markdown preview's own tab is left out, as is a file
// with unsaved edits: its unsaved text would reopen as its saved text.
func (a *App) recordClosedTab(g *group, path string) {
	if isDiffTab(path) {
		return
	}
	e := closedTab{path: tabPath(path), preview: g.preview == path, markdown: isMarkdownTab(path)}
	if d := a.docs[e.path]; d == nil || d.untitled || (d.buf != nil && d.buf.Modified()) {
		return
	}
	if !e.markdown {
		if st := g.eds[e.path]; st != nil {
			p, y := st.CursorPos(), st.ScrollY()
			e.pos, e.scrollY = &p, y
		} else if p := g.pending[e.path].pos; p != nil {
			pos := *p
			e.pos = &pos
		}
	}
	a.closed = slices.DeleteFunc(a.closed, func(t closedTab) bool { return t.path == e.path && t.markdown == e.markdown })
	a.closed = append(a.closed, e)
	if len(a.closed) > maxClosedTabs {
		a.closed = a.closed[len(a.closed)-maxClosedTabs:]
	}
}

// reopenClosedTab opens the last closed tab again, in its group when it
// is still there, as browsers do.
func (a *App) reopenClosedTab() {
	if len(a.closed) == 0 {
		return
	}
	e := a.closed[len(a.closed)-1]
	a.closed = a.closed[:len(a.closed)-1]
	g := a.focus
	if g == nil {
		return
	}
	key := e.path
	if e.markdown {
		key = markdownTab(e.path)
	}
	if g.has(key) {
		a.activateIn(g, key)
		return
	}
	if e.markdown {
		d := a.openIn(g, e.path, goTo{})
		if d != nil {
			a.openMarkdownTab(g)
		}
		return
	}
	a.openIn(g, e.path, goTo{pos: e.pos})
	if e.scrollY > 0 {
		if st := g.eds[e.path]; st != nil {
			st.SetScrollY(e.scrollY)
		}
	}
	if e.preview && g.preview == "" && slices.Contains(g.order, e.path) {
		g.preview = e.path
	}
}

// maxRecents bounds the activated files recents keeps.
const maxRecents = 60

// endTabCycle lands the Ctrl+Tab walk: the tab it reached is the most
// recent again, and the rest of the list keeps the order it had.
func (a *App) endTabCycle() {
	for _, g := range a.groups() {
		if g.cycleActive {
			a.landTabCycle(g)
		}
	}
}

// noteRecent lands a file in recents as endTabCycle's walk does: the
// tab reached counts as the file active most lately.
func (a *App) noteRecent(path string) {
	if isDiffTab(path) || isMarkdownTab(path) {
		return
	}
	a.recents = slices.DeleteFunc(a.recents, func(p string) bool { return p == path })
	a.recents = append(a.recents, path)
	if len(a.recents) > maxRecents {
		a.recents = slices.Delete(a.recents, 0, len(a.recents)-maxRecents)
	}
}

// settleTabCycles lands the walks whose modifier is no longer held.
func (a *App) settleTabCycles() {
	for _, g := range a.groups() {
		if g.cycleActive && a.modifiers&g.cycleMod == 0 {
			a.landTabCycle(g)
		}
	}
}

// landTabCycle ends g's walk: the tab it reached is the most recent
// again, and the rest of the list keeps the order it had.
func (a *App) landTabCycle(g *group) {
	g.cycleActive = false
	g.cycle, g.cycleMod = nil, 0
	if g.active != "" {
		g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == g.active })
		g.mru = append(g.mru, g.active)
		a.noteRecent(g.active)
	}
}

// cycleTab activates a tab of the focused group in most-recent order:
// dir 1 back through the history, -1 back toward the latest. While Ctrl
// is held the walk goes on, the active tab changing but mru not moving;
// letting Ctrl go ends it, and the tab reached becomes the most recent.
// A group without the two activations the walk needs cycles the tabs
// left to right, as before.
func (a *App) cycleTab(dir int) {
	g := a.focus
	if g == nil || len(g.order) < 2 {
		return
	}
	if !g.cycleActive {
		g.cycle = mruOrder(g)
		if len(g.cycle) < 2 {
			i := slices.Index(g.order, g.active)
			a.userActivate(g.order[(i+dir+len(g.order))%len(g.order)])
			return
		}
		g.cycleMod = a.modifiers & (ui.Ctrl | ui.Cmd)
		g.cycleActive = true
	}
	// The walk moves dir through cycle, past tabs closed since it began.
	i := slices.Index(g.cycle, g.active)
	if i < 0 {
		i = 0 // the active tab closed: the walk goes on from its start
	}
	for n := 0; n < len(g.cycle); n++ {
		i = (i + dir + len(g.cycle)) % len(g.cycle)
		if g.has(g.cycle[i]) {
			if g.cycle[i] != g.active {
				a.userActivate(g.cycle[i])
			}
			return
		}
	}
}

// mruOrder lists g's tabs most recently active first: mru back to
// front, then any tab of order the history lost, as a session restored
// before it tracked activations leaves.
func mruOrder(g *group) []string {
	out := make([]string, 0, len(g.order))
	for i := len(g.mru) - 1; i >= 0; i-- {
		if g.has(g.mru[i]) {
			out = append(out, g.mru[i])
		}
	}
	for _, tab := range g.order {
		if !slices.Contains(out, tab) {
			out = append(out, tab)
		}
	}
	return out
}

// closeTab closes a tab of g. A document no group shows any more is let
// go; a group left without tabs goes, unless it is the last.
func (a *App) closeTab(g *group, path string) {
	if !g.has(path) {
		return
	}
	docPath := tabPath(path)
	if g.colorPreview != nil && (g.colorPreview.path == docPath || g.active == path) {
		g.dismissColorPreview()
	}
	if v := g.diffs[path]; v != nil && v.search != nil && v.search.dirty() {
		a.activateIn(g, path)
		v.search.note, v.search.noteErr = searchEditText("searchEditCloseDirty"), true
		a.note(v.search.note, true)
		return
	}
	if isDiffTab(path) {
		delete(g.diffs, path)
	}
	if d := a.docs[docPath]; d != nil && d.buf != nil && d.buf.Modified() {
		last := true
		for _, other := range a.groups() {
			if (other != g && other.hasDocument(docPath)) || (other == g && ((path != docPath && other.has(docPath)) || (path == docPath && other.has(markdownTab(docPath))))) {
				last = false
				break
			}
		}
		if last && a.guardDirty([]*doc{d}, func() { a.closeTab(g, path) }) {
			return
		}
	}
	if !g.closing {
		a.recordClosedTab(g, path)
	}
	if g.preview == path {
		g.preview = ""
	}
	g.order = slices.DeleteFunc(g.order, func(p string) bool { return p == path })
	g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == path })
	g.cycle = slices.DeleteFunc(g.cycle, func(p string) bool { return p == path })
	if !g.hasDocument(docPath) {
		if cs := a.completions[g.id]; cs != nil && cs.path == docPath {
			a.cancelCompletions(g, cs.editor)
		}
		if d := a.docs[docPath]; d != nil && g.pin.requested.doc == d {
			g.pin.cancelRequest()
			g.pin = readingPin{seq: g.pin.seq + 1}
		}
		g.closeVideo(docPath)
		delete(g.eds, docPath)
		delete(g.minimaps, docPath)
		delete(g.pending, docPath)
		delete(g.markdownPreview, docPath)
		delete(g.hexPreview, docPath)
		delete(g.hexLists, docPath)
		if v := g.hexControls[docPath]; v != nil && v.cancel != nil {
			v.cancel()
		}
		delete(g.hexControls, docPath)
	}
	if g.active == path {
		g.active = ""
		if n := len(g.mru); n > 0 {
			g.active = g.mru[n-1]
		}
	}
	a.releaseIfUnused(docPath)
	if a.root == "" && len(a.docs) == 0 && !a.hasTabs() {
		a.host.SetTitle("See")
	}
	if len(g.order) == 0 && len(a.groups()) > 1 {
		a.removeGroup(g)
	}
	a.closeRegexPlaygroundForSource(g, docPath)
	if p := a.tabFile(g, g.active); a.focus == g && p != "" {
		a.revealInTree(p)
	}
	a.saveConfigSoon()
}

// releaseIfUnused lets a document go once no group shows it.
func (a *App) releaseIfUnused(path string) {
	for _, g := range a.groups() {
		if g.hasDocument(path) {
			return
		}
	}
	d := a.docs[path]
	if d == nil {
		return
	}
	a.captureCurrentLocalHistory(d)
	delete(a.docs, path)
	if a.root == "" && !d.untitled {
		releaseStandalonePathIfIdle(a, d, path)
	}
	if d.untitled {
		removeDraft(d)
	} else {
		a.removeRecovery(d)
	}
	d.seq++ // drop a load on its way
	a.clearDocumentLinks(d)
	a.clearFoldingRanges(d)
	if a.outline.requested.doc == d && a.outline.cancel != nil {
		a.outline.cancel()
		a.outline.cancel = nil
	}
	if m := a.serverFor(d.path); m != nil {
		m.Close(d.uri)
	}
}

// moveTab moves a tab from one group to another, at index at, or into a
// new group at s of to. Its editor goes with it, keeping its place.
func (a *App) moveTab(from *group, path string, to *group, s side, at int) {
	if from != nil && from.preview == path {
		from.preview = ""
	}
	if !from.has(path) {
		return
	}
	if s != sideNone {
		if from == to && len(from.order) == 1 {
			return // splitting a group's only tab off itself changes nothing
		}
		to = a.splitBeside(to, s)
		at = 0
	}
	if from == to {
		from.addTab(path, at)
		a.activateIn(to, path)
		return
	}
	if cs := a.completions[from.id]; cs != nil && cs.path == tabPath(path) {
		a.cancelCompletions(from, cs.editor)
	}
	if isDiffTab(path) {
		if to.diffs == nil {
			to.diffs = map[string]*gitDiffTab{}
		}
		if !to.has(path) {
			to.diffs[path] = from.diffs[path]
		}
		to.addTab(path, at)
		delete(from.diffs, path)
		from.order = slices.DeleteFunc(from.order, func(p string) bool { return p == path })
		from.mru = slices.DeleteFunc(from.mru, func(p string) bool { return p == path })
		from.cycle = slices.DeleteFunc(from.cycle, func(p string) bool { return p == path })
		if from.active == path {
			from.active = ""
			if n := len(from.mru); n > 0 {
				from.active = from.mru[n-1]
			}
		}
		if len(from.order) == 0 && len(a.groups()) > 1 {
			a.removeGroup(from)
		}
		a.activateIn(to, path)
		return
	}
	if isMarkdownTab(path) {
		underlying := tabPath(path)
		to.addTab(path, at)
		if d := a.docs[underlying]; d != nil {
			a.showLoaded(to, d)
		}
		from.order = slices.DeleteFunc(from.order, func(p string) bool { return p == path })
		from.mru = slices.DeleteFunc(from.mru, func(p string) bool { return p == path })
		from.cycle = slices.DeleteFunc(from.cycle, func(p string) bool { return p == path })
		if !from.hasDocument(underlying) {
			delete(from.eds, underlying)
			delete(from.minimaps, underlying)
			delete(from.pending, underlying)
		}
		if from.active == path {
			from.active = ""
			if n := len(from.mru); n > 0 {
				from.active = from.mru[n-1]
			}
		}
		if len(from.order) == 0 && len(a.groups()) > 1 {
			a.removeGroup(from)
		}
		a.activateIn(to, path)
		return
	}
	ed := from.eds[path]
	previewMarkdown := from.markdownPreview[path]
	previewHex := from.hexPreview[path]
	hexList := from.hexLists[path]
	hexCtl := from.hexControls[path]
	alreadyOpen := to.has(path)
	if v := from.videos[path]; v != nil {
		if !alreadyOpen {
			if to.videos == nil {
				to.videos = map[string]*videoPreview{}
			}
			to.videos[path] = v
			delete(from.videos, path)
		} else {
			from.closeVideo(path)
		}
	}
	pend, hasPend := from.pending[path]
	if to.has(path) {
		ed = to.eds[path] // it shows there already: keep that one
	}
	to.addTab(path, at)
	if !alreadyOpen && previewMarkdown {
		if to.markdownPreview == nil {
			to.markdownPreview = make(map[string]bool)
		}
		to.markdownPreview[path] = true
	}
	if !alreadyOpen && previewHex {
		if to.hexPreview == nil {
			to.hexPreview = map[string]bool{}
		}
		to.hexPreview[path] = true
	}
	if !alreadyOpen && hexList != nil {
		if to.hexLists == nil {
			to.hexLists = map[string]*ui.ListState{}
		}
		to.hexLists[path] = hexList
	}
	if !alreadyOpen && hexCtl != nil {
		if to.hexControls == nil {
			to.hexControls = map[string]*hexControl{}
		}
		to.hexControls[path] = hexCtl
	}
	if ed != nil {
		if from.has(markdownTab(path)) && !alreadyOpen {
			if d := a.docs[path]; d != nil {
				to.eds[path] = a.newEditor(to, d)
			}
		} else {
			to.eds[path] = ed
		}
		if d := a.docs[path]; d != nil {
			ed.OnCompletion = func(req editor.CompletionRequest) { a.requestCompletions(to, d, ed, req) }
		}
	}
	if hasPend {
		to.pending[path] = pend
	}
	// Close it in from without letting the document go.
	from.order = slices.DeleteFunc(from.order, func(p string) bool { return p == path })
	from.mru = slices.DeleteFunc(from.mru, func(p string) bool { return p == path })
	from.cycle = slices.DeleteFunc(from.cycle, func(p string) bool { return p == path })
	if !from.hasDocument(path) {
		delete(from.eds, path)
	}
	delete(from.pending, path)
	delete(from.markdownPreview, path)
	delete(from.hexPreview, path)
	delete(from.hexLists, path)
	delete(from.hexControls, path)
	if from.active == path {
		from.active = ""
		if n := len(from.mru); n > 0 {
			from.active = from.mru[n-1]
		}
	}
	if len(from.order) == 0 && len(a.groups()) > 1 {
		a.removeGroup(from)
	}
	a.activateIn(to, path)
	a.closeRegexPlaygroundForSource(from, tabPath(path))
}

// splitActive shows the focused group's document in a new group at s
// too, at the same place, as Xcode's and VS Code's Split Editor.
func (a *App) splitActive(s side) {
	g := a.focus
	path := g.active
	ng := a.splitBeside(g, s)
	if path == "" {
		a.focus = ng
		a.saveConfigSoon()
		return
	}
	ng.addTab(path, 0)
	if isDiffTab(path) {
		if v := g.diffs[path]; v != nil && v.pending {
			ng.diffs = map[string]*gitDiffTab{path: v} // filled where it is when read
		} else if v != nil && v.log != nil {
			ng.diffs = map[string]*gitDiffTab{path: {name: v.name, log: &gitLogView{query: v.log.query}}}
		} else if v != nil && v.branch != nil {
			branch := &branchDiffView{
				baseRef:      v.branch.baseRef,
				baseName:     v.branch.baseName,
				shown:        max(v.branch.shown, branchDiffFilesAtOnce),
				collapsed:    cloneBoolMap(v.branch.collapsed),
				selectedPath: v.branch.selectedPath,
				jumpPath:     v.branch.jumpPath,
			}
			ng.diffs = map[string]*gitDiffTab{path: {name: v.name, branch: branch}}
			a.loadBranchDiff(branch, false)
		} else if v != nil && v.commit != nil {
			cv := *v.commit
			cv.scroll = ui.ScrollState{}
			ng.diffs = map[string]*gitDiffTab{path: {name: v.name, rev: v.rev, hash: v.hash, commit: &cv}}
		} else if v != nil && v.revision != nil {
			r := *v.revision
			clone := &gitDiffTab{name: v.name, revision: &r}
			ng.diffs = map[string]*gitDiffTab{path: clone}
			a.loadRevision(clone, "")
		} else if v != nil && v.search != nil {
			ng.diffs = map[string]*gitDiffTab{path: v}
			delete(g.diffs, path)
			g.order = slices.DeleteFunc(g.order, func(p string) bool { return p == path })
			g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == path })
			if g.active == path {
				g.active = ""
				if len(g.mru) > 0 {
					g.active = g.mru[len(g.mru)-1]
				}
			}
		} else if v != nil && v.localHistory != nil {
			clone := a.cloneLocalHistoryTab(v)
			ng.diffs = map[string]*gitDiffTab{path: clone}
			lh := clone.localHistory
			keepID := ""
			if lh.selected >= 0 && lh.selected < len(lh.snapshots) {
				keepID = lh.snapshots[lh.selected].ID
			}
			a.loadLocalHistory(clone, keepID)
		} else if v != nil && v.oldBuffer != nil {
			ng.diffs = map[string]*gitDiffTab{path: a.newCLIBufferDiff(v.oldBuffer, v.newBuffer, v.name)}
		} else if v != nil && v.change {
			nv := &gitDiffTab{name: v.name, root: v.root, staged: v.staged, change: true}
			hunk, _ := v.state.CurrentHunk()
			a.setGitChangeView(nv, gitChangeView{file: v.patch, partial: v.partial, untracked: v.untracked}, hunk)
			ng.diffs = map[string]*gitDiffTab{path: nv}
		} else if v != nil {
			nv := a.newGitDiffTab(v.patch, v.name, v.staged)
			nv.rev, nv.hash, nv.oldPath, nv.untracked = v.rev, v.hash, v.oldPath, v.untracked
			nv.cli = v.cli
			ng.diffs = map[string]*gitDiffTab{path: nv}
		}
		a.activateIn(ng, path)
		return
	}
	if isMarkdownTab(path) {
		a.activateIn(ng, path)
		if d := a.docs[tabPath(path)]; d != nil {
			a.showLoaded(ng, d)
		}
		return
	}
	if ed := g.eds[path]; ed != nil {
		pos := ed.CursorPos()
		ng.pending[path] = goTo{pos: &pos}
	}
	a.activateIn(ng, path)
	if d := a.docs[path]; d != nil {
		a.showLoaded(ng, d)
	}
}

// focusGroup focuses the n-th group, from 0.
func (a *App) focusGroup(n int) {
	gs := a.groups()
	if n >= 0 && n < len(gs) {
		a.requestGroupFocus(gs[n])
		a.saveConfigSoon()
	}
}
