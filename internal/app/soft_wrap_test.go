package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
)

func TestEditorAndDiffSoftWrapUseDocumentLanguage(t *testing.T) {
	a := &App{cfg: &config.Config{
		SoftWrap: true,
		SoftWrapByLanguage: map[string]bool{
			"Markdown": false,
			"Go":       true,
			"Python":   false,
		},
	}}

	buffers := []struct {
		name string
		want bool
	}{
		{name: "README.md", want: false},
		{name: "main.go", want: true},
		{name: "script.py", want: false},
		{name: "notes.unknown", want: true},
	}
	for _, tt := range buffers {
		buf := editor.NewBuffer("long line")
		if language := languageFor(tt.name, ""); language != nil {
			buf.SetLanguage(language)
		}
		st := &editor.State{Buffer: buf}
		a.applyEditorPrefs(st)
		if st.SoftWrap != tt.want {
			t.Errorf("editor %s SoftWrap = %v, want %v", tt.name, st.SoftWrap, tt.want)
		}
		diff := &editor.DiffState{New: buf}
		a.applyDiffPrefs(diff)
		if diff.Wrap != tt.want {
			t.Errorf("diff %s Wrap = %v, want %v", tt.name, diff.Wrap, tt.want)
		}
	}
}

func TestLanguageSoftWrapSettingsUpdateOpenEditors(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"README.md", "main.go"} {
		if err := os.WriteFile(filepath.Join(root, name), []byte("long line"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	h := newHarness(t, root)
	markdown := h.open("README.md")
	goFile := h.open("main.go")
	markdownState := h.a.focus.eds[markdown.path]
	goState := h.a.focus.eds[goFile.path]
	if markdownState == nil || goState == nil {
		t.Fatal("expected both opened files to have editor states")
	}
	h.a.settings.pane = "editor"
	settings := ui.NewTester(h.a.settingsView, 900, 680)
	click := func(label string) {
		t.Helper()
		if err := settings.Click(label); err != nil {
			t.Fatalf("click %q: %v", label, err)
		}
		settings.Frame()
		h.tt.Frame()
	}
	openLanguageMenu := func() {
		t.Helper()
		control, ok := settings.AccessByRole(ui.RolePopUpButton, "Language")
		if !ok {
			t.Fatal("language selector not found")
		}
		settings.ClickAt(control.Bounds.X+control.Bounds.W/2, control.Bounds.Y+control.Bounds.H/2)
		settings.Frame()
		h.tt.Frame()
		if !settings.HasText("Go") {
			t.Fatal("language selector did not open its options")
		}
	}
	toggleGlobalWrap := func() {
		t.Helper()
		control, ok := settings.AccessByRole(ui.RoleSwitch, "Wrap Lines")
		if !ok {
			t.Fatal("global Wrap Lines switch not found")
		}
		settings.ClickAt(control.Bounds.X+control.Bounds.W/2, control.Bounds.Y+control.Bounds.H/2)
		settings.Frame()
		h.tt.Frame()
	}
	assertWraps := func(markdownWrap, goWrap bool) {
		t.Helper()
		if markdownState.SoftWrap != markdownWrap || goState.SoftWrap != goWrap {
			t.Fatalf("open editor wrapping: Markdown=%v, Go=%v; want Markdown=%v, Go=%v",
				markdownState.SoftWrap, goState.SoftWrap, markdownWrap, goWrap)
		}
	}
	assertWraps(false, false)

	click("Wrap")
	if got, ok := h.a.cfg.SoftWrapByLanguage["Markdown"]; !ok || !got {
		t.Fatalf("Markdown Wrap override = %v, present %v; want true, present", got, ok)
	}
	assertWraps(true, false)

	toggleGlobalWrap()
	if !h.a.cfg.SoftWrap {
		t.Fatal("global Wrap Lines setting did not turn on")
	}
	assertWraps(true, true)

	openLanguageMenu()
	click("Go")
	if h.a.settings.softWrapLanguage != "Go" {
		t.Fatalf("selected language = %q, want Go", h.a.settings.softWrapLanguage)
	}
	click("Don't Wrap")
	if got, ok := h.a.cfg.SoftWrapByLanguage["Go"]; !ok || got {
		t.Fatalf("Go Don't Wrap override = %v, present %v; want false, present", got, ok)
	}
	assertWraps(true, false)

	click("Default")
	if _, ok := h.a.cfg.SoftWrapByLanguage["Go"]; ok {
		t.Fatal("Default did not remove the Go override")
	}
	assertWraps(true, true)

	toggleGlobalWrap()
	if h.a.cfg.SoftWrap {
		t.Fatal("global Wrap Lines setting did not turn off")
	}
	assertWraps(true, false)
}
