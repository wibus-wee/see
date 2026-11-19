package app

import (
	"context"

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

func (a *App) commandEnabled(name string) bool {
	switch name {
	case "Reopen Closed Tab":
		return len(a.closed) > 0
	case "Go to Implementation":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "implementation")
	case "Go to Type Definition":
		return a.activeEditor() != nil && a.navigationEnabled(a.activePath(), "typeDefinition")
	}
	return true
}

func (a *App) navigationMenuItems(run func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range commands() {
		if cmd.name != "Go to Definition" && cmd.name != "Go to Implementation" && cmd.name != "Go to Type Definition" {
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
	state := [3]bool{a.activeEditor() != nil, a.commandEnabled("Go to Implementation"), a.commandEnabled("Go to Type Definition")}
	if state != a.navigationMenuState {
		a.navigationMenuState = state
		a.host.RefreshMenu()
	}
}
