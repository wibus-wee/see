package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
)

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
