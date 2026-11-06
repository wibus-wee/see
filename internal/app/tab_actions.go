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

// tabCloseTarget follows a document through Save As without capturing later tabs.
type tabCloseTarget struct {
	tab      string
	document *doc
	diff     *gitDiffTab
}

// closeTabSet keeps the selected documents fixed while confirmation is pending.
func (a *App) closeTabSet(g *group, candidates []string) {
	targets := make([]tabCloseTarget, 0, len(candidates))
	for _, tab := range candidates {
		if !g.has(tab) {
			continue
		}
		targets = append(targets, tabCloseTarget{tab: tab, document: a.docs[tabPath(tab)], diff: g.diffs[tab]})
	}
	a.closeTabTargets(g, targets, a.gen, map[*doc]uint64{})
}

func (a *App) closeTabTargets(g *group, targets []tabCloseTarget, generation int, approved map[*doc]uint64) {
	if a.gen != generation || !slices.Contains(a.groups(), g) {
		return
	}
	var candidates []string
	for _, target := range targets {
		tab := target.tab
		if target.document != nil {
			d := target.document
			if a.docs[d.path] != d {
				continue
			}
			tab = d.path
			if isMarkdownTab(target.tab) {
				tab = markdownTab(tab)
			}
		} else if target.diff == nil || g.diffs[tab] != target.diff {
			continue
		}
		if g.has(tab) {
			candidates = append(candidates, tab)
		}
	}
	var dirty []*doc
	versions := map[*doc]uint64{}
	for _, tab := range candidates {
		d := a.docs[tabPath(tab)]
		if d == nil || d.buf == nil || !d.buf.Modified() {
			continue
		}
		if _, seen := versions[d]; seen {
			continue
		}
		if version, ok := approved[d]; ok && version == d.buf.Version() {
			continue
		}
		retained := false
		for _, other := range a.groups() {
			for _, open := range other.order {
				if !isDiffTab(open) && tabPath(open) == d.path && (other != g || !slices.Contains(candidates, open)) {
					retained = true
				}
			}
		}
		if !retained {
			dirty = append(dirty, d)
			versions[d] = d.buf.Version()
		}
	}
	if a.guardDirty(dirty, func() {
		// Discard approves only the versions shown when this question was asked.
		if a.allowDirty {
			for d, version := range versions {
				approved[d] = version
			}
		}
		inherited := a.allowDirty
		a.allowDirty = false
		defer func() { a.allowDirty = inherited }()
		a.closeTabTargets(g, targets, generation, approved)
	}) {
		return
	}
	for _, tab := range candidates {
		inherited := a.allowDirty
		d := a.docs[tabPath(tab)]
		version, ok := approved[d]
		a.allowDirty = ok && d != nil && d.buf != nil && version == d.buf.Version()
		a.closeTab(g, tab)
		a.allowDirty = inherited
	}
}
