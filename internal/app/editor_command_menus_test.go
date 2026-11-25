package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo"
	"see/locales"
)

func TestSelectionAndTransformMenuRoutes(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(\"hello world\")\n}\n"}))
	h.open("main.go")
	update := func(fn func()) { fn() }

	selection := h.a.selectionMenu(update)
	if got, want := editorMenuLabels(selection), []string{"Select All", "-", "Select Next Occurrence", "Select All Occurrences", "-", "Expand Selection", "Shrink Selection", "-", "Split into Lines"}; !slices.Equal(got, want) {
		t.Fatalf("Selection menu = %v, want %v", got, want)
	}
	transform := h.a.transformMenu(update)
	if got, want := editorMenuLabels(transform), []string{"Upper Case", "Lower Case", "Title Case", "Snake Case", "Kebab Case", "Camel Case", "Pascal Case", "-", "Sort Lines", "Reverse Lines", "Unique Lines", "Join Lines", "Trim Trailing Whitespace"}; !slices.Equal(got, want) {
		t.Fatalf("Transform menu = %v, want %v", got, want)
	}
	if item := editorMenuItem(selection, "Split into Lines"); item == nil || !item.Disabled {
		t.Fatal("Split into Lines should be disabled without a selection")
	}
	if got := locales.Catalog.Format("en", commandMessages["selection.menu"], nil); got != "Selection" {
		t.Fatalf("English Selection menu label = %q", got)
	}
	if got := locales.Catalog.Format("zh-Hans", commandMessages["transform.menu"], nil); got != "文本转换" {
		t.Fatalf("Chinese Transform menu label = %q", got)
	}
	for _, id := range []string{
		"selection.all", "selection.nextOccurrence", "selection.allOccurrences", "selection.expand", "selection.shrink", "selection.splitLines",
		"transform.upperCase", "transform.lowerCase", "transform.titleCase", "transform.snakeCase", "transform.kebabCase", "transform.camelCase", "transform.pascalCase",
		"transform.sortLines", "transform.reverseLines", "transform.uniqueLines", "transform.joinLines", "transform.trimTrailingWhitespace",
	} {
		if !hasCommandID(id) {
			t.Errorf("command palette is missing %q", id)
		}
	}
}

func TestSelectionAndTransformMenuCommandsRun(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": "package main\n\nfunc main() {\n\tprintln(\"hello world\")\n}\n"}))
	d := h.open("main.go")
	st := h.a.activeEditor()
	update := func(fn func()) { fn() }

	clickEditorMenuItem(t, h.a.selectionMenu(update), "Select All")
	if got := st.Selection(); got.Start() != 0 || got.End() != d.buf.Len() {
		t.Fatalf("Select All selected %d:%d, want 0:%d", got.Start(), got.End(), d.buf.Len())
	}

	text := d.buf.String()
	start := strings.Index(text, "hello world")
	if start < 0 {
		t.Fatal("test string is missing")
	}
	st.Select(start, start+len("hello world"))
	clickEditorMenuItem(t, h.a.transformMenu(update), "Upper Case")
	if !strings.Contains(d.buf.String(), "HELLO WORLD") {
		t.Fatalf("Upper Case did not transform the selection: %q", d.buf.String())
	}
}

func TestSelectionAndTransformCommandEnablement(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"main.go": "x"}))
	if h.a.commandEnabled("selection.all") || h.a.commandEnabled("transform.upperCase") {
		t.Fatal("editor commands should be disabled without an editor")
	}
	h.open("main.go")
	st := h.a.activeEditor()
	if !h.a.commandEnabled("selection.all") || !h.a.commandEnabled("transform.upperCase") {
		t.Fatal("editor commands should be enabled for a writable editor")
	}
	if h.a.commandEnabled("selection.splitLines") {
		t.Fatal("Split into Lines should be disabled without a selection")
	}
	st.Select(0, 1)
	if !h.a.commandEnabled("selection.splitLines") {
		t.Fatal("Split into Lines should be enabled for a selection")
	}
	st.ReadOnly = true
	if h.a.commandEnabled("transform.upperCase") {
		t.Fatal("Transform commands should be disabled for a read-only editor")
	}
	if !h.a.commandEnabled("selection.all") {
		t.Fatal("Selection commands should remain enabled for a read-only editor")
	}
}

func editorMenuLabels(items []*mygo.MenuItem) []string {
	labels := make([]string, 0, len(items))
	for _, item := range items {
		if item.Type == mygo.MenuItemSeparator {
			labels = append(labels, "-")
		} else {
			labels = append(labels, item.Label)
		}
	}
	return labels
}

func editorMenuItem(items []*mygo.MenuItem, label string) *mygo.MenuItem {
	for _, item := range items {
		if item.Label == label {
			return item
		}
	}
	return nil
}

func clickEditorMenuItem(t *testing.T, items []*mygo.MenuItem, label string) {
	t.Helper()
	item := editorMenuItem(items, label)
	if item == nil {
		t.Fatalf("menu item %q is missing: %v", label, editorMenuLabels(items))
	}
	if item.Disabled {
		t.Fatalf("menu item %q is disabled", label)
	}
	if item.Click == nil {
		t.Fatalf("menu item %q has no action", label)
	}
	item.Click(item, nil)
}

func hasCommandID(id string) bool {
	for _, cmd := range commands() {
		if cmd.id == id {
			return true
		}
	}
	return false
}
