package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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
	parsed, err := ReadCodeWorkspace(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []Folder{{Path: frontend, Name: "Client"}, {Path: backend, Name: "backend"}}
	if !reflect.DeepEqual(parsed.Folders, want) {
		t.Fatalf("folders = %#v, want %#v", parsed.Folders, want)
	}
	if !parsed.Settings.FilesExclude["**/dist"] {
		t.Fatalf("settings were not read: %+v", parsed.Settings)
	}
}

func TestCodeWorkspaceMembershipEditsRoundTripAndPreserveJSONC(t *testing.T) {
	base := t.TempDir()
	client := filepath.Join(base, "client")
	server := filepath.Join(base, "server")
	tools := filepath.Join(base, "tools")
	for _, root := range []string{client, server, tools} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(base, "project.code-workspace")
	original := `{
	// Keep this comment and unrelated settings.
	"folders": [
		{"name":"Client", "path":"client"}, // client root
		{"path":"server", "name":"Server"},
	],
	"settings": {"files.exclude": {"**/dist": true}},
	"extensions": {"recommendations": ["go"]}
}`
	if err := os.WriteFile(path, []byte(original), 0o600); err != nil {
		t.Fatal(err)
	}

	folders, err := AddCodeWorkspaceFolder(path, Folder{Path: tools, Name: "Tools"})
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{folders[0].Name, folders[1].Name, folders[2].Name}; !reflect.DeepEqual(got, []string{"Client", "Server", "Tools"}) {
		t.Fatalf("folders after add = %v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"Keep this comment", "// client root", `"path":"client"`, `"settings": {"files.exclude": {"**/dist": true}}`, `"extensions": {"recommendations": ["go"]}`} {
		if !bytes.Contains(data, []byte(preserved)) {
			t.Errorf("workspace edit dropped %q:\n%s", preserved, data)
		}
	}
	if !bytes.Contains(data, []byte(`"path":"tools"`)) {
		t.Fatalf("added folder path was not relative to the workspace file:\n%s", data)
	}

	folders, err = RemoveCodeWorkspaceFolder(path, server)
	if err != nil {
		t.Fatal(err)
	}
	if got := []string{folders[0].Name, folders[1].Name}; !reflect.DeepEqual(got, []string{"Client", "Tools"}) {
		t.Fatalf("folders after remove = %v", got)
	}
	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, preserved := range []string{"Keep this comment", "// client root", `"settings": {"files.exclude": {"**/dist": true}}`, `"extensions": {"recommendations": ["go"]}`} {
		if !bytes.Contains(data, []byte(preserved)) {
			t.Errorf("workspace edit dropped %q:\n%s", preserved, data)
		}
	}
}

func TestCodeWorkspaceFolderEditRejectsInvalidDiskWithoutWriting(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "project.code-workspace")
	if err := os.WriteFile(path, []byte(`{"folders":[{"path":"root"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"folders":[`), 0o600); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := AddCodeWorkspaceFolder(path, Folder{Path: root, Name: "Root"}); err == nil {
		t.Fatal("expected invalid on-disk workspace to reject membership edit")
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatalf("invalid workspace was modified: %q", after)
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

func TestWriteNewCodeWorkspaceRoundTripsWithoutOverwriting(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "root")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "root.code-workspace")
	if err := WriteNewCodeWorkspace(path, []Folder{{Path: root, Name: filepath.Base(root)}}); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadCodeWorkspace(path)
	if err != nil || len(parsed.Folders) != 1 || parsed.Folders[0].Path != root {
		t.Fatalf("new workspace = %+v, error %v", parsed, err)
	}
	if err := WriteNewCodeWorkspace(path, []Folder{{Path: root}}); err == nil {
		t.Fatal("created over an existing workspace file")
	}
}

func TestReadCodeWorkspaceRejectsRemoteFolders(t *testing.T) {
	path := filepath.Join(t.TempDir(), "remote.code-workspace")
	if err := os.WriteFile(path, []byte(`{"folders":[{"uri":"vscode-remote://ssh-remote+host/project"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := ReadCodeWorkspace(path)
	if err == nil || !strings.Contains(err.Error(), "remote workspace folder") {
		t.Fatalf("remote folder error = %v", err)
	}
}
