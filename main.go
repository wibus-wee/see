// Native Source Reader is a desktop source code reader: a file tree,
// tabs, a read-only code editor and Go to Definition through gopls, drawn
// natively by MyGo's ui, with no web view.
package main

import (
	"log"
	"os"
	"runtime"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	cfg := loadConfig(configPath())
	a := newApp(cfg)
	var (
		win, welcome *mygo.Window
		wins         = &windows{a: a}
		quitting     bool
	)
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) { quitting = true })
	// Light, dark, or the system's, from the start.
	a.setAppearance = func(s string) { mygo.Theme.SetSource(themeSource(s)) }
	a.setAppearance(cfg.Appearance)
	mygo.App.SetName("Native Source Reader")
	mygo.App.WhenReady(func() {
		a.version = mygo.App.Version()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Native Source Reader",
			Width:     1280,
			Height:    820,
			MinWidth:  480,
			MinHeight: 320,
			// The toolbar shares a line with the window controls, and the
			// navigator shows the desktop's material through it, as
			// Xcode's: macOS's sidebar material, Mica on Windows 11, and
			// a plain background on Linux, which has none. A compact bar:
			// macOS's inset style would add a toolbar 52 points high, so
			// the controls are placed in the middle of ours instead.
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       toolbarHeight,
			TrafficLightPosition: &mygo.Point{X: 14, Y: (toolbarHeight - trafficLightSize) / 2},
			Vibrancy:             mygo.VibrancySidebar,
			Hidden:               true, // until a folder opens (windows.sync)
			Content:              ui.View(func(c *ui.Context) { a.view(c); wins.sync(win) }),
		})
		// The welcome window, as Xcode's: of its own, of one size, its
		// buttons over its left side.
		welcome = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Welcome to Native Source Reader",
			Width:           welcomeWidth,
			Height:          welcomeHeight,
			DisableResize:   true,
			DisableMinimize: true,
			DisableMaximize: true,
			TitleBarStyle:   mygo.TitleBarHiddenInset,
			Hidden:          true,
			Content:         ui.View(func(c *ui.Context) { a.welcomeView(c); wins.sync(welcome) }),
		})
		wins.main, wins.welcome = win, welcome
		a.post = func(fn func()) { win.Update(func() { fn(); wins.sync(nil) }) }
		a.setTitle = win.SetTitle
		// Closing either window hides it, unless the app is quitting: the
		// main window's closes the folder and goes back to the welcome
		// window, and the welcome window's leaves the app running, as
		// Xcode does, on macOS; elsewhere, with no Dock to come back
		// from, the app quits with no folder open.
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Update(wins.closeMain)
			}
		})
		welcome.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Update(func() {
					if wins.closeWelcome(runtime.GOOS == "darwin") {
						mygo.App.Quit()
					}
				})
			}
		})
		mygo.App.OnActivate(func(visible bool) {
			if !visible {
				win.Update(wins.reopen)
			}
		})
		a.openSettings, a.closeSettings = settingsWindow(a)
		// The menu bar, built again as the recent folders and the
		// appearance change, since its items show them.
		setMenu := func() {
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				appMenu(a),
				a.fileMenu(win.Update),
				{Role: mygo.RoleEditMenu},
				{Label: "View", Submenu: []*mygo.MenuItem{
					{Label: "Appearance", Submenu: appearanceMenu(a)},
				}},
				{Role: mygo.RoleWindowMenu},
			}))
		}
		cfg.onRecent = setMenu
		a.refreshMenu = setMenu
		setMenu()
		a.pickFolder = func() (string, error) {
			parent := win // on the window in front; this runs off the UI thread
			if wins.welcomeUp.Load() {
				parent = welcome
			}
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: parent, Title: "Open Folder", Directory: true})
			if err != nil || len(paths) == 0 {
				return "", err
			}
			return paths[0], nil
		}
		// A folder on the command line, else the welcome window, or the
		// last folder when the user asked for it at launch.
		a.showWelcome = true // until a folder opens
		defer wins.sync(nil)
		switch {
		case len(os.Args) > 1:
			a.openWorkspace(os.Args[1])
		case cfg.LastWorkspace != "" && cfg.ReopenLast:
			if _, err := os.Stat(cfg.LastWorkspace); err != nil {
				a.welcomeNote = "The last folder, " + cfg.LastWorkspace + ", is no longer there."
			} else {
				a.openWorkspace(cfg.LastWorkspace)
			}
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		if a.saveTimer != nil {
			a.saveConfig() // a save still waiting
		}
		if a.wsCancel != nil {
			a.wsCancel()
		}
		a.closeTerminals() // the shells get SIGHUP, as when a terminal app quits
		if a.lsp != nil {
			a.lsp.stopWait(2 * time.Second)
		}
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}

func themeSource(s string) mygo.ThemeSource {
	switch s {
	case "light":
		return mygo.ThemeLight
	case "dark":
		return mygo.ThemeDark
	}
	return mygo.ThemeSystem
}

// appearanceMenu chooses the app's appearance: the system's, light or
// dark, as the settings of Xcode and Finder do.
func appearanceMenu(a *app) []*mygo.MenuItem {
	var items []*mygo.MenuItem
	for _, o := range []struct{ label, value string }{{"System", ""}, {"Light", "light"}, {"Dark", "dark"}} {
		items = append(items, &mygo.MenuItem{
			Label:   o.label,
			Type:    mygo.MenuItemRadio,
			Checked: a.cfg.Appearance == o.value,
			Click:   func(*mygo.MenuItem, *mygo.Window) { a.chooseAppearance(o.value) },
		})
	}
	return items
}

// appMenu is the app's menu, with Settings… as macOS apps have it.
func appMenu(a *app) *mygo.MenuItem {
	return &mygo.MenuItem{Label: "Native Source Reader", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		mygo.Separator(),
		{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.openSettings() }},
		mygo.Separator(),
		{Role: mygo.RoleServices},
		mygo.Separator(),
		{Role: mygo.RoleHide},
		{Role: mygo.RoleHideOthers},
		{Role: mygo.RoleUnhide},
		mygo.Separator(),
		{Role: mygo.RoleQuit},
	}}
}

// settingsWindow returns what opens the settings window, one at a time,
// and what closes it: a window of its own, its sidebar over the
// desktop's material, as macOS's System Settings.
func settingsWindow(a *app) (open, close func()) {
	var w *mygo.Window
	open = func() {
		if w != nil && !w.IsDestroyed() {
			w.Focus()
			return
		}
		a.settings = settingsState{}
		w = mygo.NewWindow(mygo.WindowOptions{
			Title:                "Settings",
			Width:                760,
			Height:               560,
			MinWidth:             620,
			MinHeight:            400,
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       settingsBarHeight,
			TrafficLightPosition: &mygo.Point{X: 20, Y: (settingsBarHeight - trafficLightSize) / 2},
			Vibrancy:             mygo.VibrancySidebar,
			Content:              ui.View(a.settingsView),
		})
	}
	close = func() {
		if w != nil && !w.IsDestroyed() {
			w.Close()
		}
	}
	return open, close
}

// trafficLightSize is the height of macOS's window buttons, which the
// title bars center them by.
const trafficLightSize = 14

// settingsBarHeight is the settings window's title bar, as tall as
// System Settings'.
const settingsBarHeight = 52
