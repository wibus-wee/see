package langserver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/proc"
)

func TestWorkspaceExecutableNeverStarts(t *testing.T) {
	root := t.TempDir()
	// The planted executable must resolve on the running platform, or the
	// denial the test measures never gets to fire: resolution fails first,
	// which is not the trust decision. Windows resolves executables by
	// PATHEXT extension, so the file is tool.exe there and carries the test
	// binary's own bytes — a valid program whose running is exactly what
	// the denial exists to prevent. POSIX keeps the shell script, whose
	// marker write proves nothing ran.
	name, content := "tool", []byte("#!/bin/sh\n: > started\n")
	if runtime.GOOS == "windows" {
		name = "tool.exe"
		own, err := os.ReadFile(os.Args[0])
		if err != nil {
			t.Fatal(err)
		}
		content = own
	}
	executable := filepath.Join(root, name)
	if err := os.WriteFile(executable, content, 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), name)
	if err := os.Symlink(executable, alias); err != nil {
		t.Skip(err)
	}
	t.Setenv("PATH", filepath.Dir(alias))
	s := langserver.New(langserver.Options{Root: root, Start: func(ctx context.Context, root string, _ []lsp.WorkspaceFolder, exit func(error)) (langserver.Client, error) {
		return lsp.Start(ctx, alias, nil, lsp.Options{RootPath: root, OnExit: exit})
	}})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	defer cancel()
	if _, err := s.Get(ctx); !errors.Is(err, proc.ErrUntrustedExecutable) || ctx.Err() != nil {
		t.Fatalf("workspace launch: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "started")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("untrusted process marker: %v", err)
	}
}
