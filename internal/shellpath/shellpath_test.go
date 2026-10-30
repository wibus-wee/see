package shellpath

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func TestLoginShellPath(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	shell := filepath.Join(dir, "shell")
	script := "#!/bin/sh\nprintf 'startup banner\\n'\nprintf '\\0%s\\0' '/custom/bin:/usr/bin:/custom/bin:relative'\n"
	if err := os.WriteFile(shell, []byte(script), 0755); err != nil {
		t.Fatal(err)
	}
	path, err := loginShellPath(context.Background(), shell, "/usr/bin:/inherited/bin")
	if err != nil || path != "/custom/bin:/usr/bin:/inherited/bin" {
		t.Fatalf("path %q, error %v", path, err)
	}
}

func TestLoginShellPathTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nexec sleep 10\n"), 0755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := loginShellPath(ctx, shell, "/usr/bin"); err == nil {
		t.Fatal("expected timeout")
	}
}

func TestLoginShellPathRejectsIncompleteOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	shell := filepath.Join(t.TempDir(), "shell")
	if err := os.WriteFile(shell, []byte("#!/bin/sh\nprintf 'banner only'\n"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := loginShellPath(context.Background(), shell, "/usr/bin"); err == nil || !strings.Contains(err.Error(), "invalid") {
		t.Fatalf("error %v", err)
	}
}
