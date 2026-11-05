package app

import (
	"path/filepath"
	"runtime"
	"see/locales"

	"github.com/egoist/mygo"
)

// dockRecentItems uses the same recent projects as the welcome window.
func dockRecentItems(paths []string, open func(string)) []*mygo.MenuItem {
	if len(paths) == 0 {
		return nil
	}
	items := []*mygo.MenuItem{{Label: locales.MsgRecentProjects(), Disabled: true}}
	for _, path := range paths {
		items = append(items, &mygo.MenuItem{
			Label: filepath.Base(path), ToolTip: path,
			Click: func(*mygo.MenuItem, *mygo.Window) { open(path) },
		})
	}
	return items
}

// Switching projects from the menu bar, as Xcode's File menu: Open
// Folder…, Open Recent, Close Folder and the welcome window.

// fileMenu is the File menu; run runs a choice on the UI thread and
// redraws. Items whose keys the view handles itself (⌘O, ⇧⌘1) show no
// accelerator, so that a key press does not act twice. Build the menu bar
// again when the recent folders change (config.onRecent).
func (a *App) fileMenu(run func(func())) *mygo.MenuItem {
	do := func(fn func()) func(*mygo.MenuItem, *mygo.Window) {
		return func(*mygo.MenuItem, *mygo.Window) { run(fn) }
	}
	var recent []*mygo.MenuItem
	for _, p := range a.cfg.Recent {
		recent = append(recent, &mygo.MenuItem{Label: filepath.Base(p), ToolTip: p, Click: do(func() { a.openRecent(p) })})
	}
	if len(recent) > 0 {
		recent = append(recent, mygo.Separator())
	}
	recent = append(recent, &mygo.MenuItem{Label: locales.MsgClearMenu(), Disabled: len(a.cfg.Recent) == 0, Click: do(a.cfg.ClearRecent)})

	closeKey := "Ctrl+Shift+W"
	if runtime.GOOS == "darwin" {
		closeKey = "Cmd+Ctrl+W" // Xcode's Close Workspace
	}
	return &mygo.MenuItem{Label: locales.MsgFile(), Submenu: []*mygo.MenuItem{
		{Label: locales.MsgNewFile(), Disabled: a.root == "" && a.showWelcome, Click: do(a.newFile)},
		{Label: locales.MsgNewWindow(), Click: do(func() {
			a.host.OpenProjectWindow("")
		})},
		{Label: locales.MsgOpenFile(), Click: do(a.openFileDialog)},
		{Label: locales.MsgOpenFolder(), Click: do(a.openFolderDialog)},
		{Label: locales.MsgOpenRecent(), Submenu: recent},
		{Label: locales.MsgSave(), Click: do(a.saveActive)},
		mygo.Separator(),
		{Label: locales.MsgCloseFolder(), Accelerator: closeKey, Click: do(a.closeFolder)},
		mygo.Separator(),
		{Label: locales.MsgWelcomeToSee(), Click: do(func() { a.showWelcome = true })},
	}}
}

// closeFolder closes the workspace and goes back to the welcome window.
func (a *App) closeFolder() {
	if a.root == "" {
		return
	}
	if a.guardDirty(a.dirtyDocs(), a.closeFolder) {
		return
	}
	a.closeWorkspace()
	a.showWelcome = true
}
