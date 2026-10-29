package main

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// host is what an app asks of the desktop around its window: the window
// itself, its dialogs and menus, the system's trash and clipboard, and the
// app's other windows. main gives each project window's app a windowHost;
// tests give their own.
//
// The dialogs block: call them off the UI thread.
type host interface {
	setTitle(title string)
	setWorkspaceSize(width, height int)
	popupMenu(x, y int, items []*mygo.MenuItem)

	pickFolder() (string, error)             // "" when canceled
	pickNewFile(root string) (string, error) // "" when canceled
	// confirmDirty asks what to do with unsaved changes: the index of
	// Save, Don't Save or Cancel. errNoDialog has the app ask in the window.
	confirmDirty(message string) (int, error)

	trashItem(path string) error
	copyFiles(paths ...string) error
	copyImage(png []byte) error

	// openProjectWindow opens path in a window of its own, "" an empty
	// one, and reports whether it could.
	openProjectWindow(path string) bool
	refreshMenu() // builds the menu bar again
	setAppearance(appearance string)

	openSettings()
	closeSettings()
	refreshSettings() // draws the settings window again
	openFontPopover(anchor ui.Rect, terminal bool)
	closeFontPopover()
	pickTerminalApp()
	loadTerminalIcon(path string)
}

// errNoDialog is a host's answer where it has no native dialog.
var errNoDialog = errors.New("no dialog")

// noHost is a host without a desktop, as an app has until main gives it
// its window: it shows nothing and asks nothing, so confirmations ask in
// the window and dialogs are canceled.
type noHost struct{}

func (noHost) setTitle(string)                      {}
func (noHost) setWorkspaceSize(int, int)            {}
func (noHost) popupMenu(int, int, []*mygo.MenuItem) {}
func (noHost) pickFolder() (string, error)          { return "", nil }
func (noHost) pickNewFile(string) (string, error)   { return "", nil }
func (noHost) confirmDirty(string) (int, error)     { return 0, errNoDialog }
func (noHost) trashItem(string) error               { return errNoDialog }
func (noHost) copyFiles(...string) error            { return errNoDialog }
func (noHost) copyImage([]byte) error               { return errNoDialog }
func (noHost) openProjectWindow(string) bool        { return false }
func (noHost) refreshMenu()                         {}
func (noHost) setAppearance(string)                 {}
func (noHost) openSettings()                        {}
func (noHost) closeSettings()                       {}
func (noHost) refreshSettings()                     {}
func (noHost) openFontPopover(ui.Rect, bool)        {}
func (noHost) closeFontPopover()                    {}
func (noHost) pickTerminalApp()                     {}
func (noHost) loadTerminalIcon(string)              {}
