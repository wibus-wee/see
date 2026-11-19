package app

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWindowsSeeCommandInstall(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "a 100% build", "See.exe")
	dir := filepath.Join(root, "bin")
	path := filepath.Join(dir, "see.cmd")
	if got, already, err := installSeeCommand(exe, dir); got != path || already || err != nil {
		t.Fatalf("first install = %q, %v, %v", got, already, err)
	}
	data, err := os.ReadFile(path)
	want := "@echo off\r\nsetlocal DisableDelayedExpansion\r\n\"" +
		filepath.Join(root, "a 100%% build", "See.exe") + "\" --see-cli %*\r\n"
	if err != nil || string(data) != want {
		t.Fatalf("see.cmd = %q, %v; want %q", data, err, want)
	}
	if _, already, err := installSeeCommand(exe, dir); !already || err != nil {
		t.Fatalf("reinstall: already = %v, err = %v", already, err)
	}
	other := filepath.Join(root, "other", "See.exe")
	if _, _, err := installSeeCommand(other, dir); err == nil {
		t.Fatal("install replaced a different application's command")
	}
	if _, removed, err := uninstallSeeCommand(other, dir); removed || err == nil {
		t.Fatalf("other uninstall: removed = %v, err = %v", removed, err)
	}
	if _, removed, err := uninstallSeeCommand(exe, dir); !removed || err != nil {
		t.Fatalf("uninstall: removed = %v, err = %v", removed, err)
	}
	for _, name := range []string{"see", "see.cmd"} {
		if _, err := os.Lstat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("%s still exists: %v", name, err)
		}
	}
}
