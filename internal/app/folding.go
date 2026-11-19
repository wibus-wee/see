package app

import (
	"fmt"
	"runtime"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui/editor"
)

func foldingCommands() []command {
	mod, foldMods := "Ctrl+", "Ctrl+Shift+"
	if runtime.GOOS == "darwin" {
		mod, foldMods = "Cmd+", "Cmd+Alt+"
	}
	var cmds []command
	add := func(name, shortcut string, cmd editor.Command) {
		cmds = append(cmds, command{name: name, key: shortcut, run: func(a *App) {
			if st := a.activeEditor(); st != nil {
				st.Run(cmd)
			}
		}})
	}
	add("Fold", keyLabel(foldMods, "["), editor.Fold)
	add("Unfold", keyLabel(foldMods, "]"), editor.Unfold)
	chord := func(key string) string { return keyLabel(mod, "K") + " " + keyLabel(mod, key) }
	add("Toggle Fold", chord("L"), editor.ToggleFold)
	add("Fold Recursively", chord("["), editor.FoldRecursive)
	add("Unfold Recursively", chord("]"), editor.UnfoldRecursive)
	add("Fold All", chord("0"), editor.FoldAll)
	add("Unfold All", chord("J"), editor.UnfoldAll)
	cmds = append(cmds, command{name: "Fold Imports", run: func(a *App) {
		if st := a.activeEditor(); st != nil {
			st.FoldImports()
		}
	}})
	for n := 1; n <= 5; n++ {
		add(fmt.Sprintf("Fold Level %d", n), chord(fmt.Sprint(n)), editor.FoldLevel1+editor.Command(n-1))
	}
	return cmds
}

func (a *App) foldingMenu(update func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range foldingCommands() {
		switch cmd.name {
		case "Fold", "Unfold", "Fold All", "Unfold All", "Fold Imports":
			items = append(items, &mygo.MenuItem{Label: commandLabel(cmd.name), Click: func(*mygo.MenuItem, *mygo.Window) {
				update(func() { cmd.run(a) })
			}})
		}
	}
	return items
}
