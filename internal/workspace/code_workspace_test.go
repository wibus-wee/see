package workspace

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestReadCodeWorkspaceResolvesRelativeAndAbsoluteRoots(t *testing.T) {
	base := t.TempDir()
	frontend := filepath.Join(base, "frontend")
	backend := filepath.Join(base, "backend")
	for _, root := range []string{frontend, backend} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(base, "project.code-workspace")
	data := `{
		// Workspace files use JSON with comments and trailing commas.
		"folders": [
			{"name": "Client", "path": "frontend"},
			{"path": "` + filepath.ToSlash(backend) + `",},
			// Comments after a trailing comma are valid JSONC.
		],
		"settings": {"files.exclude": {"**/dist": true}}
	}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	folders, err := ReadCodeWorkspace(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Folder{{Path: frontend, Name: "Client"}, {Path: backend, Name: "backend"}}
	if !reflect.DeepEqual(folders, want) {
		t.Fatalf("folders = %#v, want %#v", folders, want)
	}
}

func TestReadCodeWorkspaceRejectsMissingRoots(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "missing.code-workspace")
	if err := os.WriteFile(path, []byte(`{"folders":[{"path":"missing"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodeWorkspace(path); err == nil {
		t.Fatal("missing folder was accepted")
	}
}
