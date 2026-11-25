package app

import (
	"runtime"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/locales"
)

func editorContextMenuText(key string, args map[string]any) string {
	return locales.Catalog.Format(i18n.Locale(), key, args)
}

func (a *App) editorContextMenu(menu *ui.Menu, g *group, d *doc, st *editor.State) {
	if menu.Item(commandLabel("definition.goto")).Shortcut(0, ui.KeyF12).Chosen() && st.OnDefinition != nil {
		a.focus = g
		st.OnDefinition(st.Buffer.Offset(st.CursorPos()))
	}
	if menu.Item(commandLabel("references.find")).Shortcut(ui.Shift, ui.KeyF12).Chosen() {
		a.focus = g
		a.findUsagesAt(false, g.pointerAnchor, g.pointerInEditor)
	}
	for _, kind := range []string{"implementation", "typeDefinition"} {
		item := menu.Item(commandLabel(navigationCommand(kind))).Disabled(!a.navigationEnabled(d.path, kind))
		if kind == "implementation" {
			item.Shortcut(ui.Cmd, ui.KeyF12)
		}
		if item.Chosen() {
			a.goToLocation(g, d, st.Buffer.Offset(st.CursorPos()), kind)
		}
	}
	if menu.Item(commandLabel("hierarchy.calls")).Chosen() {
		a.focus = g
		a.showCallHierarchyAt(g)
	}
	if menu.Item(commandLabel("definition.peek")).Shortcut(ui.Alt, ui.KeyF12).Disabled(!a.navigationEnabled(d.path, "peek")).Chosen() {
		a.peekDefinition(g, d, st.Buffer.Offset(st.CursorPos()))
	}
	if _, ok := g.regexAtCaret(d, st); ok {
		if menu.Item(commandLabel("regex.playground")).Chosen() {
			a.openRegexPlaygroundFor(g, d, st)
		}
	}

	menu.Separator()
	if menu.Item(commandLabel("symbol.rename")).Disabled(st.ReadOnly || !a.renameEnabled(d.path)).Shortcut(0, ui.KeyF2).Chosen() {
		a.renameSymbolAt(g, d, st)
	}
	if menu.Item(commandLabel("codeAction.show")).Disabled(st.ReadOnly || !a.codeActionsEnabled(d.path)).Chosen() {
		a.codeActionAtPoint(g, d, st, false)
	}
	_, _, mods, key := editAllScopeShortcut(runtime.GOOS)
	if menu.Item(commandLabel("edit.allInScope")).Disabled(!a.scopeEditCommandEnabled(g, d, st)).Shortcut(mods, key).Chosen() {
		a.editAllInScopeFor(g, d, st)
	}
	formatDoc, formatRange := a.formatEnabled(d.path)
	if menu.Item(commandLabel("format.document")).Disabled(st.ReadOnly || !formatDoc).Shortcut(ui.Shift|ui.Alt, ui.KeyF).Chosen() {
		a.formatAt(g, d, st, false)
	}
	if !st.ReadOnly && formatRange && hasSelection(st) {
		if menu.Item(commandLabel("format.selection")).Chosen() {
			a.formatAt(g, d, st, true)
		}
	}
	if !st.ReadOnly && menu.Item(editorContextMenuText("editorContextToggleComment", nil)).Shortcut(ui.Cmd, ui.KeySlash).Chosen() {
		st.Run(editor.ToggleComment)
	}

	menu.Separator()
	if menu.Item(editorContextMenuText("editorContextCut", nil)).Shortcut(ui.Cmd, ui.KeyX).Disabled(st.ReadOnly).Chosen() {
		st.Run(editor.Cut)
	}
	if menu.Item(editorContextMenuText("editorContextCopy", nil)).Shortcut(ui.Cmd, ui.KeyC).Chosen() {
		st.Run(editor.Copy)
	}
	if menu.Item(editorContextMenuText("editorContextPaste", nil)).Shortcut(ui.Cmd, ui.KeyV).Disabled(st.ReadOnly).Chosen() {
		st.Run(editor.Paste)
	}

	menu.Separator()
	menu.Submenu(editorContextMenuText("editorContextHistoryCompare", nil), func(m *ui.Menu) {
		if b, ok := a.lineBlame(d, st); ok && !b.Uncommitted {
			if m.Item(commandLabel("git.revealLineCommit")).Chosen() {
				a.revealLogCommit(b.Hash)
			}
			m.Separator()
		}
		if !d.untitled && m.Item(commandLabel("history.file")).Chosen() {
			a.showFileHistory(d.path)
		}
		localHistoryItem := m.Item(commandLabel("history.local")).Disabled(!a.localHistorySupported(d))
		if !a.localHistorySupported(d) {
			localHistoryItem.Tooltip(localHistoryText("localHistoryUnsupported"))
		}
		if localHistoryItem.Chosen() {
			a.showLocalHistory(g, d)
		}
		if m.Item(commandLabel("history.compare")).Chosen() {
			a.showRevision(g, d, "")
		}
	})
	if !d.untitled {
		menu.Submenu(editorContextMenuText("editorContextCopyShare", nil), func(m *ui.Menu) {
			a.linkItems(m, d, st)
		})
	}
}
