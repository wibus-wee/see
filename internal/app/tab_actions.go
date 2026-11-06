package app

import "slices"

type tabCloseMode uint8

const (
	closeOtherTabs tabCloseMode = iota
	closeRightTabs
	closeSavedTabs
)

func (a *App) closeTabs(g *group, anchor string, mode tabCloseMode) {
	at := slices.Index(g.order, anchor)
	if at < 0 {
		return
	}
	var candidates []string
	for i, tab := range g.order {
		switch mode {
		case closeOtherTabs:
			if tab == anchor {
				continue
			}
		case closeRightTabs:
			if i <= at {
				continue
			}
		case closeSavedTabs:
			if d := a.docs[tabPath(tab)]; d != nil && d.buf != nil && d.buf.Modified() {
				continue
			}
		}
		candidates = append(candidates, tab)
	}
	a.closeTabSet(g, candidates)
}

// closeTabSet asks once before removing the last views of modified buffers.
// The snapshot excludes tabs opened while the confirmation is pending.
func (a *App) closeTabSet(g *group, candidates []string) {
	var dirty []*doc
	seen := map[string]bool{}
	for _, tab := range candidates {
		if !g.has(tab) {
			continue
		}
		path := tabPath(tab)
		d := a.docs[path]
		if d == nil || d.buf == nil || !d.buf.Modified() || seen[path] {
			continue
		}
		seen[path] = true
		retained := false
		for _, other := range a.groups() {
			for _, open := range other.order {
				if !isDiffTab(open) && tabPath(open) == path && (other != g || !slices.Contains(candidates, open)) {
					retained = true
				}
			}
		}
		if !retained {
			dirty = append(dirty, d)
		}
	}
	if a.guardDirty(dirty, func() { a.closeTabSet(g, candidates) }) {
		return
	}
	for _, tab := range candidates {
		a.closeTab(g, tab)
	}
}
