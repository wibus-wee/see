package app

import "slices"

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
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}
