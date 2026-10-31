package app

import (
	"path/filepath"
	"see/internal/config"
	"slices"

	"github.com/egoist/mygo/ui/editor"
)

func (a *App) resizedWorkspace(width, height int) {
	if a.root == "" || width < 480 || height < 320 || width > 10000 || height > 10000 {
		return
	}
	if a.windowWidth != width || a.windowHeight != height {
		a.windowWidth, a.windowHeight = width, height
		a.saveConfigSoon()
	}
}

func (a *App) captureSession() {
	var snapshot func(*layout) *config.Layout
	snapshot = func(n *layout) *config.Layout {
		if n == nil {
			return nil
		}
		if n.g == nil {
			return &config.Layout{Vertical: n.vertical, Percent: n.pct, First: snapshot(n.a), Second: snapshot(n.b)}
		}
		g := n.g
		s := &config.Layout{Active: tabPath(g.active), ActivePreview: isMarkdownTab(g.active), Preview: g.preview, Focused: g == a.focus}
		if isDiffTab(g.active) {
			s.Active = ""
		}
		for _, key := range g.order {
			if isDiffTab(key) {
				continue
			}
			path := tabPath(key)
			tab := config.Tab{Path: path, MarkdownPreview: !isMarkdownTab(key) && g.markdownPreview[path], PreviewTab: isMarkdownTab(key)}
			if d := a.docs[path]; d != nil && d.untitled {
				tab.Untitled, tab.Name = true, d.name
			}
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
		a.cfg.Workspaces = make(map[string]config.Workspace)
	}
	a.cfg.Workspaces[a.root] = config.Workspace{Layout: snapshot(a.layout), SidebarHidden: a.sidebarOff, Navigator: a.navigator, WindowWidth: a.windowWidth, WindowHeight: a.windowHeight}
}

func (a *App) restoreSession() {
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
	var restore func(*config.Layout, *layout, int) *layout
	restore = func(saved *config.Layout, parent *layout, depth int) *layout {
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
			key := tab.Path
			if tab.PreviewTab {
				key = markdownTab(tab.Path)
			}
			if !filepath.IsAbs(tab.Path) || g.has(key) || (tab.PreviewTab && !isMarkdown(tab.Path)) {
				continue
			}
			pos := editor.Pos{Line: max(tab.Line, 0), Col: max(tab.Column, 0)}
			if tab.Untitled && a.docs[tab.Path] == nil {
				if !validDraftPath(tab.Path) {
					continue
				}
				d := &doc{path: tab.Path, uri: uriOf(tab.Path), name: tab.Name, untitled: true, cache: &draftCache{}}
				if d.name == "" {
					d.name = "Untitled"
				}
				a.docs[tab.Path] = d
				a.loadDoc(d)
			}
			if tab.PreviewTab {
				d := a.docs[tab.Path]
				if d == nil {
					d = &doc{path: tab.Path, uri: uriOf(tab.Path), name: filepath.Base(tab.Path)}
					a.docs[tab.Path] = d
					a.loadDoc(d)
				}
				g.addTab(key, len(g.order))
				a.activateIn(g, key)
				a.showLoaded(g, d)
			} else {
				a.openIn(g, tab.Path, goTo{pos: &pos})
			}
			if tab.MarkdownPreview {
				if g.markdownPreview == nil {
					g.markdownPreview = map[string]bool{}
				}
				g.markdownPreview[tab.Path] = true
			}
		}
		active := saved.Active
		if saved.ActivePreview {
			active = markdownTab(active)
		}
		if g.has(active) {
			g.active = active
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
			a.revealInTree(tabPath(focused.active))
		}
	}
}
