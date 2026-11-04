package app

import (
	"testing"

	"see/internal/config"
)

// testWindow records what windows does with it.
type testWindow struct {
	shown   bool
	updates int
}

func (w *testWindow) Show()         { w.shown = true }
func (w *testWindow) Hide()         { w.shown = false }
func (w *testWindow) Update(func()) { w.updates++ }

func newTestWindows(t *testing.T) (*App, *windows, *testWindow, *testWindow) {
	t.Helper()
	a := newApp(&config.Config{})
	mainWin, welcomeWin := &testWindow{}, &testWindow{}
	w := &windows{a: a, main: mainWin, welcome: welcomeWin}
	return a, w, mainWin, welcomeWin
}

// Closing the welcome window without a folder leaves the app with no
// window up, as Xcode does, rather than showing the empty main window.
func TestClosingWelcomeWithoutFolderShowsNoWindow(t *testing.T) {
	a, w, mainWin, welcomeWin := newTestWindows(t)
	a.showWelcome = true
	w.sync(nil)
	if !welcomeWin.shown || mainWin.shown {
		t.Fatalf("at launch: welcome shown %v, main shown %v", welcomeWin.shown, mainWin.shown)
	}

	w.closeWelcome(true)
	if welcomeWin.shown {
		t.Error("closing the welcome window left it up")
	}
	if mainWin.shown {
		t.Error("closing the welcome window showed the empty main window")
	}
}

// A folderless window of its own, as New Window opens, shows the empty
// editor rather than a second welcome screen.
func TestEmptyFolderlessWindowDrawsNoWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = false
	h.a.hasWelcome = false
	h.tt.Frame()
	if h.tt.HasText("Show this window when See launches") {
		t.Error("an empty window of its own drew the welcome screen")
	}
}

// A folderless window with a file open is a real window, and stays up.
func TestFolderlessDocumentShowsMainWindow(t *testing.T) {
	a, w, mainWin, welcomeWin := newTestWindows(t)
	a.showWelcome = false
	a.docs["/tmp/note.txt"] = &doc{path: "/tmp/note.txt"}

	w.sync(nil)
	if !mainWin.shown {
		t.Error("a folderless window with a file open stayed hidden")
	}
	if welcomeWin.shown {
		t.Error("the welcome window stayed up over the file")
	}
}
