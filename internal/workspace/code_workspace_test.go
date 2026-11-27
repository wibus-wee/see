package workspace

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"see/internal/uri"
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
		"settings": {"files.exclude": {"**/dist": true}, "files.watcherExclude": {"**/coverage": true}}
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
	if !parsed.Settings.FilesExclude["**/dist"] || !parsed.Settings.FilesWatcherExclude["**/coverage"] {
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

func TestReadCodeWorkspaceKeepsMissingRootsRepairable(t *testing.T) {
	base := t.TempDir()
	path := filepath.Join(base, "missing.code-workspace")
	if err := os.WriteFile(path, []byte(`{"folders":[{"path":"missing"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadCodeWorkspace(path)
	if err != nil || len(parsed.Folders) != 1 || parsed.Folders[0].Path != filepath.Join(base, "missing") {
		t.Fatalf("missing folder was not kept repairable: %+v, %v", parsed, err)
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

func TestReadCodeWorkspaceReadsSiblingClauses(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "project.code-workspace")
	data := `{"folders":[{"path":"project"}],"settings":{"files.exclude":{"**/*.js":{"when":"$(basename).ts"}}}}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadCodeWorkspace(path)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Settings.FilesExcludeWhen["**/*.js"] != "$(basename).ts" || len(parsed.Settings.FilesExclude) != 0 {
		t.Fatalf("sibling clause settings = %+v", parsed.Settings)
	}
}

func TestWorkspaceMembershipEditWritesThroughSymlinkAndRemovesMissingFolder(t *testing.T) {
	base := t.TempDir()
	realFile := filepath.Join(base, "real.code-workspace")
	linkFile := filepath.Join(base, "link.code-workspace")
	keep := filepath.Join(base, "keep")
	missing := filepath.Join(base, "missing")
	if err := os.Mkdir(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(missing, 0o700); err != nil {
		t.Fatal(err)
	}
	initial := `{"folders":[{"path":"keep"},{"path":"missing"}]}`
	if err := os.WriteFile(realFile, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realFile, linkFile); err != nil {
		t.Skipf("symlink: %v", err)
	}
	if err := os.Remove(missing); err != nil {
		t.Fatal(err)
	}
	folders, err := RemoveCodeWorkspaceFolder(linkFile, missing)
	if err != nil || len(folders) != 1 || folders[0].Path != keep {
		t.Fatalf("remove missing folder = %+v, %v", folders, err)
	}
	info, err := os.Lstat(linkFile)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("workspace symlink was replaced: info=%v err=%v", info, err)
	}
	if data, err := os.ReadFile(realFile); err != nil || strings.Contains(string(data), "missing") {
		t.Fatalf("workspace target was not updated through symlink: %s err=%v", data, err)
	}
}

func TestReadCodeWorkspaceAcceptsLocalFileURIAndRejectsRemoteURI(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(base, "uri.code-workspace")
	data := `{"folders":[{"uri":"` + uri.FromPath(root) + `"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	parsed, err := ReadCodeWorkspace(path)
	if err != nil || len(parsed.Folders) != 1 || parsed.Folders[0].Path != root {
		t.Fatalf("local file URI root = %+v, %v", parsed, err)
	}
	remote := `{"folders":[{"uri":"vscode-remote://ssh-remote+host/home/project"}]}`
	if err := os.WriteFile(path, []byte(remote), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCodeWorkspace(path); err == nil || !strings.Contains(err.Error(), "not supported") {
		t.Fatalf("remote URI error = %v", err)
	}
}

func TestWorkspaceMembershipEditKeepsSymlinkMemberBase(t *testing.T) {
	base := t.TempDir()
	targetDir := filepath.Join(base, "target")
	linkDir := filepath.Join(base, "links")
	for _, dir := range []string{targetDir, linkDir} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	keep := filepath.Join(targetDir, "keep")
	added := filepath.Join(linkDir, "added")
	for _, dir := range []string{keep, added} {
		if err := os.Mkdir(dir, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(targetDir, "team.code-workspace")
	link := filepath.Join(linkDir, "team.code-workspace")
	if err := os.WriteFile(target, []byte(`{"folders":[{"uri":"`+uri.FromPath(keep)+`"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	folders, err := AddCodeWorkspaceFolder(link, Folder{Path: added})
	if err != nil {
		t.Fatal(err)
	}
	if len(folders) != 2 || folders[1].Path != added {
		t.Fatalf("folders = %+v", folders)
	}
	parsed, err := ReadCodeWorkspace(link)
	if err != nil || len(parsed.Folders) != 2 || parsed.Folders[1].Path != added {
		t.Fatalf("link read = %+v, %v", parsed, err)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"path":"added"`) {
		t.Fatalf("workspace entry is not link-relative: %s", data)
	}
}

func TestWorkspaceMembershipEditKeepsParentDirectoryAlias(t *testing.T) {
	base := t.TempDir()
	realDir := filepath.Join(base, "real")
	aliasDir := filepath.Join(base, "alias")
	if err := os.Mkdir(realDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(realDir, "root"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(realDir, aliasDir); err != nil {
		t.Skipf("symlink: %v", err)
	}
	workspaceFile := filepath.Join(aliasDir, "team.code-workspace")
	if err := os.WriteFile(filepath.Join(realDir, "team.code-workspace"), []byte(`{"folders":[{"path":"root"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := AddCodeWorkspaceFolder(workspaceFile, Folder{Path: filepath.Join(aliasDir, "added")}); err == nil {
		t.Fatal("adding a missing folder unexpectedly succeeded")
	}
	if err := os.Mkdir(filepath.Join(realDir, "added"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := AddCodeWorkspaceFolder(workspaceFile, Folder{Path: filepath.Join(aliasDir, "added")}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(realDir, "team.code-workspace"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"path":"added"`) {
		t.Fatalf("parent alias was canonicalized into member paths: %s", data)
	}
}

func TestWorkspaceMembershipEditPreservesFileURIFolders(t *testing.T) {
	base := t.TempDir()
	uriRoot := filepath.Join(base, "uri-root")
	plainRoot := filepath.Join(base, "plain-root")
	for _, root := range []string{uriRoot, plainRoot} {
		if err := os.Mkdir(root, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	path := filepath.Join(base, "uri-edit.code-workspace")
	data := `{"folders":[{"uri":"` + uri.FromPath(uriRoot) + `"},{"path":"plain-root"}]}`
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	folders, err := RemoveCodeWorkspaceFolder(path, plainRoot)
	if err != nil || len(folders) != 1 || folders[0].Path != uriRoot {
		t.Fatalf("remove beside URI folder = %+v, %v", folders, err)
	}
	folders, err = RemoveCodeWorkspaceFolder(path, uriRoot)
	if err == nil || len(folders) != 0 {
		t.Fatalf("removing the last URI folder = %+v, %v", folders, err)
	}
}
