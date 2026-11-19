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
		return "Go to Implementation"
	}
	return "Go to Type Definition"
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
func (a *App) commandEnabled(name string) bool {
	g := a.focus
	switch name {
	case "Reopen Closed Tab":
		return len(a.closed) > 0
	case "Edit All in Scope":
		d, st := a.scopeEditDocument()
		return d != nil && st != nil && a.serverFor(d.path) != nil
	case "Go to Implementation":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "implementation")
	case "Go to Type Definition":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "typeDefinition")
	case "Peek Definition":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "peek")
	// Commands that only run an editor command or read the editor.
	case "Find in File", "Replace in File", "Go to Definition",
		"Next Problem", "Previous Problem", "Go to Line", "Search File Symbols",
		"Show Document Items", "Fold", "Unfold", "Toggle Fold",
		"Fold Recursively", "Unfold Recursively", "Fold All", "Unfold All",
		"Fold Imports", "Fold Level 1", "Fold Level 2", "Fold Level 3",
		"Fold Level 4", "Fold Level 5":
		return a.activeEditor() != nil
	// References and callers need a language server for the file; Pin
	// References also pins a list the peek already shows.
	case "Find References", "Find Callers":
		return a.activeEditor() != nil && a.serverFor(a.activePath()) != nil
	case "Pin References":
		return a.choices != nil && a.choices.title == "references" && len(a.choices.locs) > 0 ||
			a.activeEditor() != nil && a.serverFor(a.activePath()) != nil
	// Workspace-wide views and searches need an open folder.
	case "Find in Workspace", "Replace in Workspace", "Search Workspace Symbols",
		"Filter Files", "Open Quickly", "Show Outline", "Show Group Files":
		return a.root != ""
	// Commands that only read or act on the repository.
	case "Show Git", "Git: Show History", "Git: Show Log", "Next Change",
		"Previous Change", "Toggle Inline Diff", "Toggle Inline Git Blame":
		return a.git.root != ""
	case "Git: Fetch", "Git: Pull", "Git: Push", "Git: Switch Branch…":
		return a.git.root != "" && !a.git.busy
	// Go tasks need a folder; an untrusted one asks first, which is
	// still something the command does.
	case "Go: Build Workspace", "Go: Test Workspace", "Tasks: Stop", "Tasks: Show",
		"Show Output":
		return a.root != ""
	// Markdown preview needs a markdown tab open.
	case "Open Markdown Preview":
		return g != nil && isMarkdown(tabPath(g.active)) && a.docs[tabPath(g.active)] != nil
	case "Show Line's Commit in Git Log":
		return a.canRevealLineCommit()
	case "Show File History":
		return a.canShowFileHistory()
	case "Git: Reveal HEAD in Log", "Git: Jump to Date…":
		return a.git.root != ""
	// Tab and group commands need a group with tabs, or more than one.
	case "Next Tab", "Previous Tab":
		return g != nil && len(g.order) >= 2
	case "Move Tab to Next Editor", "Focus Next Editor", "Focus Previous Editor":
		return len(a.groups()) >= 2 && (name != "Move Tab to Next Editor" || g != nil && g.active != "")
	case "Save":
		return a.activeEditor() != nil
	case "Save All":
		return len(a.dirtyDocs()) > 0
	case "Close Tab":
		return g == nil || g.active != "" // nil focus closes the window
	case "Close Other Tabs", "Close Tabs to the Right":
		return g != nil && len(g.order) >= 2
	case "Close Saved Tabs":
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
	case "Keep Preview Tab":
		return g != nil && g.preview != ""
	case "Go Back":
		return len(a.hist.Back) > 0
	case "Go Forward":
		return len(a.hist.Forward) > 0
	case "Select Last Tab":
		return g != nil && len(g.order) > 0
	}
	if strings.HasPrefix(name, "Select Tab ") {
		return g != nil && len(g.order) > 0
	}
	return true
}

func (a *App) navigationMenuItems(run func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range commands() {
		if cmd.name != "Go to Definition" && cmd.name != "Peek Definition" && cmd.name != "Go to Implementation" && cmd.name != "Go to Type Definition" {
			continue
		}
		items = append(items, &mygo.MenuItem{
			Label: commandLabel(cmd.name), Disabled: a.activeEditor() == nil || !a.commandEnabled(cmd.name),
			Click: func(*mygo.MenuItem, *mygo.Window) { run(func() { cmd.run(a) }) },
		})
	}
	return items
}

func (a *App) refreshNavigationMenu() {
	state := [4]bool{a.activeEditor() != nil, a.commandEnabled("Go to Implementation"), a.commandEnabled("Go to Type Definition"), a.commandEnabled("Peek Definition")}
	if state != a.navigationMenuState {
		a.navigationMenuState = state
		a.host.RefreshMenu()
	}
}
