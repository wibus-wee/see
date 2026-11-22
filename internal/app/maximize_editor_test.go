package app

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestMaximizeEditorToggle(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first"}))
	h.open("a.txt")
	h.a.toggleMaximizeEditor()
	if h.a.maximized != nil {
		t.Fatal("single editor maximized")
	}
	h.a.splitActive(sideRight)
	h.a.layout.pct = 37
	h.a.captureSession()
	before := h.a.cfg.Workspaces[h.a.root].Layout
	h.a.toggleMaximizeEditor()
	if h.a.maximized != h.a.focus {
		t.Fatal("focused editor not maximized")
	}
	h.tt.Frame()
	for _, g := range h.a.groups() {
		_, visible := h.tt.Find(fmt.Sprintf("Editor group %d", g.id))
		if visible != (g == h.a.maximized) {
			t.Fatalf("group %d visible=%v", g.id, visible)
		}
	}
	if _, ok := h.tt.Find("Restore Editor Layout"); !ok {
		t.Fatal("restore button missing")
	}
	h.a.captureSession()
	after := h.a.cfg.Workspaces[h.a.root].Layout
	if !reflect.DeepEqual(before, after) {
		t.Fatal("maximize changed session layout")
	}
	h.a.toggleMaximizeEditor()
	h.tt.Frame()
	if h.a.maximized != nil || h.a.layout.pct != 37 {
		t.Fatal("restore changed split percentage")
	}
	for _, g := range h.a.groups() {
		if _, visible := h.tt.Find(fmt.Sprintf("Editor group %d", g.id)); !visible {
			t.Fatalf("restored group %d missing", g.id)
		}
	}
}

func TestMaximizeEditorEmptyAndInvalid(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{}))
	left := h.a.focus
	h.a.splitActive(sideRight)
	right := h.a.focus
	h.a.toggleMaximizeEditor()
	h.tt.Frame()
	h.a.requestGroupFocus(right)
	h.a.splitBeside(right, sideNone)
	h.a.removeGroup(h.a.newGroup())
	if h.a.maximized != right {
		t.Fatal("same focus or invalid operation restored layout")
	}
	if err := h.tt.Click("Restore Editor Layout"); err != nil {
		t.Fatal(err)
	}
	if h.a.maximized != nil {
		t.Fatal("restore button did not restore")
	}
	h.a.focus = h.a.newGroup()
	h.a.toggleMaximizeEditor()
	if h.a.maximized != nil {
		t.Fatal("group outside layout maximized")
	}
	h.a.focus = left
	h.a.toggleMaximizeEditor()
	h.a.removeGroup(right)
	h.tt.Frame()
	if _, visible := h.tt.Find("Restore Editor Layout"); visible {
		t.Fatal("single remaining group has restore button")
	}
}

func TestMaximizeEditorAutoRestore(t *testing.T) {
	for _, action := range []string{"split", "splitBeside", "remove", "focus", "cycle", "move", "request", "drop", "reset"} {
		t.Run(action, func(t *testing.T) {
			h := newHarness(t, writeTree(t, map[string]string{"a.txt": "first"}))
			d := h.open("a.txt")
			left := h.a.focus
			h.a.splitActive(sideRight)
			right := h.a.focus
			h.a.toggleMaximizeEditor()
			switch action {
			case "split":
				h.a.splitActive(sideBottom)
			case "splitBeside":
				h.a.splitBeside(right, sideLeft)
			case "remove":
				h.a.removeGroup(right)
			case "focus":
				h.a.focusGroup(0)
			case "cycle":
				h.tt.Key(0, ui.KeyF6)
			case "move":
				h.a.moveTabToNextEditor()
			case "request":
				h.a.requestGroupFocus(left)
			case "drop":
				h.a.dropTab(&tabDrag{from: left, path: d.path}, right, sideBottom, 0)
			case "reset":
				h.a.resetLayout()
			}
			if h.a.maximized != nil {
				t.Fatal("maximize not cleared")
			}
		})
	}
}

func TestMaximizeEditorShortcuts(t *testing.T) {
	for _, goos := range []string{"darwin", "linux", "windows", "freebsd"} {
		cmds := commandsFor(goos)
		var maximize command
		for _, cmd := range cmds {
			if cmd.name == "Maximize Editor" {
				maximize = cmd
			}
		}
		want := ui.Ctrl | ui.Alt | ui.Shift
		if goos == "darwin" {
			want = ui.Ctrl | ui.Super | ui.Shift
		}
		if maximize.shortcutKey != ui.KeyEnter || maximize.shortcutMods != want {
			t.Fatalf("%s: incorrect maximize shortcut", goos)
		}
		for _, cmd := range cmds {
			if cmd.name != maximize.name && cmd.shortcutKey == maximize.shortcutKey && cmd.shortcutMods == maximize.shortcutMods {
				t.Fatalf("%s: conflicts with %s", goos, cmd.name)
			}
		}
		for _, binding := range editor.DefaultBindings() {
			if binding.Key == maximize.shortcutKey && effectiveEditorMods(goos, binding.Mods) == want {
				t.Fatalf("%s: conflicts with editor binding %+v", goos, binding)
			}
		}
	}
}

func effectiveEditorMods(goos string, mods ui.Modifiers) ui.Modifiers {
	if mods&ui.Cmd != 0 {
		mods &^= ui.Cmd
		if goos == "darwin" {
			mods |= ui.Super
		} else {
			mods |= ui.Ctrl
		}
	}
	return mods
}
