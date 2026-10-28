// See is a desktop source code reader: a file tree,
// tabs, a code editor and Go to Definition through language servers, drawn
// natively by MyGo's ui, with no web view.
package main

import (
	"log"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"
)

func dirtyDialog(parent *mygo.Window) func(string) (int, error) {
	return func(message string) (int, error) {
		result, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent: parent, Type: mygo.MessageWarning, Title: "Unsaved Changes",
			Message: message, Detail: "Do you want to save your changes?",
			Buttons: []string{"Save", "Discard Changes", "Cancel"}, CancelButton: 2,
		})
		return result.Button, err
	}
}

func main() {
	startShellPath()
	mygo.Use(native.Plugin)
	cfg := loadAppConfig(configPath())
	a := newApp(cfg)
	var (
		win, welcome  *mygo.Window
		wins          = &windows{a: a}
		quitting      bool
		quitApproved  bool
		projects      []*projectWindow
		active        *projectWindow
		varSetMenu    func()
		createProject func(string)
	)
	mygo.App.OnBeforeQuit(func(e *mygo.QuitEvent) {
		if !quitApproved {
			for _, project := range projects {
				if len(project.app.dirtyDocs()) > 0 {
					e.PreventDefault()
					approveProjectQuit(append([]*projectWindow(nil), projects...), func() { quitApproved = true; mygo.App.Quit() })
					return
				}
			}
		}
		quitApproved = false
		quitting = true
	})
	// Light, dark, or the system's, from the start.
	a.setAppearance = func(s string) { mygo.Theme.SetSource(themeSource(s)) }
	a.setAppearance(cfg.Appearance)
	mygo.App.SetName("See")
	mygo.App.WhenReady(func() {
		a.version = mygo.App.Version()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "See",
			Width:     defaultWindowWidth,
			Height:    defaultWindowHeight,
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
			TrafficLightPosition: &mygo.Point{X: 20, Y: (toolbarHeight - trafficLightSize) / 2},
			Vibrancy:             mygo.VibrancySidebar,
			Hidden:               true, // until a folder opens (windows.sync)
			Content:              ui.View(func(c *ui.Context) { a.view(c); wins.sync(win) }),
		})
		// The welcome window, as Xcode's: of its own, of one size, its
		// buttons over its left side.
		welcome = mygo.NewWindow(mygo.WindowOptions{
			Title:           "Welcome to See",
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
		primary := &projectWindow{app: a, win: win}
		projects = append(projects, primary)
		active = primary
		activate := func(p *projectWindow) {
			active = p
			if varSetMenu != nil {
				varSetMenu()
			}
		}
		win.OnFocus(func() { activate(primary) })
		welcome.OnFocus(func() { activate(primary) })
		createProject = func(path string) {
			a.showWelcome = false
			wins.sync(nil)
			if path != "" {
				if root, err := canonicalRoot(path); err == nil {
					for _, p := range projects {
						if p.app.root == root {
							p.win.Show()
							p.win.Focus()
							return
						}
					}
				}
			}
			child := newApp(cfg)
			child.version = a.version
			child.setAppearance = a.setAppearance
			width, height := defaultWindowWidth, defaultWindowHeight
			if path == "" {
				width, height = welcomeWidth, welcomeHeight
			}
			var childWin *mygo.Window
			childWin = mygo.NewWindow(mygo.WindowOptions{
				Title: "See", Width: width, Height: height, MinWidth: 480, MinHeight: 320,
				DisableResize: path == "", DisableMinimize: path == "", DisableMaximize: path == "",
				TitleBarStyle: mygo.TitleBarHidden, TitleBarHeight: toolbarHeight,
				TrafficLightPosition: &mygo.Point{X: 20, Y: (toolbarHeight - trafficLightSize) / 2},
				Vibrancy:             mygo.VibrancySidebar, Content: ui.View(child.view),
			})
			p := &projectWindow{app: child, win: childWin}
			projects = append(projects, p)
			child.post = func(fn func()) { childWin.Update(fn) }
			child.setWorkspaceSize = func(width, height int) {
				childWin.SetResizable(true)
				childWin.SetMinimizable(true)
				childWin.SetMaximizable(true)
				childWin.SetSize(width, height)
				childWin.Center()
			}
			childWin.OnResize(func() {
				childWin.Update(func() {
					if !childWin.IsMaximized() && !childWin.IsFullScreen() {
						width, height := childWin.Size()
						child.resizedWorkspace(width, height)
					}
				})
			})
			welcomeSize := path == ""
			child.setTitle = func(title string) {
				childWin.SetTitle(title)
				wantWelcome := child.root == ""
				if wantWelcome != welcomeSize {
					welcomeSize = wantWelcome
					if wantWelcome {
						childWin.SetSize(welcomeWidth, welcomeHeight)
						childWin.SetResizable(false)
						childWin.SetMinimizable(false)
						childWin.SetMaximizable(false)
					}
					childWin.Center()
				}
			}
			child.popupMenu = func(x, y int, items []*mygo.MenuItem) { go mygo.NewMenu(items).PopupAt(childWin, x, y) }
			child.pickFolder = func() (string, error) {
				paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: childWin, Title: "Open Folder", Directory: true})
				if err != nil || len(paths) == 0 {
					return "", err
				}
				return paths[0], nil
			}
			child.pickNewFile = func(root string) (string, error) {
				return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: childWin, Title: "New File", DefaultPath: root, ButtonLabel: "Create"})
			}
			child.confirmDirty = dirtyDialog(childWin)
			child.openProjectWindow = createProject
			child.newProjectWindow = func() { createProject("") }
			child.openSettings, child.closeSettings = settingsWindow(child)
			child.refreshMenu = func() {
				if varSetMenu != nil {
					varSetMenu()
				}
			}
			childWin.OnFocus(func() { activate(p) })
			childWin.OnClose(func(e *mygo.CloseEvent) {
				if quitting {
					return
				}
				e.PreventDefault()
				childWin.Update(func() {
					var closeProject func()
					closeProject = func() {
						if child.guardDirty(child.dirtyDocs(), closeProject) {
							return
						}
						child.closeWorkspace()
						projects = slices.DeleteFunc(projects, func(item *projectWindow) bool { return item == p })
						if child.closeSettings != nil {
							child.closeSettings()
						}
						childWin.Destroy()
						if active == p {
							activate(primary)
						}
						if len(projects) == 1 && a.root == "" {
							wins.reopen()
						}
					}
					closeProject()
				})
			})
			activate(p)
			if path != "" {
				child.openWorkspace(path)
			}
			childWin.Show()
			childWin.Focus()
		}
		a.openProjectWindow = createProject
		a.newProjectWindow = func() { createProject("") }

		a.post = func(fn func()) { win.Update(func() { fn(); wins.sync(nil) }) }
		if shellPathReady != nil {
			go func() {
				<-shellPathReady
				win.Update(func() {
					for _, project := range projects {
						if project.app.refreshSettings != nil {
							project.app.refreshSettings()
						}
					}
				})
			}()
		}
		a.setTitle = win.SetTitle
		a.setWorkspaceSize = func(width, height int) { win.SetSize(width, height); win.Center() }
		win.OnResize(func() {
			win.Update(func() {
				if !win.IsMaximized() && !win.IsFullScreen() {
					width, height := win.Size()
					a.resizedWorkspace(width, height)
				}
			})
		})
		// The project switcher is a native menu, as Xcode's Open Recent:
		// PopupAt waits for the menu to close, so it runs off the UI
		// thread, and its choices come back through post.
		a.popupMenu = func(x, y int, items []*mygo.MenuItem) {
			go mygo.NewMenu(items).PopupAt(win, x, y)
		}
		// Closing either window hides it, unless the app is quitting: the
		// main window's closes the folder and goes back to the welcome
		// window, and the welcome window's leaves the app running, as
		// Xcode does, on macOS; elsewhere, with no Dock to come back
		// from, the app quits with no folder open.
		win.OnClose(func(e *mygo.CloseEvent) {
			if !quitting {
				e.PreventDefault()
				win.Update(func() {
					if len(projects) <= 1 {
						wins.closeMain()
						return
					}
					var closePrimary func()
					closePrimary = func() {
						if a.guardDirty(a.dirtyDocs(), closePrimary) {
							return
						}
						a.closeWorkspace()
						a.showWelcome = false
						wins.sync(nil)
					}
					closePrimary()
				})
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
			current := active
			if current == nil {
				current = primary
			}
			currentApp, currentWin := current.app, current.win
			mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
				appMenu(currentApp), currentApp.fileMenu(currentWin.Update), {Role: mygo.RoleEditMenu},
				{Label: "View", Submenu: []*mygo.MenuItem{
					{Label: "Command Palette…", Click: func(*mygo.MenuItem, *mygo.Window) { currentWin.Update(currentApp.openPalette) }},
					{Label: "Appearance", Submenu: appearanceMenu(currentApp)},
				}}, {Role: mygo.RoleWindowMenu},
			}))
		}
		varSetMenu = setMenu
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
		a.pickNewFile = func(root string) (string, error) {
			return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: win, Title: "New File", DefaultPath: root, ButtonLabel: "Create"})
		}
		a.confirmDirty = dirtyDialog(win)
		// A folder on the command line, else the welcome window, or the
		// last folder when the user asked for it at launch.
		a.showWelcome = true // until a folder opens
		defer wins.sync(nil)
		switch {
		case len(os.Args) > 1:
			a.openWorkspace(os.Args[1])
		case filepath.Base(os.Args[0]) == "see":
			a.openWorkspace(".")
		case cfg.LastWorkspace != "" && cfg.ReopenLast:
			if _, err := os.Stat(cfg.LastWorkspace); err != nil {
				a.welcomeNote = "The last folder, " + cfg.LastWorkspace + ", is no longer there."
			} else {
				a.openWorkspace(cfg.LastWorkspace)
			}
		}
	})
	mygo.App.OnWillQuit(func(*mygo.QuitEvent) {
		for _, p := range projects {
			p.app.saveConfig()
			if p.app.wsCancel != nil {
				p.app.wsCancel()
			}
			p.app.closeTerminals()
			if p.app.lsp != nil {
				p.app.lsp.stopWait(2 * time.Second)
			}
			if p.app.tsLSP != nil {
				p.app.tsLSP.stopWait(2 * time.Second)
			}
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
	return &mygo.MenuItem{Label: "See", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		updater.MenuItem(),
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
	var w, fontPopover *mygo.Window
	a.refreshSettings = func() {
		if w != nil && !w.IsDestroyed() {
			w.Update(func() {})
		}
	}
	open = func() {
		if w != nil && !w.IsDestroyed() {
			w.Focus()
			return
		}
		a.settings = settingsState{}
		w = mygo.NewWindow(mygo.WindowOptions{
			Title:                "Settings",
			Width:                900,
			Height:               680,
			MinWidth:             620,
			MinHeight:            400,
			TitleBarStyle:        mygo.TitleBarHidden,
			TitleBarHeight:       settingsBarHeight,
			TrafficLightPosition: &mygo.Point{X: 20, Y: (settingsBarHeight - trafficLightSize) / 2},
			Vibrancy:             mygo.VibrancySidebar,
			Content:              ui.View(a.settingsView),
		})
		current := w
		customPath := a.cfg.ExternalTerminalPath
		go func() {
			apps, err := mygo.Shell.InstalledApps()
			if err != nil {
				return
			}
			for _, app := range apps {
				for name, id := range externalTerminalIDs {
					if app.ID != id {
						continue
					}
					data, err := mygo.Shell.FileIcon(app.Path, 36)
					if err != nil {
						continue
					}
					current.Update(func() {
						if w != current || current.IsDestroyed() {
							return
						}
						icon, err := ui.DecodeBitmap(data)
						if err == nil {
							if a.settings.terminalIcons == nil {
								a.settings.terminalIcons = make(map[string]*ui.Bitmap)
							}
							a.settings.terminalIcons[name] = icon
						}
					})
				}
			}
		}()
		if customPath != "" {
			a.loadTerminalIcon(customPath)
		}
	}
	a.loadTerminalIcon = func(path string) {
		if w == nil || w.IsDestroyed() {
			return
		}
		current := w
		go func() {
			data, err := mygo.Shell.FileIcon(path, 36)
			current.Update(func() {
				if w != current || current.IsDestroyed() || a.cfg.ExternalTerminalPath != path {
					return
				}
				if a.settings.terminalIcons == nil {
					a.settings.terminalIcons = make(map[string]*ui.Bitmap)
				}
				delete(a.settings.terminalIcons, "Custom App")
				if err == nil {
					if icon, err := ui.DecodeBitmap(data); err == nil {
						a.settings.terminalIcons["Custom App"] = icon
					}
				}
			})
		}()
	}
	a.pickTerminalApp = func() {
		if w == nil || w.IsDestroyed() {
			return
		}
		current := w
		go func() {
			paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: current, Title: "Choose Terminal App", DefaultPath: "/Applications", Filters: []mygo.FileFilter{{Name: "Applications", Extensions: []string{"app"}}}})
			current.Update(func() {
				if w != current || current.IsDestroyed() {
					return
				}
				if err != nil {
					a.note("Could not choose terminal app: "+err.Error(), true)
					a.post(func() {})
					return
				}
				if len(paths) == 0 {
					return
				}
				a.cfg.ExternalTerminalPath = paths[0]
				a.settingsChanged()
				a.loadTerminalIcon(paths[0])
			})
		}()
	}
	a.openFontPopover = func(anchor ui.Rect, terminal bool) {
		if w == nil || w.IsDestroyed() {
			return
		}
		if fontPopover != nil && !fontPopover.IsDestroyed() {
			fontPopover.Close()
			return
		}
		a.settings.fontQuery = ""
		a.settings.fontOpen = true
		fontPopover = mygo.NewFlyout(mygo.FlyoutOptions{
			Parent: w, Anchor: mygo.Rectangle{X: int(anchor.X), Y: int(anchor.Y), Width: int(anchor.W), Height: int(anchor.H)},
			Placement: mygo.PlacementRightStart, Gap: 6, Width: 300, Height: 390,
			Focusable: true, Popover: true, Shadow: true, CornerRadius: 14,
			Content: ui.View(func(c *ui.Context) { a.fontPopover(c, terminal) }),
		})
		current := fontPopover
		current.OnClosed(func() {
			if fontPopover == current {
				fontPopover = nil
				a.settings.fontOpen = false
			}
			if w != nil && !w.IsDestroyed() {
				w.Update(func() {})
			}
		})
	}
	a.closeFontPopover = func() {
		if fontPopover != nil && !fontPopover.IsDestroyed() {
			fontPopover.Close()
		}
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
