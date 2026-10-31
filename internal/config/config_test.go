package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Load(p)
	c.LastWorkspace = "/w"
	c.Trust("/w")
	c.AddRecent("/w")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2 := Load(p)
	if c2.LastWorkspace != "/w" || !c2.IsTrusted("/w") || len(c2.Recent) != 1 {
		t.Fatalf("loaded %+v", c2)
	}
}

func TestBrokenFileGivesDefaults(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(p, []byte("{broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if c.LastWorkspace != "" || c.path != p {
		t.Fatalf("loaded %+v", c)
	}
}

func TestRecentFoldersStayCapped(t *testing.T) {
	c := Load("")
	for i := 0; i < MaxRecent+5; i++ {
		c.AddRecent(filepath.Join("/w", string(rune('a'+i))))
	}
	if len(c.Recent) != MaxRecent {
		t.Fatalf("len(Recent) = %d, want %d", len(c.Recent), MaxRecent)
	}
	moved := c.Recent[3]
	c.AddRecent(moved)
	if c.Recent[0] != moved {
		t.Fatalf("Recent[0] = %q, want %q moved to the front", c.Recent[0], moved)
	}
	if len(c.Recent) != MaxRecent {
		t.Fatalf("a folder moved, not added: len = %d", len(c.Recent))
	}
	c.ClearRecent()
	if len(c.Recent) != 0 {
		t.Fatalf("cleared: %v", c.Recent)
	}
}

func TestDefaultsClampSettings(t *testing.T) {
	c := Load("")
	if c.EditorFontSize() != 13 || c.TerminalFontSize() != 12 || c.EditorPanelPct() != 66 {
		t.Fatalf("defaults: %v %v %v", c.EditorFontSize(), c.TerminalFontSize(), c.EditorPanelPct())
	}
	c.FontSize, c.TermFontSize, c.PanelPct = 100, 1, 99
	if c.EditorFontSize() != 13 || c.TerminalFontSize() != 12 || c.EditorPanelPct() != 66 {
		t.Fatalf("clamped: %v %v %v", c.EditorFontSize(), c.TerminalFontSize(), c.EditorPanelPct())
	}
	c.FontSize, c.TermFontSize, c.PanelPct = 16, 14, 50
	if c.EditorFontSize() != 16 || c.TerminalFontSize() != 14 || c.EditorPanelPct() != 50 {
		t.Fatalf("kept: %v %v %v", c.EditorFontSize(), c.TerminalFontSize(), c.EditorPanelPct())
	}
}

func TestWorkspaceSizeFallsBack(t *testing.T) {
	if w, h := WorkspaceSize(Workspace{}); w != DefaultWindowWidth || h != DefaultWindowHeight {
		t.Fatalf("empty: %d %d", w, h)
	}
	if w, h := WorkspaceSize(Workspace{WindowWidth: 100, WindowHeight: 9000}); w != DefaultWindowWidth || h != DefaultWindowHeight {
		t.Fatalf("nonsense: %d %d", w, h)
	}
	if w, h := WorkspaceSize(Workspace{WindowWidth: 900, WindowHeight: 700}); w != 900 || h != 700 {
		t.Fatalf("kept: %d %d", w, h)
	}
}

func TestRecentHookRuns(t *testing.T) {
	c := Load("")
	ran := 0
	c.SetRecentHook(func() { ran++ })
	c.AddRecent("/w")
	c.RemoveRecent("/w")
	if ran == 0 {
		t.Fatal("the hook never ran")
	}
}
