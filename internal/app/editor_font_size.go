package app

import (
	"github.com/egoist/mygo"
)

func (a *App) editorFontSizeMenu(run func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range commands() {
		var accelerator string
		switch cmd.id {
		case "font.bigger":
			accelerator = "CmdOrCtrl+="
		case "font.smaller":
			accelerator = "CmdOrCtrl+-"
		case "font.reset":
		default:
			continue
		}
		items = append(items, &mygo.MenuItem{
			Label: commandLabel(cmd.id), Accelerator: accelerator,
			Click: func(*mygo.MenuItem, *mygo.Window) { run(func() { cmd.run(a) }) },
		})
	}
	return items
}
