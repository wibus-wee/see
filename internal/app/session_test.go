package app

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

func TestWorkspaceWindowSizeRestoresPerProject(t *testing.T) {
	first, second := t.TempDir(), t.TempDir()
	h := newHarness(t, first)
	configFile := filepath.Join(t.TempDir(), "config.json")
	h.a.cfg.path = configFile
	h.a.resizedWorkspace(1050, 700)
	h.a.saveConfig()
	h.a.openWorkspace(second)
	if h.a.windowWidth != defaultWindowWidth || h.a.windowHeight != defaultWindowHeight {
		t.Fatal("new project did not use default size")
	}
	h.a.resizedWorkspace(1440, 900)
	h.a.saveConfig()
	h.a.cfg = loadConfig(configFile)
	var width, height int
	h.host.resize = func(w, h int) { width, height = w, h }
	h.a.openWorkspace(first)
	if width != 1050 || height != 700 {
		t.Fatalf("first size = %d x %d", width, height)
	}
	h.a.openWorkspace(second)
	if width != 1440 || height != 900 {
		t.Fatalf("second size = %d x %d", width, height)
	}
	h.a.closeWorkspace()
	h.a.resizedWorkspace(welcomeWidth, welcomeHeight)
	if got := h.a.cfg.Workspaces[second]; got.WindowWidth != 1440 || got.WindowHeight != 900 {
		t.Fatalf("welcome changed project size: %+v", got)
	}
}
