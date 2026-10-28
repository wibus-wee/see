package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
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
