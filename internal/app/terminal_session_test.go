package app

import (
	"os"
	"path/filepath"
	"testing"

	"see/internal/config"
)

func TestTermSessionCapturesTabsPanesAndPanel(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	dir := filepath.Join(h.a.root, "sub")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p1 := &termPane{id: 1, title: "zsh"}
	p2 := &termPane{id: 2, title: "build"}
	p3 := &termPane{id: 3, title: "tests"}
	h.a.term = termPanel{
		open: true,
		view: panelTerminal,
		tabs: []*termTab{
			{id: 1, panes: []*termPane{p1, p2}, active: p2},
			{id: 3, panes: []*termPane{p3}, active: p3},
		},
		active: nil,
	}
	h.a.term.active = h.a.term.tabs[1]
	tabs, active, open := h.a.termSession()
	if !open || active != 1 || len(tabs) != 2 {
		t.Fatalf("open=%v active=%d tabs=%+v", open, active, tabs)
	}
	if len(tabs[0].Panes) != 2 || tabs[0].Active != 1 || tabs[0].Panes[0].Title != "zsh" || tabs[0].Panes[1].Title != "build" {
		t.Fatalf("first tab: %+v", tabs[0])
	}
	if tabs[1].Active != 0 || tabs[1].Panes[0].Title != "tests" {
		t.Fatalf("second tab: %+v", tabs[1])
	}
}

func TestTermSessionEmptyWhenNoTerminalsOrPanelNotTerminal(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	tabs, active, open := h.a.termSession()
	if tabs != nil || active != 0 || open {
		t.Fatalf("empty panel kept %v %d %v", tabs, active, open)
	}
	h.a.term = termPanel{open: true, view: panelOutput}
	tabs, active, open = h.a.termSession()
	if tabs != nil || open {
		t.Fatalf("output view kept terminals %v %v", tabs, open)
	}
}

func TestTermDirFallsBackToWorkspaceRoot(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	sub := filepath.Join(h.a.root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := h.a.termDir(sub); got != sub {
		t.Fatalf("existing dir: %q", got)
	}
	gone := filepath.Join(h.a.root, "gone")
	if got := h.a.termDir(gone); got != h.a.root {
		t.Fatalf("gone dir fell back to %q, want root", got)
	}
	outside := filepath.Dir(h.a.root)
	if got := h.a.termDir(outside); got != h.a.root {
		t.Fatalf("outside dir gave %q, want root", got)
	}
	if got := h.a.termDir(""); got != h.a.root {
		t.Fatalf("empty dir gave %q, want root", got)
	}
}

func TestOpenRestoredTerminalsWithoutRootDoesNothing(t *testing.T) {
	h := newHarness(t, "")
	s := config.Workspace{Terminals: []config.TermTab{{Panes: []config.TermPane{{Dir: "/tmp"}}}}, PanelOpen: true}
	h.a.openRestoredTerminals(s)
	if len(h.a.term.tabs) != 0 {
		t.Fatalf("restored terminals without a workspace: %+v", h.a.term.tabs)
	}
}

func TestSessionRoundTripKeepsTerminalLayout(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "text"}))
	h.open("a.txt")
	p := &termPane{id: 1, title: "work"}
	h.a.term = termPanel{open: true, view: panelTerminal, tabs: []*termTab{{id: 1, panes: []*termPane{p}, active: p}}}
	h.a.term.active = h.a.term.tabs[0]
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "config.json"))
	h.a.captureSession()
	s := h.a.cfg.Workspaces[h.a.root]
	if len(s.Terminals) != 1 || !s.PanelOpen || s.Terminals[0].Panes[0].Title != "work" {
		t.Fatalf("session kept %+v", s)
	}
	h.a.saveConfig()
	loaded := config.Load(h.a.cfg.Path())
	lw := loaded.Workspaces[h.a.root]
	if len(lw.Terminals) != 1 || !lw.PanelOpen || lw.Terminals[0].Panes[0].Title != "work" {
		t.Fatalf("reloaded session lost terminals: %+v", lw)
	}
}
