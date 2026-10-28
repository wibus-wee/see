package main

import (
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestWorkspaceSessionRestoresTabsAndSplit(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "first\nsecond\n", "b.txt": "other\n"})
	h := newHarness(t, root)
	a, b := filepath.Join(root, "a.txt"), filepath.Join(root, "b.txt")
	d := h.open("a.txt")
	h.ed(d).GoTo(editor.Pos{Line: 1, Col: 3})
	h.a.userOpen(b)
	h.a.splitActive(sideBottom)
	h.a.layout.pct = 37
	h.a.sidebarOff = true
	h.a.navigator = navGit
	configFile := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg.path = configFile
	h.a.closeWorkspace()
	h.a.cfg = loadConfig(configFile)
	h.a.openWorkspace(root)
	gs := h.a.groups()
	if len(gs) != 2 || !h.a.layout.vertical || h.a.layout.pct != 37 {
		t.Fatalf("layout not restored: %+v", h.a.layout)
	}
	if len(gs[0].order) != 2 || gs[0].order[0] != a || gs[0].order[1] != b || gs[1].active != b || h.a.focus != gs[1] {
		t.Fatalf("tabs or focus not restored: %+v %+v", gs[0], gs[1])
	}
	if !h.a.sidebarOff || h.a.navigator != navGit {
		t.Fatal("sidebar state not restored")
	}
	h.settle(func() bool { return h.a.docs[a] != nil && !h.a.docs[a].loading })
	if got := gs[0].eds[a].CursorPos(); got != (editor.Pos{Line: 1, Col: 3}) {
		t.Fatalf("cursor = %+v", got)
	}
}
