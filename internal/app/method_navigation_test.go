package app

import (
	"context"
	"runtime"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

func TestMethodNavigationBindings(t *testing.T) {
	mods := ui.Alt
	if runtime.GOOS == "darwin" {
		mods = ui.Ctrl | ui.Alt
	}
	for _, tc := range []struct {
		name string
		key  ui.Key
	}{{"Next Method", ui.KeyDown}, {"Previous Method", ui.KeyUp}} {
		cmd := jumpShortcut(t, tc.name)
		if cmd.shortcutMods != mods || cmd.shortcutKey != tc.key {
			t.Fatalf("%s = %v+%v, want %v+%v", tc.name, cmd.shortcutMods, cmd.shortcutKey, mods, tc.key)
		}
		for _, other := range commands() {
			if other.name != cmd.name && other.shortcutMods == mods && other.shortcutKey == tc.key {
				t.Errorf("%s conflicts with %s", cmd.name, other.name)
			}
		}
		taken := false
		for _, b := range editorBindings() {
			taken = taken || b.Mods == mods && b.Key == tc.key && b.Command == editor.NoCommand
		}
		if !taken {
			t.Errorf("%s is left to the editor", cmd.name)
		}
	}
}

func TestMethodNavigationShortcuts(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": outlineSource}))
	h.a.cfg.Trust(h.a.root)
	client := &fileSymbolClient{fakeClient: h.fc, symbols: func(context.Context, string) ([]lsp.DocumentSymbol, error) { return outlineSymbols(), nil }}
	h.a.lsp = serverOf(h.a.root, client)
	defer h.a.lsp.StopWait(time.Second)
	d := h.open("a.go")
	st := h.a.activeEditor()
	h.settle(func() bool { return !h.a.outline.loading && len(h.a.outline.symbols) == 4 })
	next, prev := jumpShortcut(t, "Next Method"), jumpShortcut(t, "Previous Method")
	st.GoTo(editor.Pos{Line: 3, Col: 2})
	h.tt.Frame()
	// The struct and its field are skipped; the ends do not wrap.
	for i, step := range []struct {
		cmd  command
		want editor.Pos
	}{
		{next, editor.Pos{Line: 6, Col: 17}}, {next, editor.Pos{Line: 8, Col: 5}}, {next, editor.Pos{Line: 8, Col: 5}},
		{prev, editor.Pos{Line: 6, Col: 17}}, {prev, editor.Pos{Line: 6, Col: 17}},
	} {
		h.tt.Key(step.cmd.shortcutMods, step.cmd.shortcutKey)
		h.tt.Frame()
		if got := st.CursorPos(); got != step.want {
			t.Fatalf("step %d: got %+v want %+v", i, got, step.want)
		}
	}
	if d.buf.String() != outlineSource {
		t.Fatal("shortcut moved lines")
	}
	if got := h.a.currentSymbol(); got != 2 {
		t.Fatalf("jump bar symbol %d", got)
	}
	st.GoTo(editor.Pos{Line: 8, Col: 0})
	h.tt.Frame()
	h.tt.Key(prev.shortcutMods, prev.shortcutKey)
	h.tt.Frame()
	if got := st.CursorPos(); got != (editor.Pos{Line: 6, Col: 17}) {
		t.Fatalf("previous from line start: %+v", got)
	}
	d.buf.Set("\n" + outlineSource)
	st.GoTo(editor.Pos{})
	h.tt.Key(next.shortcutMods, next.shortcutKey)
	h.tt.Frame()
	if got := st.CursorPos(); got != (editor.Pos{}) {
		t.Fatalf("navigated stale symbols: %+v", got)
	}
}

func TestMethodNavigationMenu(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": outlineSource}))
	labels := map[string]bool{}
	for _, item := range h.a.navigationMenuItems(func(fn func()) { fn() }) {
		labels[item.Label] = item.Disabled
	}
	for _, name := range []string{"Next Method", "Previous Method"} {
		if disabled, ok := labels[commandLabel(name)]; !ok || !disabled {
			t.Fatalf("%s menu item without an editor: present %v disabled %v", name, ok, disabled)
		}
	}
	h.open("a.go")
	h.tt.Frame()
	if !h.a.commandEnabled("method.next") {
		t.Fatal("Next Method disabled with an editor")
	}
}
