package git

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestDiscoverRepositoriesFindsWorkspaceAndDirectChildren(t *testing.T) {
	workspace := t.TempDir()
	init := func(dir string) {
		t.Helper()
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}
		if out, err := Command(context.Background(), dir, "init", "-q"); err != nil {
			t.Fatalf("git init in %s: %v (%s)", dir, err, out)
		}
	}
	init(workspace)
	frontend := filepath.Join(workspace, "frontend")
	backend := filepath.Join(workspace, "backend")
	nested := filepath.Join(workspace, "tools", "nested")
	init(frontend)
	init(backend)
	init(nested)

	got, err := DiscoverRepositories(context.Background(), workspace)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{workspace, frontend, backend}
	for i, path := range want {
		resolved, err := filepath.EvalSymlinks(path)
		if err != nil {
			t.Fatal(err)
		}
		want[i] = filepath.Clean(resolved)
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("repositories = %v, want %v", got, want)
	}
}
