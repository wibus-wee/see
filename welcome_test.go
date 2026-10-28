package main

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func TestRecentFolders(t *testing.T) {
	c := &config{}
	for i := range maxRecent + 3 {
		c.addRecent(fmt.Sprint("/w", i))
	}
	if len(c.Recent) != maxRecent || c.Recent[0] != fmt.Sprint("/w", maxRecent+2) {
		t.Fatalf("recent %q", c.Recent)
	}
	c.addRecent("/w5") // again: moves to the front, once
	if c.Recent[0] != "/w5" || len(c.Recent) != maxRecent || slices.Index(c.Recent[1:], "/w5") >= 0 {
		t.Errorf("recent %q", c.Recent)
	}
	c.removeRecent("/w5")
	if slices.Contains(c.Recent, "/w5") {
		t.Errorf("recent %q", c.Recent)
	}
}

func TestWelcomeOpensRecent(t *testing.T) {
	a := writeTree(t, map[string]string{"a.go": "package a\n"})
	b := writeTree(t, map[string]string{"b.go": "package b\n"})
	h := newHarness(t, a)
	h.a.openWorkspace(b)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	if want := []string{b, a}; !slices.Equal(h.a.cfg.Recent, want) {
		t.Fatalf("recent %q, want %q", h.a.cfg.Recent, want)
	}

	h.tt.Key(ui.Cmd|ui.Shift, ui.Key1)
	h.tt.Frame()
	if !h.a.showWelcome {
		t.Fatal("⇧⌘1 did not ask for the welcome window")
	}
	wt := ui.NewTester(h.a.welcomeView, welcomeWidth, welcomeHeight)
	if !wt.HasText(a) || !wt.HasText(b) {
		t.Fatalf("welcome window: %q", wt.Texts())
	}
	// The open folder's row goes back to it without reopening it.
	gen := h.a.gen
	if err := wt.Click(b); err != nil {
		t.Fatal(err)
	}
	if h.a.showWelcome || h.a.gen != gen {
		t.Errorf("showWelcome %v, gen %d → %d", h.a.showWelcome, gen, h.a.gen)
	}
	// Another opens it.
	h.a.showWelcome = true
	wt.Frame()
	if err := wt.Click(a); err != nil {
		t.Fatal(err)
	}
	if h.a.root != a || h.a.showWelcome || h.a.cfg.Recent[0] != a {
		t.Errorf("root %q, showWelcome %v, recent %q", h.a.root, h.a.showWelcome, h.a.cfg.Recent)
	}
}

// fakeWindow records what windows does with it.
type fakeWindow struct {
	shown   bool
	updates int
}

func (f *fakeWindow) Show()            { f.shown = true }
func (f *fakeWindow) Hide()            { f.shown = false }
func (f *fakeWindow) Update(fn func()) { f.updates++; fn() }

func TestWindowsFollowTheFolder(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, "")
	main, welcome := &fakeWindow{}, &fakeWindow{}
	w := &windows{a: h.a, main: main, welcome: welcome}
	check := func(when string, wantMain, wantWelcome bool) {
		t.Helper()
		if main.shown != wantMain || welcome.shown != wantWelcome || w.welcomeUp.Load() != wantWelcome {
			t.Errorf("%s: main %v, welcome %v; want %v, %v", when, main.shown, welcome.shown, wantMain, wantWelcome)
		}
	}

	h.a.showWelcome = true // as at launch
	w.sync(nil)
	check("launch", false, true)

	h.a.openWorkspace(root)
	w.sync(welcome)
	check("a folder opened", true, false)
	if main.updates == 0 {
		t.Error("the main window was not asked to draw the folder")
	}

	h.a.showWelcome = true // ⇧⌘1
	w.sync(main)
	check("⇧⌘1", true, true)
	n := main.updates
	w.sync(main)
	w.sync(welcome)
	if main.updates != n {
		t.Error("the windows draw each other with nothing changed")
	}

	if w.closeWelcome(false) {
		t.Error("closing the welcome window over a folder would quit")
	}
	check("welcome closed", true, false)

	w.closeMain()
	check("main closed", false, true)
	if h.a.root != "" {
		t.Errorf("root %q after closing the main window", h.a.root)
	}

	if !w.closeWelcome(false) {
		t.Error("closing the last window without a Dock should quit")
	}
	h.a.showWelcome = true
	if w.closeWelcome(true) {
		t.Error("closing the last window on macOS should keep the app")
	}
	check("in the background", false, false)
	w.reopen()
	check("Dock icon clicked", false, true)
}

func TestWelcomeDropsMissingRecent(t *testing.T) {
	h := newHarness(t, "")
	gone := filepath.Join(t.TempDir(), "gone")
	h.a.cfg.Recent = []string{gone}
	h.tt.Frame()
	h.a.openRecent(gone)
	if len(h.a.cfg.Recent) != 0 || h.a.welcomeNote == "" {
		t.Errorf("recent %q, note %q", h.a.cfg.Recent, h.a.welcomeNote)
	}
}

func TestWelcomeReopenSetting(t *testing.T) {
	h := newHarness(t, "")
	h.tt.Frame()
	h.click("Show this window when See launches")
	if !h.a.cfg.ReopenLast {
		t.Error("unchecking did not turn reopening on")
	}
	h.click("Show this window when See launches")
	if h.a.cfg.ReopenLast {
		t.Error("checking did not turn reopening off")
	}
}

func TestWelcomeReopensLast(t *testing.T) {
	a := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, "")
	h.a.cfg.Recent = []string{a}
	h.tt.Frame()
	h.click("Reopen " + filepath.Base(a))
	if h.a.root != a {
		t.Errorf("root %q", h.a.root)
	}
}

func TestFileMenuSwitchesProjects(t *testing.T) {
	a := writeTree(t, map[string]string{"a.go": "package a\n"})
	b := writeTree(t, map[string]string{"b.go": "package b\n"})
	h := newHarness(t, a)
	rebuilt := 0
	h.a.cfg.onRecent = func() { rebuilt++ }
	h.a.openWorkspace(b)
	if rebuilt == 0 {
		t.Error("the menu was not rebuilt for a new recent folder")
	}
	run := func(fn func()) { fn() }
	item := func(m *mygo.MenuItem, label string) *mygo.MenuItem {
		for _, it := range m.Submenu {
			if it.Label == label {
				return it
			}
		}
		t.Fatalf("no %q in %q", label, m.Label)
		return nil
	}
	recent := item(h.a.fileMenu(run), "Open Recent")
	item(recent, filepath.Base(a)).Click(nil, nil)
	if h.a.root != a {
		t.Fatalf("root %q, want %q", h.a.root, a)
	}
	item(h.a.fileMenu(run), "Close Folder").Click(nil, nil)
	h.tt.Frame()
	if h.a.root != "" || !h.tt.HasText("Open Folder…") {
		t.Errorf("root %q after Close Folder", h.a.root)
	}
	item(item(h.a.fileMenu(run), "Open Recent"), "Clear Menu").Click(nil, nil)
	if len(h.a.cfg.Recent) != 0 {
		t.Errorf("recent %q after Clear Menu", h.a.cfg.Recent)
	}
}
