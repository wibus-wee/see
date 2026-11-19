package app

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
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
