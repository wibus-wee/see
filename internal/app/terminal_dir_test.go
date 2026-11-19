package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"see/internal/config"
)

func TestTerminalDirOK(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "f.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	a := &App{root: root}
	for _, tc := range []struct {
		dir  string
		want bool
	}{
		{root, true},
		{sub, true},
		{file, false},                        // a file, not a folder
		{filepath.Join(root, "gone"), false}, // deleted
		{filepath.Dir(root), false},          // outside the workspace
		{"", false},
	} {
		if got := a.terminalDirOK(tc.dir); got != tc.want {
			t.Errorf("terminalDirOK(%q) = %v, want %v", tc.dir, got, tc.want)
		}
	}
}

func TestPaneDirFallsBackToWorkspaceRoot(t *testing.T) {
	a := &App{root: t.TempDir()}
	if got := a.paneDir(nil); got != a.root {
		t.Fatalf("no pane: %q, want root", got)
	}
	// A pane without a running shell reports no directory; the split
	// falls back to the workspace's root.
	p := &termPane{id: 1}
	if got := a.paneDir(p); got != a.root {
		t.Fatalf("no shell: %q, want root", got)
	}
}

func TestShellTitleIsTheShellThatRuns(t *testing.T) {
	// A terminal of the configured shell shows its name, not $SHELL's,
	// until the shell's first OSC title arrives.
	a := &App{cfg: &config.Config{TermShell: "/usr/local/bin/fish"}}
	if got := a.shellTitle(); got != "fish" {
		t.Fatalf("configured shell: %q, want fish", got)
	}
	a = &App{cfg: &config.Config{}}
	if got := a.shellTitle(); got != shellName() {
		t.Fatalf("default shell: %q, want %q", got, shellName())
	}
}

func TestShowInFolderLabel(t *testing.T) {
	got := showInFolderLabel()
	want := "Show in File Manager"
	switch runtime.GOOS {
	case "darwin":
		want = "Show in Finder"
	case "windows":
		want = "Show in Explorer"
	}
	if got != want {
		t.Errorf("showInFolderLabel() = %q, want %q", got, want)
	}
}
