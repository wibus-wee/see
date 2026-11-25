package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReaderModeDefaultAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "reader.json")
	c := Load(p)
	if c.HideDocComments {
		t.Fatal("reader mode should default on")
	}
	c.HideDocComments = true
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if !Load(p).HideDocComments {
		t.Fatal("disabled reader mode was not persisted")
	}
	c.HideDocComments = false
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if Load(p).HideDocComments {
		t.Fatal("enabled reader mode was not persisted")
	}
}

func TestGitAutoDiscoverDefaultsOffAndPersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	if c.GitAutoDiscover {
		t.Fatal("nested Git repository discovery should default off")
	}
	c.GitAutoDiscover = true
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if !Load(p).GitAutoDiscover {
		t.Fatal("nested Git repository discovery was not persisted")
	}
}

func TestSoftWrapByLanguageDefaultsAndPersistence(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	if c.SoftWrapForLanguage("Markdown") {
		t.Fatal("language wrapping should inherit the disabled global default")
	}
	c.SoftWrap = true
	if !c.SoftWrapForLanguage("Go") {
		t.Fatal("language wrapping should inherit the enabled global default")
	}
	c.SoftWrapByLanguage = map[string]bool{"Markdown": false, "Go": true}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	loaded := Load(p)
	if loaded.SoftWrapForLanguage("Markdown") || !loaded.SoftWrapForLanguage("Go") || !loaded.SoftWrapForLanguage("Python") {
		t.Fatalf("loaded language wrapping settings: global=%v overrides=%v", loaded.SoftWrap, loaded.SoftWrapByLanguage)
	}
}

func TestLSPConfigParses(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	json := `{
		"lsp": [
			{
				"name": "rust-analyzer",
				"extensions": [".rs"],
				"command": ["rust-analyzer"],
				"env": {"RA_LOG": "info"},
				"initializationOptions": {"check": {"command": "clippy"}}
			},
			{
				"name": "pyright",
				"languages": ["python"],
				"command": ["pyright-langserver", "--stdio"]
			}
		]
	}`
	if err := os.WriteFile(p, []byte(json), 0o600); err != nil {
		t.Fatal(err)
	}
	c := Load(p)
	if len(c.LSP) != 2 {
		t.Fatalf("parsed %d servers, want 2: %+v", len(c.LSP), c.LSP)
	}
	rust := c.LSP[0]
	if rust.Name != "rust-analyzer" || len(rust.Extensions) != 1 || rust.Extensions[0] != ".rs" {
		t.Fatalf("rust-analyzer parsed wrong: %+v", rust)
	}
	if len(rust.Command) != 1 || rust.Command[0] != "rust-analyzer" {
		t.Fatalf("command: %+v", rust.Command)
	}
	if rust.Env["RA_LOG"] != "info" {
		t.Fatalf("env: %+v", rust.Env)
	}
	if rust.InitializationOptions == nil {
		t.Fatal("initializationOptions dropped")
	}
	py := c.LSP[1]
	if py.Name != "pyright" || len(py.Languages) != 1 || py.Languages[0] != "python" {
		t.Fatalf("pyright parsed wrong: %+v", py)
	}
	if len(py.Command) != 2 || py.Command[1] != "--stdio" {
		t.Fatalf("command args: %+v", py.Command)
	}
}

func TestRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c := Load(p)
	c.LastWorkspace = "/w"
	c.EditorFoldImports = true
	c.HideWhitespaceChanges = true
	c.Workspaces = map[string]Workspace{"/w": {Layout: &Layout{Tabs: []Tab{{Path: "/w/main.go", Folds: []int{2, 7}}, {Path: "/w/empty.go", Folds: []int{}}}}}}
	c.Trust("/w")
	c.AddRecent("/w")
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	c2 := Load(p)
	if !c2.EditorFoldImports || !c2.HideWhitespaceChanges || len(c2.Workspaces["/w"].Layout.Tabs[0].Folds) != 2 || c2.Workspaces["/w"].Layout.Tabs[1].Folds == nil {
		t.Fatalf("folding preferences or session lost: %+v", c2)
	}
	if c2.LastWorkspace != "/w" || !c2.IsTrusted("/w") || len(c2.Recent) != 1 {
		t.Fatalf("loaded %+v", c2)
	}
}

func TestHiddenNamesDefaultAndEmpty(t *testing.T) {
	p := filepath.Join(t.TempDir(), "config.json")
	c := Load(p)
	if c.HiddenNames != ".DS_Store" {
		t.Fatalf("default hidden names = %q", c.HiddenNames)
	}
	c.HiddenNames = ""
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	if got := Load(p).HiddenNames; got != "" {
		t.Fatalf("empty hidden names did not persist: %q", got)
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

func TestFailedSavePreservesPreviousSettingsAndCanRetry(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	c := Load(path)
	c.Trust("/original")
	c.LastWorkspace = "/original"
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Block the staging file without depending on platform permission behavior.
	staging := path + ".tmp"
	if err := os.Mkdir(staging, 0700); err != nil {
		t.Fatal(err)
	}
	c.LastWorkspace = "/replacement"
	c.Trust("/replacement")
	if err := c.Save(); err == nil {
		t.Fatal("save succeeded despite blocked staging path")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatal("failed save changed the committed settings")
	}
	loaded := Load(path)
	if loaded.LastWorkspace != "/original" || loaded.IsTrusted("/replacement") {
		t.Fatalf("failed save leaked new settings: %+v", loaded)
	}
	if err := os.Remove(staging); err != nil {
		t.Fatal(err)
	}
	if err := c.Save(); err != nil {
		t.Fatal(err)
	}
	loaded = Load(path)
	if loaded.LastWorkspace != "/replacement" || !loaded.IsTrusted("/original") || !loaded.IsTrusted("/replacement") {
		t.Fatal("save could not recover after staging failure")
	}
}
