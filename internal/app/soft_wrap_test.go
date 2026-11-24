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

func TestWorkingCopyMarkdownDiffUsesLanguageSoftWrap(t *testing.T) {
	root := gitRepo(t, map[string]string{"wrap-demo.md": "# Wrap demo\n\nbefore\n"})
	writeFile(t, root, "wrap-demo.md", "# Wrap demo\n\nafter\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.cfg.SoftWrap = false
	h.a.cfg.SoftWrapByLanguage = map[string]bool{"Markdown": true}
	if len(h.a.git.changes) != 1 || h.a.git.changes[0].Path != "wrap-demo.md" {
		t.Fatalf("working copy changes = %+v", h.a.git.changes)
	}
	h.a.showGitDiff(h.a.git.changes[0], false)
	key := diffTab("wrap-demo.md", false)
	h.settle(func() bool {
		v := h.a.focus.diffs[key]
		return v != nil && v.state != nil
	})
	v := h.a.focus.diffs[key]
	if language := v.state.New.Language(); language == nil || language.Name != "Markdown" {
		t.Fatalf("diff new-buffer language = %v, want Markdown", language)
	}
	if !v.state.Wrap {
		t.Fatal("Working Copy Markdown diff did not use the enabled language override")
	}
}

func TestSavingUntitledDocumentRefreshesLanguageSoftWrap(t *testing.T) {
	h := newHarness(t, "")
	h.a.cfg.SoftWrap = false
	h.a.cfg.SoftWrapByLanguage = map[string]bool{"Markdown": true}
	h.a.newFile()
	draft := h.a.docs[h.a.activePath()]
	state := h.a.activeEditor()
	if draft == nil || state == nil || state.SoftWrap {
		t.Fatalf("untitled draft/state = %v/%v, initial wrapping = %v", draft, state != nil, state != nil && state.SoftWrap)
	}
	draft.buf.Set("long line")
	destination := filepath.Join(t.TempDir(), "README.md")
	h.host.newFile = func(string, string) (string, error) { return destination, nil }
	h.a.saveActive()
	h.settle(func() bool { return !draft.saving && !draft.untitled })
	if language := draft.buf.Language(); language == nil || language.Name != "Markdown" {
		t.Fatalf("saved document language = %v, want Markdown", language)
	}
	if h.a.focus.eds[draft.path] != state {
		t.Fatal("saving untitled document replaced its editor state")
	}
	if !state.SoftWrap {
		t.Fatal("saved Markdown editor did not use its enabled language override")
	}
}

func TestWorkingCopyCMakeDiffUsesLanguageSoftWrap(t *testing.T) {
	root := gitRepo(t, map[string]string{"CMakeLists.txt": "set(VALUE old)\n"})
	writeFile(t, root, "CMakeLists.txt", "set(VALUE new)\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.cfg.SoftWrap = false
	h.a.cfg.SoftWrapByLanguage = map[string]bool{"CMake": true}
	if len(h.a.git.changes) != 1 || h.a.git.changes[0].Path != "CMakeLists.txt" {
		t.Fatalf("working copy changes = %+v", h.a.git.changes)
	}
	h.a.showGitDiff(h.a.git.changes[0], false)
	key := diffTab("CMakeLists.txt", false)
	h.settle(func() bool {
		v := h.a.focus.diffs[key]
		return v != nil && v.state != nil
	})
	v := h.a.focus.diffs[key]
	if language := v.state.New.Language(); language == nil || language.Name != "CMake" {
		t.Fatalf("diff new-buffer language = %v, want CMake", language)
	}
	if !v.state.Wrap {
		t.Fatal("Working Copy CMake diff did not use the enabled language override")
	}
}

func TestGitPeekSoftWrapUpdatesForOpenPreview(t *testing.T) {
	for _, hideWhitespace := range []bool{false, true} {
		hideWhitespace := hideWhitespace
		name := map[bool]string{false: "visible-whitespace", true: "hidden-whitespace"}[hideWhitespace]
		t.Run(name, func(t *testing.T) {
			root := gitRepo(t, map[string]string{"README.md": "old line\n"})
			writeFile(t, root, "README.md", "new line\n")
			h := newHarness(t, root)
			h.readGit()
			h.a.cfg.HideWhitespaceChanges = hideWhitespace
			h.a.cfg.SoftWrap = false
			h.a.cfg.SoftWrapByLanguage = map[string]bool{"Markdown": false}
			d := h.open("README.md")
			h.settle(func() bool {
				state := h.a.focus.eds[d.path]
				return d.gitChanges != nil && state != nil && state.DiffBase != nil
			})
			state := h.a.focus.eds[d.path]
			h.a.showGitPeek(h.a.focus, d, state, 0)
			peek := h.a.git.peek
			if peek == nil {
				t.Fatal("Git change preview did not open")
			}
			if hideWhitespace {
				filtered, visible := h.a.gitPeekDiff(peek, state)
				if !visible || filtered == nil {
					t.Fatal("filtered Git preview was not created")
				}
			}
			h.a.cfg.SoftWrapByLanguage["Markdown"] = true
			h.a.applyEditorPrefsAll()
			if !state.SoftWrap {
				t.Error("open editor did not pick up the enabled Markdown override")
			}
			if !peek.diff.Wrap {
				t.Error("open Git preview did not pick up the enabled Markdown override")
			}
			if hideWhitespace && !peek.filtered.Wrap {
				t.Error("cached filtered Git preview did not pick up the enabled Markdown override")
			}
		})
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
