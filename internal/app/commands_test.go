package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
	"see/internal/lsp"
)

type symbolClient struct {
	*fakeClient
	path string
}

func (c *symbolClient) WorkspaceSymbols(ctx context.Context, query string) ([]lsp.WorkspaceSymbol, error) {
	if query != "Thing" {
		return nil, nil
	}
	return []lsp.WorkspaceSymbol{{Name: "Thing", Location: lsp.Location{URI: uriOf(c.path), Range: lsp.Range{Start: lsp.Position{Line: 1, Character: 0}}}}}, nil
}

func TestWorkspaceSymbolShortcutAndNavigation(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\nfunc Thing() {}\n"})
	h := newHarness(t, root)
	h.a.cfg.Trust(root)
	h.a.lsp.Stop()
	h.a.tsLSP.Stop()
	h.a.tsLSP = nil
	c := &symbolClient{fakeClient: &fakeClient{enc: lsp.UTF16}, path: filepath.Join(root, "a.go")}
	h.a.lsp = serverOf(root, c)
	defer h.a.lsp.StopWait(time.Second)
	h.tt.Key(ui.Cmd, ui.KeyT)
	h.tt.Frame()
	if !h.a.quick.open || h.a.quick.query != "#" {
		t.Fatalf("shortcut opened query %q", h.a.quick.query)
	}
	h.tt.Type("Thing")
	h.tt.Frame()
	if h.a.quick.query != "#Thing" {
		t.Fatalf("typed query %q", h.a.quick.query)
	}
	h.settle(func() bool { return len(h.a.quick.symbols) == 1 })
	if !h.tt.HasText("Thing") {
		t.Fatal("symbol is not shown")
	}
	h.tt.Key(0, ui.KeyEnter)
	if h.a.quick.open || h.a.activePath() != c.path {
		t.Fatalf("symbol navigation: open=%v path=%q", h.a.quick.open, h.a.activePath())
	}
}

func TestQuickPanelSwitchesModes(t *testing.T) {
	root := writeTree(t, map[string]string{"example.go": "package example\n"})
	h := newHarness(t, root)
	h.tt.Key(ui.Cmd, ui.KeyP)
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Type(">show or hide the navigator")
	h.tt.Frame()
	if len(h.a.quick.results) != 1 || h.a.quick.results[0].Path != "Show or Hide the Navigator" {
		t.Fatalf("command results: %v", h.a.quick.results)
	}
	h.tt.Key(0, ui.KeyEnter)
	if h.a.quick.open || !h.a.sidebarOff {
		t.Fatal("command did not run and close the panel")
	}
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyP)
	h.tt.Frame()
	h.tt.Key(0, ui.KeyBackspace)
	h.tt.Type("example")
	h.tt.Frame()
	if len(h.a.quick.results) != 1 || h.a.quick.results[0].Path != "example.go" {
		t.Fatalf("file results after removing >: %v", h.a.quick.results)
	}
	h.tt.Key(0, ui.KeyEnter)
	if h.a.activePath() != filepath.Join(root, "example.go") {
		t.Fatalf("active file: %q", h.a.activePath())
	}
}

func TestInstallSeeLink(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "bin")
	exe := filepath.Join(t.TempDir(), "See")
	path, already, err := installSeeLink(exe, dir)
	if err != nil || already {
		t.Fatalf("install: already=%v, err=%v", already, err)
	}
	if target, err := os.Readlink(path); err != nil || target != exe {
		t.Fatalf("link target=%q, err=%v", target, err)
	}
	if _, already, err = installSeeLink(exe, dir); err != nil || !already {
		t.Fatalf("reinstall: already=%v, err=%v", already, err)
	}
	if _, _, err = installSeeLink(exe+"-other", dir); err == nil {
		t.Fatal("replaced an existing command")
	}
	if target, _ := os.Readlink(path); target != exe {
		t.Fatalf("existing link changed to %q", target)
	}
}

func TestUninstallSeeLink(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(t.TempDir(), "See")
	path := filepath.Join(dir, "see")
	if _, removed, err := uninstallSeeLink(exe, dir); err != nil || removed {
		t.Fatalf("missing link: removed=%v, err=%v", removed, err)
	}
	if err := os.Symlink(exe+"-other", path); err != nil {
		t.Fatal(err)
	}
	if _, removed, err := uninstallSeeLink(exe, dir); err == nil || removed {
		t.Fatalf("foreign link: removed=%v, err=%v", removed, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("other command"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, removed, err := uninstallSeeLink(exe, dir); err == nil || removed {
		t.Fatalf("regular file: removed=%v, err=%v", removed, err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(exe, path); err != nil {
		t.Fatal(err)
	}
	if _, removed, err := uninstallSeeLink(exe, dir); err != nil || !removed {
		t.Fatalf("installed link: removed=%v, err=%v", removed, err)
	}
	if _, err := os.Lstat(path); !os.IsNotExist(err) {
		t.Fatalf("link still exists: %v", err)
	}
}
