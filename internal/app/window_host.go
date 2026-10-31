package app

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// windowHost is the desktop around a project window: the window, its
// dialogs and menus, the system's trash and clipboard, and through main
// the app's other windows.
type windowHost struct {
	app *App
	win *mygo.Window
	// own is set for a project window of its own, which takes the
	// welcome window's size while it has no folder. The first window
	// leaves that to the welcome window of its own, which wins tells
	// when it shows over it.
	own          bool
	welcomeSized bool
	wins         *windows
	welcome      *mygo.Window
	// createProject opens a project window; setMenu builds the menu bar.
	createProject func(path string)
	setMenu       func()
	*settingsWindow
}

func newWindowHost(a *App, win *mygo.Window) *windowHost {
	return &windowHost{app: a, win: win, settingsWindow: &settingsWindow{a: a}}
}

func (h *windowHost) setTitle(title string) {
	h.win.SetTitle(title)
	if !h.own {
		return
	}
	if want := h.app.root == ""; want != h.welcomeSized {
		h.welcomeSized = want
		if want {
			h.win.SetSize(welcomeWidth, welcomeHeight)
			h.win.SetResizable(false)
			h.win.SetMinimizable(false)
			h.win.SetMaximizable(false)
		}
		h.win.Center()
	}
}

func (h *windowHost) setWorkspaceSize(width, height int) {
	if h.own {
		h.win.SetResizable(true)
		h.win.SetMinimizable(true)
		h.win.SetMaximizable(true)
	}
	h.win.SetSize(width, height)
	h.win.Center()
}

// popupMenu opens a native menu: PopupAt waits for the menu to close, so
// it runs off the UI thread, and its choices come back through post.
func (h *windowHost) popupMenu(x, y int, items []*mygo.MenuItem) {
	go mygo.NewMenu(items).PopupAt(h.win, x, y)
}

// dialogParent is the window in front, which dialogs go over.
func (h *windowHost) dialogParent() *mygo.Window {
	if h.wins != nil && h.welcome != nil && h.wins.welcomeUp.Load() {
		return h.welcome
	}
	return h.win
}

func (h *windowHost) pickFolder() (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: h.dialogParent(), Title: "Open Folder", Directory: true})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
}

func (h *windowHost) pickNewFile(root string) (string, error) {
	return mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: h.win, Title: "New File", DefaultPath: root, ButtonLabel: "Create"})
}

func (h *windowHost) confirmDirty(message string) (int, error) {
	result, err := mygo.Dialog.Message(mygo.MessageOptions{
		Parent: h.win, Type: mygo.MessageWarning, Title: "Unsaved Changes",
		Message: message, Detail: "Do you want to save your changes?",
		Buttons: []string{"Save", "Discard Changes", "Cancel"}, CancelButton: 2,
	})
	return result.Button, err
}

func (h *windowHost) trashItem(path string) error     { return mygo.Shell.TrashItem(path) }
func (h *windowHost) copyFiles(paths ...string) error { return mygo.Clipboard.WriteFiles(paths...) }
func (h *windowHost) copyImage(png []byte) error      { return mygo.Clipboard.WriteImage(png) }
func (h *windowHost) setAppearance(appearance string) { mygo.Theme.SetSource(themeSource(appearance)) }

func (h *windowHost) openProjectWindow(path string) bool {
	if h.createProject == nil {
		return false
	}
	h.createProject(path)
	return true
}

func (h *windowHost) refreshMenu() {
	if h.setMenu != nil {
		h.setMenu()
	}
}

// settingsWindow is an app's settings window, one at a time, its sidebar
// over the desktop's material, as macOS's System Settings, and the font
// popover over it.
type settingsWindow struct {
	a              *App
	w, fontPopover *mygo.Window
}

func (s *settingsWindow) shown() bool { return s.w != nil && !s.w.IsDestroyed() }

func (s *settingsWindow) refreshSettings() {
	if s.shown() {
		s.w.Update(func() {})
	}
}

func (s *settingsWindow) openSettings() {
	if s.shown() {
		s.w.Focus()
		return
	}
	a := s.a
	a.settings = settingsState{}
	s.w = mygo.NewWindow(mygo.WindowOptions{
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
	current := s.w
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
					if s.w != current || current.IsDestroyed() {
						return
					}
					if icon, err := ui.DecodeBitmap(data); err == nil {
						if a.settings.terminalIcons == nil {
							a.settings.terminalIcons = make(map[string]*ui.Bitmap)
						}
						a.settings.terminalIcons[name] = icon
					}
				})
			}
		}
	}()
	if p := a.cfg.ExternalTerminalPath; p != "" {
		s.loadTerminalIcon(p)
	}
}

func (s *settingsWindow) closeSettings() {
	if s.shown() {
		s.w.Close()
	}
}

func (s *settingsWindow) loadTerminalIcon(path string) {
	if !s.shown() {
		return
	}
	a, current := s.a, s.w
	go func() {
		data, err := mygo.Shell.FileIcon(path, 36)
		current.Update(func() {
			if s.w != current || current.IsDestroyed() || a.cfg.ExternalTerminalPath != path {
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

func (s *settingsWindow) pickTerminalApp() {
	if !s.shown() {
		return
	}
	a, current := s.a, s.w
	go func() {
		paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{Parent: current, Title: "Choose Terminal App", DefaultPath: "/Applications", Filters: []mygo.FileFilter{{Name: "Applications", Extensions: []string{"app"}}}})
		current.Update(func() {
			if s.w != current || current.IsDestroyed() {
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
			s.loadTerminalIcon(paths[0])
		})
	}()
}

func (s *settingsWindow) openFontPopover(anchor ui.Rect, terminal bool) {
	if !s.shown() {
		return
	}
	if s.fontPopover != nil && !s.fontPopover.IsDestroyed() {
		s.fontPopover.Close()
		return
	}
	a := s.a
	a.settings.fontQuery = ""
	a.settings.fontOpen = true
	s.fontPopover = mygo.NewFlyout(mygo.FlyoutOptions{
		Parent: s.w, Anchor: mygo.Rectangle{X: int(anchor.X), Y: int(anchor.Y), Width: int(anchor.W), Height: int(anchor.H)},
		Placement: mygo.PlacementRightStart, Gap: 6, Width: 300, Height: 390,
		Focusable: true, Popover: true, Shadow: true, CornerRadius: 14,
		Content: ui.View(func(c *ui.Context) { a.fontPopover(c, terminal) }),
	})
	current := s.fontPopover
	current.OnClosed(func() {
		if s.fontPopover == current {
			s.fontPopover = nil
			a.settings.fontOpen = false
		}
		s.refreshSettings()
	})
}

func (s *settingsWindow) closeFontPopover() {
	if s.fontPopover != nil && !s.fontPopover.IsDestroyed() {
		s.fontPopover.Close()
	}
}
