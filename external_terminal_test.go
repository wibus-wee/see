package main

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

func TestExternalTerminalCommands(t *testing.T) {
	dir := "/tmp/a folder/'$(touch nope)"
	for _, tc := range []struct{ name, program, target string }{
		{"Terminal", "open", "com.apple.Terminal"},
		{"Ghostty", "open", "com.mitchellh.ghostty"},
		{"Rex", "open", "com.superlogical.rex"},
		{"Seedmux", "osascript", seedmuxService},
	} {
		cmd, err := externalTerminalCommand(tc.name, "", dir, "darwin")
		if err != nil || filepath.Base(cmd.Path) != tc.program || !slices.Contains(cmd.Args, tc.target) || cmd.Args[len(cmd.Args)-1] != dir {
			t.Fatalf("%s: command %v, error %v", tc.name, cmd, err)
		}
	}
	if _, err := externalTerminalCommand("Ghostty", "", dir, "linux"); err == nil {
		t.Fatal("expected an unsupported platform error")
	}
	cmd, err := externalTerminalCommand("Custom App", "/Applications/My Terminal.app", dir, "darwin")
	if err != nil || !slices.Equal(cmd.Args[1:], []string{"-a", "/Applications/My Terminal.app", dir}) {
		t.Fatalf("custom app: %v, %v", cmd, err)
	}
	if _, err := externalTerminalCommand("Custom App", "", dir, "darwin"); err == nil {
		t.Fatal("expected an error for a missing custom app")
	}
}

func TestExternalTerminalSettingAndShortcut(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.go": "package a\n"}))
	h.open("a.go")
	h.a.cfg.path = filepath.Join(t.TempDir(), "config.json")
	h.a.settings.pane = "terminal"
	st := ui.NewTester(h.a.settingsView, 760, 560)
	if err := st.Click("External Terminal App"); err != nil {
		t.Fatal(err)
	}
	if err := st.Click("Ghostty"); err != nil {
		t.Fatal(err)
	}
	st.Frame()
	if got := loadConfig(h.a.cfg.path).ExternalTerminal; got != "Ghostty" {
		t.Fatalf("saved terminal = %q", got)
	}
	if err := st.Click("External Terminal App"); err != nil {
		t.Fatal(err)
	}
	if err := st.Click("Custom App"); err != nil {
		t.Fatal(err)
	}
	if err := st.Click("External Terminal App Path"); err != nil {
		t.Fatal(err)
	}
	st.Type("/Applications/My Terminal.app")
	st.Frame()
	if got := loadConfig(h.a.cfg.path); got.ExternalTerminal != "Custom App" || got.ExternalTerminalPath != "/Applications/My Terminal.app" {
		t.Fatalf("saved custom app: %q, %q", got.ExternalTerminal, got.ExternalTerminalPath)
	}
	var found bool
	for _, cmd := range commands() {
		if cmd.name == "Open in External Terminal" && cmd.shortcutMods == ui.Cmd|ui.Shift && cmd.shortcutKey == ui.KeyC {
			found = true
		}
	}
	if !found {
		t.Fatal("external terminal shortcut missing")
	}
	if !slices.ContainsFunc(editorBindings(), func(b editor.Binding) bool { return b.Mods == ui.Cmd|ui.Shift && b.Key == ui.KeyC }) {
		t.Fatal("editor did not release the shortcut")
	}
}
