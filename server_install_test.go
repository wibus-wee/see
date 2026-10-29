package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

func TestInstallTypeScriptServerUsesPrivateDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	script := "#!/bin/sh\nmkdir -p node_modules/tsrs/bin\nprintf 'server' > node_modules/tsrs/bin/tsrs\n"
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	dest := filepath.Join(t.TempDir(), "typescript")
	if err := installTypeScriptServer(context.Background(), dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "node_modules", "tsrs", "bin", "tsrs")); err != nil {
		t.Fatal(err)
	}
	if err := installTypeScriptServer(context.Background(), dest); err == nil {
		t.Fatal("overwrote existing installation")
	}
}

func TestInstallingFromSettingsKeepsServer(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SEE_TYPESCRIPT_LANGUAGE_SERVER", "")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\nmkdir -p node_modules/tsrs/bin\nprintf server > node_modules/tsrs/bin/tsrs\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	h := newHarness(t, t.TempDir())
	h.a.settings.pane = "intelligence"
	st := ui.NewTester(h.a.settingsView, 900, 680)
	if err := st.Click("Install TypeScript & JavaScript"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.serverJob })
	for range 5 {
		st.Frame()
	}
	if !managedTypeScriptInstalled() {
		t.Fatal("server was removed after installation")
	}
	if !st.HasText("Remove managed TypeScript server") {
		t.Fatalf("missing installed state: %v", st.Texts())
	}
	time.Sleep(50 * time.Millisecond)
	if !managedTypeScriptInstalled() {
		t.Fatal("server was removed after settings rebuilt")
	}
}

func TestInstallTypeScriptServerCleansFailedStage(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\necho broken >&2\nexit 1\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	parent := t.TempDir()
	if err := installTypeScriptServer(context.Background(), filepath.Join(parent, "typescript")); err == nil || !strings.Contains(err.Error(), "broken") {
		t.Fatalf("error = %v", err)
	}
	entries, err := os.ReadDir(parent)
	if err != nil || len(entries) != 0 {
		t.Fatalf("stage not cleaned: %v, %v", entries, err)
	}
}

func TestManagedTypeScriptPersistsAcrossAppInstances(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, "config"))
	t.Setenv("SEE_TYPESCRIPT_LANGUAGE_SERVER", "")
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "bun"), []byte("#!/bin/sh\nmkdir -p node_modules/tsrs/bin\nprintf server > node_modules/tsrs/bin/tsrs\n"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	if err := installTypeScriptServer(context.Background(), managedTypeScriptDir()); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	first := newHarness(t, root)
	first.a.closeWorkspace()
	second := newHarness(t, root)
	defer second.a.closeWorkspace()
	if got, err := findTypeScriptServer(root); err != nil || got != managedTypeScriptEntry() {
		t.Fatalf("after reopening: path %q, error %v", got, err)
	}
}
