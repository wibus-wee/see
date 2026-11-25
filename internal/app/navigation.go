package app

import (
	"context"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/locales"
)

type implementationClient interface {
	HasImplementation() bool
	Implementation(context.Context, string, lsp.Position) ([]lsp.Location, error)
}

type typeDefinitionClient interface {
	HasTypeDefinition() bool
	TypeDefinition(context.Context, string, lsp.Position) ([]lsp.Location, error)
}

func supportsNavigation(c langserver.Client, kind string) bool {
	switch kind {
	case "typeHierarchy":
		p, ok := c.(typeHierarchyClient)
		return ok && p.HasTypeHierarchy()
	case "implementation":
		p, ok := c.(implementationClient)
		return ok && p.HasImplementation()
	case "typeDefinition":
		p, ok := c.(typeDefinitionClient)
		return ok && p.HasTypeDefinition()
	default:
		// "definition" and "peek", which asks for the same locations.
		return c.HasDefinition()
	}
}

func navigationCommand(kind string) string {
	if kind == "implementation" {
		return "implementation.goto"
	}
	return "typeDefinition.goto"
}

func navigationText(kind, suffix string) string {
	return locales.Catalog.Format(i18n.Locale(), kind+suffix, nil)
}

func (a *App) navigationEnabled(path, kind string) bool {
	m := a.serverFor(path)
	if m == nil {
		return false
	}
	c := m.Current()
	return c == nil || supportsNavigation(c, kind)
}

func (a *App) navigateActive(kind string) {
	g := a.focus
	if g == nil {
		return
	}
	d := a.docs[g.active]
	st := a.activeEditor()
	if d != nil && st != nil && a.navigationEnabled(d.path, kind) {
		a.goToLocation(g, d, st.Buffer.Offset(st.CursorPos()), kind)
	}
}

// commandEnabled reports whether a palette or menu command does
// anything now, so commands that would silently no-op show disabled
// instead.
func (a *App) commandEnabled(id string) bool {
	g := a.focus
	switch id {
	case "editorGroup.splitRight", "editorGroup.splitDown":
		return g != nil && g.active != ""
	case "hierarchy.types":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "typeHierarchy")
	case "tab.reopen":
		return len(a.closed) > 0
	case "history.local":
		d := a.docs[a.activePath()]
		return a.localHistorySupported(d) && a.activeEditor() != nil
	case "history.compare":
		d := a.docs[a.activePath()]
		return a.activeEditor() != nil && d != nil && !d.untitled && d.buf != nil
	case "edit.allInScope":
		d, st := a.scopeEditDocument()
		return d != nil && st != nil && a.serverFor(d.path) != nil
	case "symbol.rename":
		d, st := a.renameDocument()
		return d != nil && st != nil && a.renameEnabled(d.path)
	case "implementation.goto":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "implementation")
	case "typeDefinition.goto":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "typeDefinition")
	case "definition.peek":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "peek")
	case "format.document":
		document, _ := a.formatEnabled(a.activePath())
		return a.activeEditor() != nil && document
	case "format.selection":
		_, rng := a.formatEnabled(a.activePath())
		st := a.activeEditor()
		return st != nil && rng && hasSelection(st)
	case "selection.all", "selection.nextOccurrence", "selection.allOccurrences", "selection.expand", "selection.shrink":
		return a.activeEditor() != nil
	case "selection.splitLines":
		st := a.activeEditor()
		return st != nil && hasSelection(st)
	case "transform.upperCase", "transform.lowerCase", "transform.titleCase", "transform.snakeCase",
		"transform.kebabCase", "transform.camelCase", "transform.pascalCase", "transform.sortLines",
		"transform.reverseLines", "transform.uniqueLines", "transform.joinLines", "transform.trimTrailingWhitespace":
		st := a.activeEditor()
		return st != nil && !st.ReadOnly
	// Commands that only run an editor command or read the editor.
	case "find.file", "find.replaceFile", "definition.goto",
		"problem.next", "problem.previous", "method.next", "method.previous", "line.goto", "symbols.file",
		"jumpbar.documentItems", "fold.fold", "fold.unfold", "fold.toggle",
		"fold.recursive", "fold.unfoldRecursive", "fold.all", "fold.unfoldAll",
		"fold.imports", "fold.level1", "fold.level2", "fold.level3",
		"fold.level4", "fold.level5":
		return a.activeEditor() != nil
	// References, callers and the hierarchy need a language server for
	// the file; Pin References also pins a list the peek already shows.
	case "references.find", "references.callers", "hierarchy.calls":
		return a.activeEditor() != nil && a.serverFor(a.activePath()) != nil
	case "references.pin":
		return a.choices != nil && a.choices.title == "references" && len(a.choices.locs) > 0 ||
			a.activeEditor() != nil && a.serverFor(a.activePath()) != nil
	// Workspace-wide views and searches need an open folder.
	case "search.workspace", "search.replaceWorkspace", "symbols.workspace",
		"files.filter", "file.openQuickly", "outline.show", "jumpbar.groupFiles":
		return a.root != ""
	case "search.editResults":
		return len(a.find.results) > 0 && !a.find.running
	case "navigator.reveal":
		return a.canRevealInProjectNavigator(a.activeProjectFile())
	// Commands that only read or act on the repository.
	case "git.show", "git.showHistory", "git.showLog", "change.next",
		"change.previous", "diff.toggleInline", "git.toggleBlame":
		return a.git.root != ""
	case "git.fetch", "git.pull", "git.push", "git.switchBranch":
		return a.git.root != "" && !a.git.busy
	// Go tasks need a folder; an untrusted one asks first, which is
	// still something the command does.
	case "go.build", "go.test", "tasks.stop", "tasks.show",
		"output.show":
		return a.root != ""
	// Markdown preview needs a markdown tab open.
	case "markdown.preview":
		return g != nil && isMarkdown(tabPath(g.active)) && a.docs[tabPath(g.active)] != nil
	case "git.revealLineCommit":
		return a.canRevealLineCommit()
	case "change.discard":
		return a.canDiscardChange()
	case "history.file":
		return a.canShowFileHistory()
	case "git.revealHead", "git.jumpToDate":
		return a.git.root != ""
	// Tab and group commands need a group with tabs, or more than one.
	case "tab.next", "tab.previous":
		return g != nil && len(g.order) >= 2
	case "tab.moveToNextEditor", "editorGroup.focusNext", "editorGroup.focusPrevious":
		return len(a.groups()) >= 2 && (id != "tab.moveToNextEditor" || g != nil && g.active != "")
	case "file.save":
		return a.activeEditor() != nil
	case "file.saveAll":
		return len(a.dirtyDocs()) > 0
	case "tab.close":
		return g == nil || g.active != "" // nil focus closes the window
	case "tab.closeOthers", "tab.closeRight":
		return g != nil && len(g.order) >= 2
	case "tab.closeSaved":
		if g == nil {
			return false
		}
		for _, tab := range g.order {
			if d := a.docs[tabPath(tab)]; d != nil && d.buf != nil && d.buf.Modified() {
				continue
			}
			return true
		}
		return false
	case "tab.keepPreview":
		return g != nil && g.preview != ""
	case "nav.back":
		return len(a.hist.Back) > 0
	case "nav.forward":
		return len(a.hist.Forward) > 0
	case "tab.selectLast":
		return g != nil && len(g.order) > 0
	}
	if strings.HasPrefix(id, "tab.select") {
		return g != nil && len(g.order) > 0
	}
	return true
}

func (a *App) navigationMenuItems(run func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range commands() {
		if cmd.id != "definition.goto" && cmd.id != "definition.peek" && cmd.id != "implementation.goto" && cmd.id != "typeDefinition.goto" && cmd.id != "hierarchy.calls" && cmd.id != "hierarchy.types" && cmd.id != "method.next" && cmd.id != "method.previous" {
			continue
		}
		items = append(items, &mygo.MenuItem{
			Label: commandLabel(cmd.id), Disabled: a.activeEditor() == nil || !a.commandEnabled(cmd.id),
			Click: func(*mygo.MenuItem, *mygo.Window) { run(func() { cmd.run(a) }) },
		})
	}
	return items
}

func (a *App) refreshNavigationMenu() {
	state := [8]bool{a.activeEditor() != nil, a.commandEnabled("implementation.goto"), a.commandEnabled("typeDefinition.goto"), a.commandEnabled("definition.peek"), a.commandEnabled("hierarchy.types"), a.canDiscardChange(), a.commandEnabled("selection.splitLines"), a.commandEnabled("transform.upperCase")}
	if state != a.navigationMenuState {
		a.navigationMenuState = state
		a.host.RefreshMenu()
	}
}
