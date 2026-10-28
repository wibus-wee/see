package main

import (
	"slices"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

// group is an editor area with tabs of its own, as Xcode's editors
// side by side: each shows its documents in editors of its own, which
// keep their own place, over the buffers all groups share.
type group struct {
	id              int
	order           []string // the tabs, left to right
	mru             []string // most recently active last
	active          string
	preview         string
	eds             map[string]*editor.State
	pending         map[string]goTo // where to go once a document loads
	markdownPreview map[string]bool
	tabSel          int
	relatedHandle   ui.Handle
	relatedBounds   ui.Rect
	pointerAnchor   ui.Point
	pointerInEditor bool
	caretAnchor     ui.Point
}

// goTo is a place to show in a document: a position of the buffer, or
// a server's, converted once the text is there, and how many bytes to
// select from it.
type goTo struct {
	pos    *editor.Pos
	lsp    *lsp.Position
	enc    string
	selLen int
}

func (g *group) has(path string) bool { return slices.Contains(g.order, path) }

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
func (a *app) groups() []*group { return a.layout.leaves(nil) }

func (a *app) newGroup() *group {
	a.nextGroup++
	return &group{id: a.nextGroup, eds: map[string]*editor.State{}, pending: map[string]goTo{}, tabSel: -1}
}

// resetLayout leaves one empty group.
func (a *app) resetLayout() {
	g := a.newGroup()
	a.layout = &layout{g: g}
	a.focus = g
}

// splitBeside puts a new, empty group at s of g.
func (a *app) splitBeside(g *group, s side) *group {
	leaf := a.layout.find(g)
	if leaf == nil || s == sideNone {
		return g
	}
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
func (a *app) removeGroup(g *group) {
	leaf := a.layout.find(g)
	if leaf == nil || leaf.parent == nil {
		return
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
func (a *app) activateIn(g *group, path string) {
	if !g.has(path) {
		return
	}
	a.focus = g
	g.active = path
	g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == path })
	g.mru = append(g.mru, path)
	a.revealInTree(path)
	a.saveConfigSoon()
}

// closeTab closes a tab of g. A document no group shows any more is let
// go; a group left without tabs goes, unless it is the last.
func (a *app) closeTab(g *group, path string) {
	if !g.has(path) {
		return
	}
	if d := a.docs[path]; d != nil && d.buf != nil && d.buf.Modified() {
		last := true
		for _, other := range a.groups() {
			if other != g && other.has(path) {
				last = false
				break
			}
		}
		if last && a.guardDirty([]*doc{d}, func() { a.closeTab(g, path) }) {
			return
		}
	}
	if g.preview == path {
		g.preview = ""
	}
	g.order = slices.DeleteFunc(g.order, func(p string) bool { return p == path })
	g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == path })
	delete(g.eds, path)
	delete(g.pending, path)
	delete(g.markdownPreview, path)
	if g.active == path {
		g.active = ""
		if n := len(g.mru); n > 0 {
			g.active = g.mru[n-1]
		}
	}
	a.releaseIfUnused(path)
	if len(g.order) == 0 && len(a.groups()) > 1 {
		a.removeGroup(g)
	}
	if a.focus == g && g.active != "" {
		a.revealInTree(g.active)
	}
	a.saveConfigSoon()
}

// releaseIfUnused lets a document go once no group shows it.
func (a *app) releaseIfUnused(path string) {
	for _, g := range a.groups() {
		if g.has(path) {
			return
		}
	}
	d := a.docs[path]
	if d == nil {
		return
	}
	delete(a.docs, path)
	if d.untitled {
		removeDraft(d)
	}
	d.seq++ // drop a load on its way
	if m := a.serverFor(d.path); m != nil {
		m.close(d.uri)
	}
}

// moveTab moves a tab from one group to another, at index at, or into a
// new group at s of to. Its editor goes with it, keeping its place.
func (a *app) moveTab(from *group, path string, to *group, s side, at int) {
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
	ed := from.eds[path]
	previewMarkdown := from.markdownPreview[path]
	alreadyOpen := to.has(path)
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
	if ed != nil {
		to.eds[path] = ed
	}
	if hasPend {
		to.pending[path] = pend
	}
	// Close it in from without letting the document go.
	from.order = slices.DeleteFunc(from.order, func(p string) bool { return p == path })
	from.mru = slices.DeleteFunc(from.mru, func(p string) bool { return p == path })
	delete(from.eds, path)
	delete(from.pending, path)
	delete(from.markdownPreview, path)
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
}

// splitActive shows the focused group's document in a new group at s
// too, at the same place, as Xcode's and VS Code's Split Editor.
func (a *app) splitActive(s side) {
	g := a.focus
	path := g.active
	ng := a.splitBeside(g, s)
	if path == "" {
		a.focus = ng
		a.saveConfigSoon()
		return
	}
	ng.addTab(path, 0)
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
func (a *app) focusGroup(n int) {
	gs := a.groups()
	if n < len(gs) {
		a.focus = gs[n]
		if ed := gs[n].eds[gs[n].active]; ed != nil {
			a.focusEditor = ed
		}
		a.saveConfigSoon()
	}
}
