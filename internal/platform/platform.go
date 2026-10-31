// Package platform is what the app asks of the desktop it runs on: the
// window, its dialogs and menus, the system's trash and clipboard, the
// app's other windows, and the UI thread itself. It names the seam
// between the app and MyGo, so that nothing else has to know about a
// window.
//
// The dialogs block: call them off the UI thread.
package platform

import (
	"errors"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// Scheduler runs work on the UI thread, where the app's state lives:
// background work hands its results back through it.
type Scheduler interface {
	Post(func())
}

// SchedulerFunc adapts a function to a Scheduler.
type SchedulerFunc func(func())

// Post runs fn on the UI thread.
func (f SchedulerFunc) Post(fn func()) { f(fn) }

// Desktop is the desktop around one of the app's windows. main gives each
// project window's app a windowHost; tests give their own.
//
// One interface, because one type implements it and the app uses all of
// it; a consumer that needs a part of it—the menus, say—can take a
// smaller interface of its own and the same value will satisfy it.
type Desktop interface {
	SetTitle(title string)
	SetWorkspaceSize(width, height int)
	PopupMenu(x, y int, items []*mygo.MenuItem)

	PickFolder() (string, error)             // "" when canceled
	PickNewFile(root string) (string, error) // "" when canceled
	// ConfirmDirty asks what to do with unsaved changes: the index of
	// Save, Don't Save or Cancel. ErrNoDialog has the app ask in the window.
	ConfirmDirty(message string) (int, error)

	TrashItem(path string) error
	CopyFiles(paths ...string) error
	CopyImage(png []byte) error

	// OpenProjectWindow opens path in a window of its own, "" an empty one,
	// and reports whether it could.
	OpenProjectWindow(path string) bool
	RefreshMenu() // builds the menu bar again
	SetAppearance(appearance string)

	OpenSettings()
	CloseSettings()
	RefreshSettings() // draws the settings window again
	OpenFontPopover(anchor ui.Rect, terminal bool)
	CloseFontPopover()
	PickTerminalApp()
	LoadTerminalIcon(path string)
}

// ErrNoDialog is a desktop's answer where it has no native dialog.
var ErrNoDialog = errors.New("no dialog")

// NoDesktop is a desktop without a window, as an app has until main gives
// it one: it shows nothing and asks nothing, so confirmations ask in the
// window and dialogs are canceled.
type NoDesktop struct{}

func (NoDesktop) SetTitle(string)                      {}
func (NoDesktop) SetWorkspaceSize(int, int)            {}
func (NoDesktop) PopupMenu(int, int, []*mygo.MenuItem) {}
func (NoDesktop) PickFolder() (string, error)          { return "", nil }
func (NoDesktop) PickNewFile(string) (string, error)   { return "", nil }
func (NoDesktop) ConfirmDirty(string) (int, error)     { return 0, ErrNoDialog }
func (NoDesktop) TrashItem(string) error               { return ErrNoDialog }
func (NoDesktop) CopyFiles(...string) error            { return ErrNoDialog }
func (NoDesktop) CopyImage([]byte) error               { return ErrNoDialog }
func (NoDesktop) OpenProjectWindow(string) bool        { return false }
func (NoDesktop) RefreshMenu()                         {}
func (NoDesktop) SetAppearance(string)                 {}
func (NoDesktop) OpenSettings()                        {}
func (NoDesktop) CloseSettings()                       {}
func (NoDesktop) RefreshSettings()                     {}
func (NoDesktop) OpenFontPopover(ui.Rect, bool)        {}
func (NoDesktop) CloseFontPopover()                    {}
func (NoDesktop) PickTerminalApp()                     {}
func (NoDesktop) LoadTerminalIcon(string)              {}
