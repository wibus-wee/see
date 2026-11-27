package langserver_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/proc"
)

func TestWorkspaceExecutableNeverStarts(t *testing.T) {
	root := t.TempDir()
	executable := filepath.Join(root, "tool")
	if err := os.WriteFile(executable, []byte("#!/bin/sh\n: > started\n"), 0755); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(t.TempDir(), "tool")
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
