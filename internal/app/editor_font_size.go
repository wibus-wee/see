package app

import (
	"github.com/egoist/mygo"
)

func (a *App) editorFontSizeMenu(run func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range commands() {
		var accelerator string
		switch cmd.name {
		case "Bigger":
			accelerator = "CmdOrCtrl+="
		case "Smaller":
			accelerator = "CmdOrCtrl+-"
		case "Reset Editor Font Size":
		default:
			continue
		}
		items = append(items, &mygo.MenuItem{
			Label: commandLabel(cmd.name), Accelerator: accelerator,
			Click: func(*mygo.MenuItem, *mygo.Window) { run(func() { cmd.run(a) }) },
		})
	}
	return items
}
