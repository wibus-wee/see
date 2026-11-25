package app

import "github.com/egoist/mygo"

func (a *App) selectionMenu(update func(func())) []*mygo.MenuItem {
	return a.commandMenuGroups(update,
		[]string{"selection.all"},
		[]string{"selection.nextOccurrence", "selection.allOccurrences"},
		[]string{"selection.expand", "selection.shrink"},
		[]string{"selection.splitLines"},
	)
}

func (a *App) transformMenu(update func(func())) []*mygo.MenuItem {
	return a.commandMenuGroups(update,
		[]string{"transform.upperCase", "transform.lowerCase", "transform.titleCase", "transform.snakeCase", "transform.kebabCase", "transform.camelCase", "transform.pascalCase"},
		[]string{"transform.sortLines", "transform.reverseLines", "transform.uniqueLines", "transform.joinLines", "transform.trimTrailingWhitespace"},
	)
}

func (a *App) commandMenuGroups(update func(func()), groups ...[]string) []*mygo.MenuItem {
	commandsByID := make(map[string]command, len(commands()))
	for _, cmd := range commands() {
		commandsByID[cmd.id] = cmd
	}

	var items []*mygo.MenuItem
	for _, ids := range groups {
		var groupItems []*mygo.MenuItem
		for _, id := range ids {
			cmd, ok := commandsByID[id]
			if !ok {
				continue
			}
			selected := cmd
			groupItems = append(groupItems, &mygo.MenuItem{
				Label:    commandLabel(selected.id),
				Disabled: !a.commandEnabled(selected.id),
				Click: func(*mygo.MenuItem, *mygo.Window) {
					update(func() { selected.run(a) })
				},
			})
		}
		if len(groupItems) == 0 {
			continue
		}
		if len(items) > 0 {
			items = append(items, mygo.Separator())
		}
		items = append(items, groupItems...)
	}
	return items
}
