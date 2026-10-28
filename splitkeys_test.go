package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// ⌘D splits right and ⇧⌘D down, as iTerm, even with the editor focused,
// which would take ⌘D for Select Next Occurrence.
func TestCmdDSplits(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n\nfunc A() {}\n"})
	h := newHarness(t, root)
	h.open("a.go")
	h.tt.Frame()
	if n := len(h.a.groups()); n != 1 {
		t.Fatalf("%d groups before", n)
	}
	h.tt.Key(ui.Cmd, ui.KeyD)
	h.tt.Frame()
	if n := len(h.a.groups()); n != 2 {
		t.Fatalf("%d groups after ⌘D, want 2", n)
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyD)
	h.tt.Frame()
	if n := len(h.a.groups()); n != 3 {
		t.Fatalf("%d groups after ⇧⌘D, want 3", n)
	}
}
