package main

import (
	"path/filepath"
	"slices"

	"github.com/egoist/mygo/ui/editor"
)

// workspaceSession stores only navigation and presentation state; file contents
// remain on disk and are loaded through the normal document pipeline.
type workspaceSession struct {
	Layout        *sessionLayout `json:"layout,omitempty"`
	SidebarHidden bool           `json:"sidebarHidden,omitempty"`
	Navigator     int            `json:"navigator,omitempty"`
}

type sessionLayout struct {
	Tabs     []sessionTab   `json:"tabs,omitempty"`
	Active   string         `json:"active,omitempty"`
	Preview  string         `json:"preview,omitempty"`
	Focused  bool           `json:"focused,omitempty"`
	Vertical bool           `json:"vertical,omitempty"`
	Percent  float32        `json:"percent,omitempty"`
	First    *sessionLayout `json:"first,omitempty"`
	Second   *sessionLayout `json:"second,omitempty"`
}

type sessionTab struct {
	Path            string `json:"path"`
	Line            int    `json:"line,omitempty"`
	Column          int    `json:"column,omitempty"`
	MarkdownPreview bool   `json:"markdownPreview,omitempty"`
}

func (a *app) captureSession() {
	var snapshot func(*layout) *sessionLayout
	snapshot = func(n *layout) *sessionLayout {
		if n == nil {
			return nil
		}
		if n.g == nil {
			return &sessionLayout{Vertical: n.vertical, Percent: n.pct, First: snapshot(n.a), Second: snapshot(n.b)}
		}
		g := n.g
		s := &sessionLayout{Active: g.active, Preview: g.preview, Focused: g == a.focus}
		for _, path := range g.order {
			tab := sessionTab{Path: path, MarkdownPreview: g.markdownPreview[path]}
			if ed := g.eds[path]; ed != nil {
				p := ed.CursorPos()
				tab.Line, tab.Column = p.Line, p.Col
			} else if p := g.pending[path].pos; p != nil {
				tab.Line, tab.Column = p.Line, p.Col
			}
			s.Tabs = append(s.Tabs, tab)
		}
		return s
	}
	if a.cfg.Workspaces == nil {
		a.cfg.Workspaces = make(map[string]workspaceSession)
	}
	a.cfg.Workspaces[a.root] = workspaceSession{Layout: snapshot(a.layout), SidebarHidden: a.sidebarOff, Navigator: a.navigator}
}

func (a *app) restoreSession() {
	s, ok := a.cfg.Workspaces[a.root]
	if !ok || s.Layout == nil {
		return
	}
	a.restoringSession = true
	defer func() { a.restoringSession = false }()
	a.sidebarOff = s.SidebarHidden
	if s.Navigator >= navFiles && s.Navigator <= navGit {
		a.navigator = s.Navigator
	}
	var focused *group
	var restore func(*sessionLayout, *layout, int) *layout
	restore = func(saved *sessionLayout, parent *layout, depth int) *layout {
		if saved == nil || depth > 32 {
			return nil
		}
		if saved.First != nil && saved.Second != nil {
			n := &layout{parent: parent, vertical: saved.Vertical, pct: saved.Percent}
			if n.pct < 5 || n.pct > 95 {
				n.pct = 50
			}
			n.a, n.b = restore(saved.First, n, depth+1), restore(saved.Second, n, depth+1)
			if n.a == nil || n.b == nil {
				return nil
			}
			return n
		}
		g := a.newGroup()
		n := &layout{g: g, parent: parent}
		for _, tab := range saved.Tabs {
			if !filepath.IsAbs(tab.Path) || g.has(tab.Path) {
				continue
			}
			pos := editor.Pos{Line: max(tab.Line, 0), Col: max(tab.Column, 0)}
			a.openIn(g, tab.Path, goTo{pos: &pos})
			if tab.MarkdownPreview {
				if g.markdownPreview == nil {
					g.markdownPreview = map[string]bool{}
				}
				g.markdownPreview[tab.Path] = true
			}
		}
		if g.has(saved.Active) {
			g.active = saved.Active
		}
		if g.has(saved.Preview) {
			g.preview = saved.Preview
		}
		if g.active != "" {
			g.mru = slices.DeleteFunc(g.mru, func(p string) bool { return p == g.active })
			g.mru = append(g.mru, g.active)
		}
		if saved.Focused {
			focused = g
		}
		return n
	}
	if restored := restore(s.Layout, nil, 0); restored != nil {
		a.layout = restored
		if focused == nil {
			focused = a.groups()[0]
		}
		a.focus = focused
		if focused.active != "" {
			a.revealInTree(focused.active)
		}
	}
}
