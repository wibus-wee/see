package main

import (
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestSettingsPanes(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	d := h.open("a.go")
	st := ui.NewTester(h.a.settingsView, 760, 560)
	for _, s := range []string{"Search Settings", "General", "Editor", "Code Intelligence", "Search", "Shortcuts", "Appearance"} {
		if !st.HasText(s) {
			t.Fatalf("missing %q in %q", s, st.Texts())
		}
	}
	// Editor settings reach the editors already open, and are kept.
	if err := st.Click("Editor"); err != nil {
		t.Fatal(err)
	}
	// The switch is at the right end of its row, in the middle of the
	// row's title and the detail under it, however the detail wraps.
	title, ok1 := st.Find("Wrap Lines")
	r, ok := st.Find("Long lines wrap at the editor's width rather than scrolling sideways.")
	if !ok || !ok1 {
		t.Fatalf("no Wrap Lines row: %q", st.Texts())
	}
	st.ClickAt(760-20-14-18, (title.Y+r.Y+r.H)/2)
	if err := st.Click("Relative"); err != nil {
		t.Fatal(err)
	}
	ed := h.ed(d)
	if !h.a.cfg.SoftWrap || !ed.SoftWrap || h.a.cfg.LineNumbers != 1 || ed.LineNumbers != editor.LineNumbersRelative {
		t.Errorf("wrap %v/%v, numbers %d/%v", h.a.cfg.SoftWrap, ed.SoftWrap, h.a.cfg.LineNumbers, ed.LineNumbers)
	}
	// Back returns to General.
	if err := st.Click("Back"); err != nil {
		t.Fatal(err)
	}
	if h.a.settings.pane != "general" {
		t.Errorf("pane %q", h.a.settings.pane)
	}
	// Finding settings narrows the panes.
	st.Click("Search Settings")
	st.Type("gopls")
	st.Frame()
	if !st.HasText("Code Intelligence") || st.HasText("Shortcuts") {
		t.Errorf("filtered: %q", st.Texts())
	}
	// Trusted folders can be taken back.
	h.a.cfg.trust(h.a.root)
	h.a.settingsGo("intelligence")
	st.Frame()
	if err := st.Click("Remove trusted folder " + h.a.root); err != nil {
		t.Fatal(err)
	}
	if h.a.cfg.trusted(h.a.root) {
		t.Error("still trusted")
	}
}

func TestSettingsScreenshot(t *testing.T) {
	dir := os.Getenv("SEE_SHOT")
	if dir == "" {
		t.Skip("set SEE_SHOT to a directory")
	}
	h := newHarness(t, writeTree(t, map[string]string{"a.go": ""}))
	h.a.cfg.trust(h.a.root)
	st := ui.NewTester(h.a.settingsView, 760, 560)
	shot := func(name string) {
		st.Frame()
		f, _ := os.Create(filepath.Join(dir, name))
		defer f.Close()
		png.Encode(f, st.Image())
	}
	st.SetDark(false)
	shot("settings-light.png")
	h.a.settingsGo("intelligence")
	shot("settings-intel-light.png")
	st.SetDark(true)
	h.a.settingsGo("editor")
	shot("settings-dark.png")
}

// Up and Down go from pane to pane in the sidebar, among those the search
// leaves, as in System Settings.
func TestSettingsSidebarKeys(t *testing.T) {
	h := newHarness(t, "")
	h.a.settings = settingsState{pane: "general"}
	h.a.settingsStep("", false)
	if h.a.settings.pane != "editor" {
		t.Errorf("Down: pane %q, want editor", h.a.settings.pane)
	}
	h.a.settingsStep("", true)
	h.a.settingsStep("", true)
	if h.a.settings.pane != "about" {
		t.Errorf("Up twice: pane %q, want about", h.a.settings.pane)
	}
	h.a.settingsStep("", true) // stays at the top
	if h.a.settings.pane != "about" {
		t.Errorf("Up at the top: pane %q", h.a.settings.pane)
	}
	h.a.settingsStep("gopls", false) // only Code Intelligence matches
	if h.a.settings.pane != "intelligence" {
		t.Errorf("Down while searching: pane %q, want intelligence", h.a.settings.pane)
	}
}

func TestFontPickerUpdatesEditor(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	d := h.open("a.go")
	h.a.settings.pane = "editor"
	h.a.settings.fontFamilies = []ui.FontFamily{{Name: "Test Mono", Monospaced: true}, {Name: "Test Sans"}}
	var anchor ui.Rect
	h.host.fontPopover = func(r ui.Rect, terminal bool) { anchor = r; h.a.settings.fontOpen = true }
	st := ui.NewTester(h.a.settingsView, 760, 560)
	if err := st.Click("Choose Font"); err != nil {
		t.Fatal(err)
	}
	if anchor.W <= 0 || anchor.H <= 0 {
		t.Fatalf("invalid anchor: %+v", anchor)
	}
	st = ui.NewTester(func(c *ui.Context) { h.a.fontPopover(c, false) }, 300, 390)
	if !st.HasText("Test Mono") || st.HasText("Test Sans") {
		t.Fatalf("font filter: %v", st.Texts())
	}
	if err := st.Click("Test Mono"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if h.a.cfg.FontFamily != "Test Mono" || h.ed(d).Font.Family != "Test Mono" || h.a.settings.fontOpen {
		t.Fatalf("font selection was not applied: %+v", h.a.cfg)
	}
}

func TestTerminalFontPreferences(t *testing.T) {
	a := &app{cfg: &config{TermFontFamily: "Example", TermFontWeight: 500, TermLineHeight: 20, TermThicken: true, TermNoLigatures: true}}
	f := a.terminalFont()
	if f.Family != "Example" || f.Weight != 500 || f.LineHeight != 1.2 || !f.Thicken || len(f.Features) != 2 {
		t.Fatalf("terminal font: %+v", f)
	}
}
