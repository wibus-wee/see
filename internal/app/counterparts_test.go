package app

import (
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/langserver"
)

func TestCounterpartBindings(t *testing.T) {
	mods := ui.Ctrl | ui.Shift
	if runtime.GOOS == "darwin" {
		mods = ui.Ctrl | ui.Cmd
	}
	for _, tc := range []struct {
		name string
		key  ui.Key
	}{{"Next Counterpart", ui.KeyDown}, {"Previous Counterpart", ui.KeyUp}} {
		cmd := jumpShortcut(t, tc.name)
		if cmd.shortcutMods != mods || cmd.shortcutKey != tc.key {
			t.Fatalf("%s = %v+%v, want %v+%v", tc.name, cmd.shortcutMods, cmd.shortcutKey, mods, tc.key)
		}
		for _, other := range commands() {
			if other.name != cmd.name && other.shortcutMods == mods && other.shortcutKey == tc.key {
				t.Errorf("%s conflicts with %s", cmd.name, other.name)
			}
		}
		for _, b := range editor.DefaultBindings() {
			if (b.Mods == mods && b.Key == tc.key) || (b.ThenMods == mods && b.ThenKey == tc.key) {
				t.Errorf("%s conflicts with editor binding %+v", cmd.name, b)
			}
		}
	}
}

func TestCounterpartShortcutsAndPalette(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"internal/format.go":      "package internal\n\nfunc Format() string { return \"see\" }\n",
		"internal/format_test.go": "package internal\n",
		"internal/format2.go":     "package internal\n",
		"other/format2_test.go":   "package other\n",
	}))
	h.a.trustDismissed = true
	h.open("internal/format.go")
	st := h.a.activeEditor()
	pos := editor.Pos{Line: 2, Col: 5}
	st.GoTo(pos)
	h.tt.Frame()
	next, prev := jumpShortcut(t, "Next Counterpart"), jumpShortcut(t, "Previous Counterpart")
	wantFile := func(rel string) {
		t.Helper()
		p := filepath.Join(h.a.root, rel)
		h.settle(func() bool {
			d := h.a.docs[p]
			return h.a.activePath() == p && d != nil && !d.loading
		})
	}
	h.tt.Key(next.shortcutMods, next.shortcutKey)
	wantFile("internal/format_test.go")
	h.tt.Key(0, ui.KeyRight)
	if got := h.a.activeEditor().CursorPos().Col; got == 0 {
		t.Fatal("newly opened counterpart did not get editor focus")
	}
	h.tt.Key(ui.Ctrl, ui.KeyMinus)
	wantFile("internal/format.go")
	if got := st.CursorPos(); got != pos {
		t.Fatalf("Go Back restored %v, want %v", got, pos)
	}

	palette := jumpShortcut(t, "Command Palette")
	h.tt.Key(palette.shortcutMods, palette.shortcutKey)
	h.tt.Type("next counterpart")
	if !h.tt.HasText(next.name) {
		t.Fatalf("missing counterpart command in palette: %v", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEnter)
	wantFile("internal/format_test.go")
	h.tt.Key(prev.shortcutMods, prev.shortcutKey)
	wantFile("internal/format.go")

	h.open("internal/format2.go")
	h.tt.Type("// saved\n")
	save := jumpShortcut(t, "Save")
	h.tt.Key(save.shortcutMods, save.shortcutKey)
	if !h.tt.HasText("Saved format2.go") {
		t.Fatalf("fixture did not save the file: %v", h.tt.Texts())
	}
	h.a.lspState = langserver.Failed
	h.tt.Frame()
	_, hadRetry := h.tt.Find("Retry gopls")
	if !hadRetry {
		t.Fatal("fixture does not show the gopls Retry action")
	}
	h.tt.Key(next.shortcutMods, next.shortcutKey)
	h.settle(func() bool { return h.a.counterpartMiss.pending })
	if !h.tt.HasText("No counterparts found.") || filepath.Base(h.a.activePath()) != "format2.go" {
		t.Fatalf("unpaired file did not stay open with a notice: %v", h.tt.Texts())
	}
	if _, retry := h.tt.Find("Retry gopls"); !retry {
		t.Fatal("counterpart feedback hid the gopls Retry action")
	}
	if err := h.tt.Click("Related Items"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem("Counterparts"); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("Counterparts should be disabled for an unpaired file: %v", err)
	}
	h.tt.CloseMenu()
	h.open("internal/format.go")
	if h.tt.HasText("No counterparts found.") {
		t.Fatal("the no-match message followed the user to a different file")
	}
}

func TestCounterpartsJumpBar(t *testing.T) {
	root := writeTree(t, map[string]string{
		"Greeter/src/greet.ts":      "// Greeter\n\n// Format a greeting for the named recipient.\nexport function greet(name: string): string {\n  const greeting = `Hello, ${name}!`;\n  return greeting;\n}\n",
		"Greeter/src/greet.test.ts": "import { greet } from \"./greet\";\n\ntest(\"greets by name\", () => {\n  expect(greet(\"Ada\")).toBe(\"Hello, Ada!\");\n});\n",
		"Greeter/src/greet.spec.ts": "import { greet } from \"./greet\";\n",
		"Greeter/src/main.ts":       "import { greet } from \"./greet\";\n\nconsole.log(greet(\"Ada\"));\n",
		"Greeter/README.md":         "# Greeter\n",
	})
	h := newHarness(t, filepath.Join(root, "Greeter"))
	h.a.trustDismissed = true
	h.open("src/greet.ts")
	if err := h.tt.Click("Related Items"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		result := h.a.focus.counterpartMenu
		return result != nil && len(result.paths) == 2
	})
	if got := h.tt.Menu(); !slices.Equal(got, []string{"Counterparts", "-", "Callers", "References", "Call Hierarchy", "Type Hierarchy"}) {
		t.Fatalf("related items = %v", got)
	}
	h.tt.Key(0, ui.KeyDown)
	h.tt.Key(0, ui.KeyRight)
	if got := h.tt.Texts(); !slices.Contains(got, "greet.test.ts") || !slices.Contains(got, "greet.spec.ts") {
		t.Fatalf("counterpart submenu did not open: %v", got)
	}
	if out := os.Getenv("SEE_COUNTERPART_SCREENSHOTS"); out != "" {
		h.a.tsState, h.a.tsErr = langserver.Off, nil
		for _, shot := range []struct {
			name string
			dark bool
		}{{"counterparts-light.png", false}, {"counterparts-dark.png", true}} {
			h.tt.SetDark(shot.dark)
			h.tt.Frame()
			f, err := os.Create(filepath.Join(out, shot.name))
			if err != nil {
				t.Fatal(err)
			}
			err = png.Encode(f, h.tt.Image())
			closeErr := f.Close()
			if err != nil {
				t.Fatal(err)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
		}
	}
	if err := h.tt.ChooseMenuItem("Counterparts", "greet.test.ts"); err != nil {
		t.Fatal(err)
	}
	wantFile := func(name string) {
		t.Helper()
		p := filepath.Join(h.a.root, "src", name)
		h.settle(func() bool {
			d := h.a.docs[p]
			return h.a.activePath() == p && d != nil && !d.loading
		})
	}
	wantFile("greet.test.ts")
	if h.tt.Menu() != nil {
		t.Fatal("the menu stayed open after choosing a counterpart")
	}
	next, prev := jumpShortcut(t, "Next Counterpart"), jumpShortcut(t, "Previous Counterpart")
	h.tt.Key(next.shortcutMods, next.shortcutKey)
	wantFile("greet.spec.ts")
	h.tt.Key(next.shortcutMods, next.shortcutKey)
	wantFile("greet.ts")
	h.tt.Key(prev.shortcutMods, prev.shortcutKey)
	wantFile("greet.spec.ts")
	st := h.a.activeEditor()
	st.GoTo(editor.Pos{Line: 1})
	h.tt.Type("// ready")
	if got := st.Buffer.Line(1); got != "// ready" {
		t.Fatalf("counterpart editor did not receive typing; line = %q", got)
	}
}

func TestCounterpartPaths(t *testing.T) {
	root := writeTree(t, map[string]string{"Widget.h": "", "Widget.mm": "", "WidgetTests.swift": "", "Widget.swift": "", "other/Widget.cc": ""})
	if err := os.Mkdir(filepath.Join(root, "Widget.cpp"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		source string
		names  []string
	}{
		{"Widget.mm", []string{"Widget.h", "Widget.mm"}},
		{"Widget.h", []string{"Widget.h", "Widget.mm"}},
		{"WidgetTests.swift", []string{"Widget.swift", "WidgetTests.swift"}},
	} {
		want := make([]string, len(tc.names))
		for i, name := range tc.names {
			want[i] = filepath.Join(root, name)
		}
		if got := counterpartPaths(filepath.Join(root, tc.source), counterpartNames(tc.source)); !slices.Equal(got, want) {
			t.Errorf("counterparts for %s = %v, want %v", tc.source, got, want)
		}
	}
}
