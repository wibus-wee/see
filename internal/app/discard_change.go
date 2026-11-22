package app

import "github.com/egoist/mygo/ui/editor"

func gitHunkAt(diff *editor.DiffState, line int) int {
	if diff == nil || diff.New == nil {
		return -1
	}
	for i, h := range diff.Hunks() {
		if line >= h.NewStart && line < max(h.NewEnd, h.NewStart+1) || h.NewStart >= diff.New.LineCount() && line == diff.New.LineCount()-1 {
			return i
		}
	}
	return -1
}

func discardBufferChange(diff *editor.DiffState, line int) bool {
	i := gitHunkAt(diff, line)
	if i < 0 {
		return false
	}
	diff.Revert(i)
	return true
}

func (a *App) canDiscardChange() bool {
	st := a.activeEditor()
	if a.focus == nil || st == nil || st.ReadOnly {
		return false
	}
	d := a.docs[a.focus.active]
	return d != nil && gitHunkAt(d.gitChanges, st.CursorPos().Line) >= 0
}

func (a *App) discardChange() {
	if a.canDiscardChange() {
		a.discardChangeAt(a.docs[a.focus.active], a.activeEditor(), a.activeEditor().CursorPos().Line)
	}
}

func (a *App) discardChangeAt(d *doc, st *editor.State, line int) {
	if !st.ReadOnly && discardBufferChange(d.gitChanges, line) {
		a.git.peek = nil
	}
}
