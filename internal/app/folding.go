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
	add := func(id, name, shortcut string, cmd editor.Command) {
		cmds = append(cmds, command{id: id, name: name, key: shortcut, run: func(a *App) {
			if st := a.activeEditor(); st != nil {
				st.Run(cmd)
			}
		}})
	}
	add("fold.fold", "Fold", keyLabel(foldMods, "["), editor.Fold)
	add("fold.unfold", "Unfold", keyLabel(foldMods, "]"), editor.Unfold)
	chord := func(key string) string { return keyLabel(mod, "K") + " " + keyLabel(mod, key) }
	add("fold.toggle", "Toggle Fold", chord("L"), editor.ToggleFold)
	add("fold.recursive", "Fold Recursively", chord("["), editor.FoldRecursive)
	add("fold.unfoldRecursive", "Unfold Recursively", chord("]"), editor.UnfoldRecursive)
	add("fold.all", "Fold All", chord("0"), editor.FoldAll)
	add("fold.unfoldAll", "Unfold All", chord("J"), editor.UnfoldAll)
	cmds = append(cmds, command{id: "fold.imports", name: "Fold Imports", run: func(a *App) {
		if st := a.activeEditor(); st != nil {
			st.FoldImports()
		}
	}})
	for n := 1; n <= 5; n++ {
		add(fmt.Sprintf("fold.level%d", n), fmt.Sprintf("Fold Level %d", n), chord(fmt.Sprint(n)), editor.FoldLevel1+editor.Command(n-1))
	}
	return cmds
}

func (a *App) foldingMenu(update func(func())) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, cmd := range foldingCommands() {
		switch cmd.id {
		case "fold.fold", "fold.unfold", "fold.all", "fold.unfoldAll", "fold.imports":
			items = append(items, &mygo.MenuItem{Label: commandLabel(cmd.id), Click: func(*mygo.MenuItem, *mygo.Window) {
				update(func() { cmd.run(a) })
			}})
		}
	}
	return items
}
