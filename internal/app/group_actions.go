package app

import (
	"see/internal/workspace"
	"slices"
)

func (a *App) cycleEditorGroup(dir int) {
	groups := a.groups()
	if len(groups) < 2 {
		return
	}
	at := slices.Index(groups, a.focus)
	if at < 0 {
		return
	}
	a.focusGroup((at + dir + len(groups)) % len(groups))
}

func (a *App) moveTabToNextEditor() {
	groups := a.groups()
	if len(groups) < 2 || a.focus == nil || a.focus.active == "" {
		return
	}
	at := slices.Index(groups, a.focus)
	if at < 0 {
		return
	}
	destination := groups[(at+1)%len(groups)]
	a.moveTab(a.focus, a.focus.active, destination, sideNone, len(destination.order))
	a.requestGroupFocus(destination)
}

func (a *App) requestGroupFocus(g *group) {
	a.focus = g
	a.focusGroupTarget = g
	a.focusEditor = nil
	a.focusImage = ""
	if isImagePath(g.active) {
		a.focusImage = g.active
	}
}

func (a *App) groupHasFocusTarget(g *group) bool {
	if v := g.diffs[g.active]; v != nil {
		return v.state != nil && v.commit == nil && v.log == nil
	}
	d := a.docs[tabPath(g.active)]
	if d == nil || d.loading || d.err != nil {
		return false
	}
	return d.image != nil || g.hexPreview[d.path] || d.kind == workspace.Binary || g.eds[d.path] != nil
}
