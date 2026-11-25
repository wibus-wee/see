package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
)

func TestEditorFontSizeCommands(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a", "b.go": "package b"}))
	h.open("a.go")
	first := h.a.activeEditor()
	h.open("b.go")
	second := h.a.activeEditor()
	diff := &editor.DiffState{}
	h.a.focus.diffs = map[string]*gitDiffTab{"font-test": {state: diff}}
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	for _, tc := range []struct {
		name         string
		before, want float64
	}{
		{"Bigger", 0, 14}, {"Smaller", 14, 13},
		{"Bigger", 27.5, 28}, {"Smaller", 9.5, 9},
		{"Bigger", 28, 28}, {"Smaller", 9, 9},
		{"Reset Editor Font Size", 20, 13},
	} {
		h.a.cfg.FontSize = tc.before
		h.a.applyEditorPrefsAll()
		h.a.saveConfig()
		fontCommand(t, tc.name).run(h.a)
		for _, size := range []float64{h.a.cfg.FontSize, float64(first.Font.Size), float64(second.Font.Size), float64(diff.Font.Size), config.Load(h.a.cfg.Path()).FontSize} {
			if size != tc.want {
				t.Fatalf("%s from %g: size %g, want %g", tc.name, tc.before, size, tc.want)
			}
		}
	}
	// A command at a bound must not attempt another save.
	h.a.cfg.FontSize = 28
	if err := os.Mkdir(h.a.cfg.Path()+".tmp", 0700); err != nil {
		t.Fatal(err)
	}
	h.a.logf = func(string, ...any) { t.Error("no-op tried to save configuration") }
	jumpShortcut(t, "Bigger").run(h.a)
}

func fontCommand(t *testing.T, name string) command {
	t.Helper()
	for _, cmd := range commands() {
		if cmd.name == name {
			return cmd
		}
	}
	t.Fatalf("missing command %q", name)
	return command{}
}

func TestEditorFontSizeSaveFailure(t *testing.T) {
	h := newHarness(t, "")
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	h.a.setEditorFontSize(14)
	staging := h.a.cfg.Path() + ".tmp"
	if err := os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	logged := false
	h.a.logf = func(string, ...any) { logged = true }
	h.a.changeEditorFontSize(1)
	if !logged || h.a.cfg.FontSize != 15 || config.Load(h.a.cfg.Path()).FontSize != 14 {
		t.Fatal("failed save must report the error and preserve the last saved settings")
	}
	if err := os.Remove(staging); err != nil {
		t.Fatal(err)
	}
	h.a.changeEditorFontSize(1)
	if config.Load(h.a.cfg.Path()).FontSize != 16 {
		t.Fatal("subsequent command did not persist settings")
	}
}

func TestEditorFontSizeKeys(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a"}))
	h.open("a.go")
	for _, tc := range []struct {
		mods ui.Modifiers
		key  ui.Key
		want float64
	}{{ui.Cmd, ui.KeyEqual, 14}, {ui.Cmd, ui.KeyMinus, 13}, {ui.Cmd | ui.Shift, ui.KeyEqual, 14}} {
		h.tt.Key(tc.mods, tc.key)
		if h.a.cfg.FontSize != tc.want || float64(h.a.activeEditor().Font.Size) != tc.want {
			t.Fatalf("key %v/%v: config=%g editor=%g, want %g", tc.mods, tc.key, h.a.cfg.FontSize, h.a.activeEditor().Font.Size, tc.want)
		}
	}
	before := h.a.sidebarOff
	h.tt.Key(ui.Cmd, ui.Key0)
	if h.a.sidebarOff == before || h.a.cfg.FontSize != 14 {
		t.Fatal("Cmd+0 must remain the navigator toggle")
	}
}

func TestEditorFontSizeWithoutEditors(t *testing.T) {
	h := newHarness(t, "")
	jumpShortcut(t, "Bigger").run(h.a)
	if h.a.cfg.FontSize != 14 {
		t.Fatalf("size = %g", h.a.cfg.FontSize)
	}
	items := h.a.editorFontSizeMenu(func(fn func()) { fn() })
	if len(items) != 3 || items[2].Accelerator != "" {
		t.Fatalf("font menu = %+v", items)
	}
	items[2].Click(nil, nil)
	if h.a.cfg.FontSize != 13 {
		t.Fatalf("reset size = %g", h.a.cfg.FontSize)
	}
}

func TestEditorLineHeightPreference(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\npackage b\n"}))
	h.open("a.go")
	st := h.a.activeEditor()
	diff := &editor.DiffState{}
	h.a.focus.diffs = map[string]*gitDiffTab{"line-height-test": {state: diff}}

	h.a.cfg.EditorLineHeight = 2
	h.a.applyEditorPrefsAll()
	h.tt.Frame()

	if got, want := st.LineTop(1)-st.LineTop(0), float64(st.Font.Size*2); got != want {
		t.Fatalf("editor line height = %g, want %g", got, want)
	}
	if diff.LineHeight != 2 {
		t.Fatalf("diff line height = %g, want 2", diff.LineHeight)
	}
}

func TestCommandShortcutsAllPlatforms(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows"} {
		t.Run(goos, func(t *testing.T) {
			type shortcut struct {
				mods ui.Modifiers
				key  ui.Key
			}
			seen := map[shortcut]string{}
			cmd := ui.Ctrl
			back := ui.Ctrl | ui.Alt
			if goos == "darwin" {
				cmd, back = ui.Super, ui.Ctrl
			}
			for _, c := range commandsFor(goos) {
				if c.shortcutKey == 0 {
					continue
				}
				binding := shortcut{c.shortcutMods, c.shortcutKey}
				if other := seen[binding]; other != "" {
					t.Errorf("%s and %s share %+v", other, c.name, binding)
				}
				seen[binding] = c.name
			}
			for binding, name := range map[shortcut]string{
				{cmd, ui.KeyEqual}: "Bigger", {cmd, ui.KeyMinus}: "Smaller",
				{back, ui.KeyMinus}: "Go Back", {back | ui.Shift, ui.KeyMinus}: "Go Forward",
			} {
				if seen[binding] != name {
					t.Errorf("%+v = %q, want %q", binding, seen[binding], name)
				}
			}
		})
	}
}
