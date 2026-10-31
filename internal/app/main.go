// See is a desktop source code reader: a file tree,
// tabs, a code editor and Go to Definition through language servers, drawn
// natively by MyGo's ui, with no web view.
package app

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"see/internal/config"
	"slices"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/updater"
	"github.com/egoist/mygo/plugins/updater/native"
	"github.com/egoist/mygo/ui"

	"see/internal/shellpath"
	"see/internal/workspace"
)

func Main() {
	if isCLIInvocation(os.Args[0]) && (len(os.Args) < 2 || os.Args[1] != cliOpenArg) {
		if err := runCLI(os.Args[1:], os.Stderr); err != nil {
			fmt.Fprintln(os.Stderr, "see:", err)
			os.Exit(1)
		}
		return
	}
	shellpath.Start()
	mygo.Use(native.Plugin)
	cfg := config.Load(config.Path())
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
	mygo.Theme.SetSource(themeSource(cfg.Appearance))
	mygo.App.SetName("See")
	mygo.App.WhenReady(func() {
		a.version = mygo.App.Version()
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "See",
			Width:     config.DefaultWindowWidth,
			Height:    config.DefaultWindowHeight,
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
				if root, err := workspace.CanonicalRoot(path); err == nil {
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
			width, height := config.DefaultWindowWidth, config.DefaultWindowHeight
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
			host := newWindowHost(child, childWin)
			host.own, host.welcomeSized = true, path == ""
			host.createProject = createProject
			host.setMenu = func() {
				if varSetMenu != nil {
					varSetMenu()
				}
			}
			child.host = host
			childWin.OnResize(func() {
				childWin.Update(func() {
					if !childWin.IsMaximized() && !childWin.IsFullScreen() {
						width, height := childWin.Size()
						child.resizedWorkspace(width, height)
					}
				})
			})
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
						child.host.closeSettings()
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
		host := newWindowHost(a, win)
		host.wins, host.welcome = wins, welcome
		host.createProject = createProject
		a.host = host

		a.post = func(fn func()) { win.Update(func() { fn(); wins.sync(nil) }) }
		if ready := shellpath.Ready(); ready != nil {
			go func() {
				<-ready
				win.Update(func() {
					for _, project := range projects {
						project.app.host.refreshSettings()
					}
				})
			}()
		}
		win.OnResize(func() {
			win.Update(func() {
				if !win.IsMaximized() && !win.IsFullScreen() {
					width, height := win.Size()
					a.resizedWorkspace(width, height)
				}
			})
		})
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
		cfg.SetRecentHook(setMenu)
		host.setMenu = setMenu
		setMenu()
		// A folder on the command line, else the welcome window, or the
		// last folder when the user asked for it at launch.
		a.showWelcome = true // until a folder opens
		defer wins.sync(nil)
		switch {
		case len(os.Args) > 2 && os.Args[1] == cliOpenArg:
			a.openCLIPath(os.Args[2])
		case len(os.Args) > 1:
			a.openCLIPath(os.Args[1])
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
				p.app.lsp.StopWait(2 * time.Second)
			}
			if p.app.tsLSP != nil {
				p.app.tsLSP.StopWait(2 * time.Second)
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
func appearanceMenu(a *App) []*mygo.MenuItem {
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
func appMenu(a *App) *mygo.MenuItem {
	return &mygo.MenuItem{Label: "See", Submenu: []*mygo.MenuItem{
		{Role: mygo.RoleAbout},
		updater.MenuItem(),
		mygo.Separator(),
		{Label: "Settings…", Accelerator: "CmdOrCtrl+,", Click: func(*mygo.MenuItem, *mygo.Window) { a.host.openSettings() }},
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

// trafficLightSize is the height of macOS's window buttons, which the
// title bars center them by.
const trafficLightSize = 14

// settingsBarHeight is the settings window's title bar, as tall as
// System Settings'.
const settingsBarHeight = 52
