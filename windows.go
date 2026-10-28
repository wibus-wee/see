package main

import (
	"fmt"
	"sync/atomic"

	"github.com/egoist/mygo/ui"
)

// The app's two windows, as Xcode's: the main window while a folder is
// open, and the welcome window, which shows at launch, over the main
// window when asked for (⇧⌘1), and again once the folder closes. Closing
// the welcome window leaves the app running in the background.

// window is what windows does with a window: *mygo.Window, or a fake in
// tests.
type window interface {
	Show()
	Hide()
	Update(fn func())
}

type windows struct {
	a                       *app
	main, welcome           window
	mainShown, welcomeShown bool
	seen                    string      // the state both windows last drew
	welcomeUp               atomic.Bool // the welcome window shows, for other goroutines
}

// sync shows and hides the windows as the app's state says: run it after
// whatever may change the state, as each window's frame and the results
// of background work. from is the window that drew just now, or nil.
func (w *windows) sync(from window) {
	a := w.a
	wantMain := a.root != ""
	wantWelcome := a.showWelcome
	// Show before hiding, so that the app always has a window up while it
	// switches from one to the other.
	if wantWelcome && !w.welcomeShown {
		w.welcome.Show()
	}
	if wantMain && !w.mainShown {
		w.main.Show()
	}
	if !wantMain && w.mainShown {
		w.main.Hide()
	}
	if !wantWelcome && w.welcomeShown {
		w.welcome.Hide()
	}
	w.mainShown, w.welcomeShown = wantMain, wantWelcome
	w.welcomeUp.Store(wantWelcome)

	// The other window draws what changed here, as a folder opened from
	// the welcome window, or the recent folders after one opens; only when
	// it changed, or the two would draw each other forever.
	if s := w.state(); s != w.seen {
		w.seen = s
		if from != w.main && w.mainShown {
			w.main.Update(func() {})
		}
		if from != w.welcome && w.welcomeShown {
			w.welcome.Update(func() {})
		}
	}
}

// state is what of the app the windows show of each other's doing.
func (w *windows) state() string {
	a := w.a
	return fmt.Sprint(a.gen, a.root, a.showWelcome, a.welcomeNote, a.cfg.ReopenLast, a.cfg.Recent)
}

// closeWelcome is the welcome window's close button: it hides, and the app
// stays, as Xcode does. It reports whether the app should quit instead,
// where there is no Dock to bring it back from.
func (w *windows) closeWelcome(keepRunning bool) (quit bool) {
	w.a.showWelcome = false
	w.sync(nil)
	return w.a.root == "" && !keepRunning
}

// closeMain is the main window's close button: it closes the folder and
// goes back to the welcome window.
func (w *windows) closeMain() {
	w.a.closeFolder()
	w.sync(nil)
}

// reopen brings a window back when the app is activated with none showing,
// as by a click on its Dock icon.
func (w *windows) reopen() {
	if w.a.root == "" {
		w.a.showWelcome = true
	} else {
		w.mainShown = false // shown again
	}
	w.sync(nil)
}

// welcomeView is the welcome window's content.
func (a *app) welcomeView(c *ui.Context) {
	a.applyAppearance(c)
	c.SetTheme(a.theme)
	switch {
	case c.Shortcut(ui.Cmd|ui.Shift, ui.KeyN) && a.newProjectWindow != nil:
		a.newProjectWindow()
	case c.Shortcut(ui.Cmd, ui.KeyO):
		a.openFolderDialog()
	case c.Shortcut(ui.Cmd, ui.KeyComma) && a.openSettings != nil:
		a.openSettings()
	case a.root != "" && c.Shortcut(0, ui.KeyEscape):
		a.showWelcome = false
	}
	a.welcomeWindow(c)
}
