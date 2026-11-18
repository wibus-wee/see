package app

import (
	"testing"

	"see/internal/config"

	"github.com/egoist/mygo/ui"
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

func TestEmptyFolderlessWindowDrawsWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	h.tt.Frame()
	if !h.tt.HasText("Show this window when See launches") {
		t.Error("a folderless window did not draw the welcome screen")
	}
}

func TestNewWindowShortcutFromWelcome(t *testing.T) {
	h := newHarness(t, "")
	h.a.showWelcome = true
	called := false
	h.host.openProject = func(path string) bool {
		called = path == ""
		return true
	}
	h.tt.Frame()
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyN)
	h.tt.Frame()
	if !called {
		t.Error("new window shortcut did not request a folderless window")
	}
}

func TestStandaloneWindowStaysOpenUntilEditorCloses(t *testing.T) {
	h := newHarness(t, "")
	mainWin, welcomeWin := &testWindow{}, &testWindow{}
	w := &windows{a: h.a, main: mainWin, welcome: welcomeWin}
	h.a.showWelcome = true
	w.sync(nil)
	h.a.newFile()
	w.sync(nil)
	if !mainWin.shown || welcomeWin.shown {
		t.Fatal("the draft did not replace the welcome window")
	}

	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyW)
	h.tt.Frame()
	w.sync(nil)
	if !mainWin.shown || welcomeWin.shown || !h.tt.HasText("Open File…") {
		t.Fatal("closing the last tab did not leave an empty standalone editor")
	}

	w.closeMain()
	if mainWin.shown || !welcomeWin.shown {
		t.Fatal("closing the standalone window did not return to welcome")
	}
}
