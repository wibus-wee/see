package app

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"see/internal/lsp"
)

func useEnglishEditorMenu(t *testing.T) {
	t.Helper()
	i18n.SetLocaleFunc(func() string { return "en" })
	t.Cleanup(func() { i18n.SetLocaleFunc(nil) })
}

func focusEditorForContextMenu(h *harness) {
	h.tt.ClickAt(400, 140)
	h.tt.Frame()
}

func openEditorContextMenuFromKeyboard(t *testing.T, h *harness) []string {
	t.Helper()
	h.tt.Frame()
	h.tt.Key(0, ui.KeyContextMenu)
	menu := h.tt.Menu()
	if len(menu) == 0 {
		t.Fatal("keyboard context-menu key did not open the editor menu")
	}
	return menu
}

func TestSeeEditorContextMenuRootOrderAndDepth(t *testing.T) {
	useEnglishEditorMenu(t)
	root := writeTree(t, map[string]string{"main.go": "package main\n\nfunc greet(name string) string { return name }\n"})
	h := newHarness(t, root)
	h.open("main.go")
	focusEditorForContextMenu(h)
	menu := openEditorContextMenuFromKeyboard(t, h)
	want := []string{
		"Go to Definition", "Find References", "Go to Implementation", "Go to Type Definition", "Show Call Hierarchy", "Peek Definition", "-",
		"Rename Symbol", "Show Code Actions", "Edit All in Scope", "Format Document", "Toggle Comment", "-",
		"Cut", "Copy", "Paste", "-", "History & Compare", "Copy & Share",
	}
	if !slices.Equal(menu, want) {
		t.Fatalf("context-menu root = %q, want %q", menu, want)
	}
	for _, path := range [][]string{
		{"History & Compare", commandLabel("history.compare"), "Nested"},
		{"Copy & Share", "Copy Code Reference", "Nested"},
		{"Find References", "Nested"},
	} {
		err := h.tt.ChooseMenuItem(path...)
		if err == nil || !strings.Contains(err.Error(), "has no submenu") {
			t.Fatalf("ChooseMenuItem(%q) error = %v, want a one-level leaf", path, err)
		}
	}
}

func TestSeeEditorContextMenuRegexRequiresRecognizedCaret(t *testing.T) {
	useEnglishEditorMenu(t)
	source := "package main\nimport \"regexp\"\nvar rx = regexp.MustCompile(\"a+\")\nvar greeting = \"Hello\"\n"
	for _, test := range []struct {
		name       string
		caret      int
		selectText bool
		wantRegex  bool
	}{
		{name: "recognized literal at caret", caret: strings.Index(source, `"a+"`) + 2, wantRegex: true},
		{name: "ordinary text selection", caret: strings.Index(source, `"Hello"`) + 2, selectText: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := writeTree(t, map[string]string{"main.go": source})
			h := newHarness(t, root)
			d := h.open("main.go")
			st := h.a.activeEditor()
			focusEditorForContextMenu(h)
			if test.selectText {
				start := strings.Index(source, `"Hello"`) + 1
				st.GoTo(d.buf.Pos(start + 2))
				st.Select(start, start+len("Hello"))
			} else {
				st.GoTo(d.buf.Pos(test.caret))
			}
			if _, found := h.a.focus.regexAtCaret(d, st); found != test.wantRegex {
				t.Fatalf("recognized regex at caret = %v, want %v", found, test.wantRegex)
			}
			menu := openEditorContextMenuFromKeyboard(t, h)
			if got := slices.Contains(menu, "Test Regex"); got != test.wantRegex {
				t.Fatalf("Test Regex visible = %v, menu %q", got, menu)
			}
		})
	}
}

func TestSeeEditorContextMenuFormatSelectionConditions(t *testing.T) {
	useEnglishEditorMenu(t)
	for _, test := range []struct {
		name      string
		selection bool
		rangeCap  bool
		want      bool
	}{
		{name: "no selection", rangeCap: true},
		{name: "no range capability", selection: true},
		{name: "selected range supported", selection: true, rangeCap: true, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			h, _, st := formattingHarness(t)
			focusEditorForContextMenu(h)
			if test.selection {
				st.Select(0, 2)
			}
			if test.rangeCap {
				armRangeFormat(h, nil)
			}
			menu := openEditorContextMenuFromKeyboard(t, h)
			got := slices.Contains(menu, "Format Selection")
			if got != test.want {
				t.Fatalf("Format Selection visible = %v, menu %q", got, menu)
			}
			if test.want && slices.Index(menu, "Format Selection") != slices.Index(menu, "Format Document")+1 {
				t.Fatalf("Format Selection is not after Format Document: %q", menu)
			}
		})
	}
}

func TestSeeEditorContextMenuReadOnlyGuards(t *testing.T) {
	useEnglishEditorMenu(t)
	h, _, st := formattingHarness(t)
	armDocumentFormat(h, nil)
	armRangeFormat(h, nil)
	h.fc.codeActions = []lsp.CodeAction{{Title: "Fix"}}
	h.fc.rename = func(context.Context, string, lsp.Position, string) (*lsp.WorkspaceEdit, error) {
		return nil, nil
	}
	st.Select(0, 2)
	st.ReadOnly = true
	focusEditorForContextMenu(h)
	menu := openEditorContextMenuFromKeyboard(t, h)
	if slices.Contains(menu, "Toggle Comment") || slices.Contains(menu, "Format Selection") {
		t.Fatalf("read-only menu includes a write action: %q", menu)
	}
	for _, item := range []string{"Cut", "Paste", "Show Code Actions", "Rename Symbol", "Format Document", "Edit All in Scope"} {
		if err := h.tt.ChooseMenuItem(item); err == nil || !strings.Contains(err.Error(), "disabled") {
			t.Fatalf("ChooseMenuItem(%q) error = %v, want disabled", item, err)
		}
	}
	if err := h.tt.ChooseMenuItem("Copy", "Nested"); err == nil || !strings.Contains(err.Error(), "has no submenu") {
		t.Fatalf("Copy should remain enabled as a direct item, got error %v", err)
	}
}
