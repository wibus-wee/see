package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"see/internal/search"
)

func codeWorkspaceFixture(t *testing.T) (string, string, string) {
	t.Helper()
	base := t.TempDir()
	client := filepath.Join(base, "client")
	server := filepath.Join(base, "server")
	workspaceDir := filepath.Join(base, "comparison")
	for _, dir := range []string{filepath.Join(client, "src"), filepath.Join(server, "src"), workspaceDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, root := range []string{client, server} {
		if err := os.WriteFile(filepath.Join(root, "src", "main.go"), []byte("package main\n// shared needle\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	workspaceFile := filepath.Join(workspaceDir, "comparison.code-workspace")
	data := `{
		// Independent roots with duplicate file names.
		"folders": [
			{"name": "Client", "path": "../client"},
			{"name": "Server", "path": "../server"},
		],
		"settings": {"files.exclude": {"**/build": true}},
	}`
	if err := os.WriteFile(workspaceFile, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	return workspaceFile, client, server
}

func TestCodeWorkspaceTreeAndSearchKeepDuplicateFilesUnderTheirRoots(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	workspaceFile, client, server := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool {
		return h.a.dirs[client] != nil && h.a.dirs[client].loaded && h.a.dirs[server] != nil && h.a.dirs[server].loaded
	})

	if h.a.workspaceID != workspaceFile || !slices.Equal(h.a.workspaceFolderPaths(), []string{client, server}) {
		t.Fatalf("workspace identity/roots = %q/%v", h.a.workspaceID, h.a.workspaceFolderPaths())
	}
	for _, root := range []string{client, server} {
		if !h.treeHas(root) {
			t.Errorf("tree is missing peer root %q", root)
		}
		src := filepath.Join(root, "src")
		h.a.tree.Open.Add(src)
		h.a.loadDir(src)
	}
	h.settle(func() bool {
		return h.a.dirs[filepath.Join(client, "src")].loaded && h.a.dirs[filepath.Join(server, "src")].loaded
	})
	h.tt.Frame()
	for _, root := range []string{client, server} {
		if !h.treeHas(filepath.Join(root, "src", "main.go")) {
			t.Errorf("tree is missing duplicate file under %q", root)
		}
	}

	h.a.ensureFiles()
	h.settle(func() bool { return !h.a.quick.listing && h.a.quick.files != nil })
	for _, root := range []string{client, server} {
		path := filepath.Join(root, "src", "main.go")
		if !slices.Contains(h.a.quick.files, path) {
			t.Errorf("file search omitted %q", path)
		}
	}
	if h.a.workspacePath(filepath.Join(client, "src", "main.go")) == h.a.workspacePath(filepath.Join(server, "src", "main.go")) {
		t.Fatal("duplicate file paths have the same workspace label")
	}

	h.a.find.opts = search.Options{Query: "needle"}
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 2 })
	if len(h.a.find.results) != 2 || h.a.find.results[0].Path == h.a.find.results[1].Path {
		t.Fatalf("text search results did not preserve both roots: %+v", h.a.find.results)
	}
	h.a.navigator = navFind
	h.tt.Frame()
	for _, label := range []string{"Client/src", "Server/src"} {
		if !h.tt.HasText(label) {
			t.Errorf("search results do not distinguish root %q: %q", label, h.tt.Texts())
		}
	}
	h.a.find.showReplace = true
	h.a.find.replacement = "shared replacement"
	h.a.find.confirm = true
	h.a.replaceFind(nil)
	h.settle(func() bool { return !h.a.find.replacing && !h.a.find.running })
	for _, root := range []string{client, server} {
		contents, err := os.ReadFile(filepath.Join(root, "src", "main.go"))
		if err != nil || !strings.Contains(string(contents), "shared replacement") {
			t.Errorf("replacement did not update %q: contents=%q err=%v", root, contents, err)
		}
	}
}

func TestRemovingCodeWorkspaceFolderOnlyRemovesItsReference(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	workspaceFile, client, server := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	configPath := filepath.Join(t.TempDir(), "settings.json")
	h.a.cfg.SetPath(configPath)
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool {
		return h.a.dirs[client] != nil && h.a.dirs[client].loaded && h.a.dirs[server] != nil && h.a.dirs[server].loaded
	})
	h.a.find.opts = search.Options{Query: "needle"}
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 2 })

	h.a.removeWorkspaceFolder(client)
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 1 })
	if len(h.a.find.results) != 1 || h.a.find.results[0].Path != filepath.Join(server, "src", "main.go") {
		t.Fatalf("search results after removing folder = %+v", h.a.find.results)
	}
	if len(h.a.workspaceFolders) != 1 || h.a.root != server || h.a.withinWorkspace(filepath.Join(client, "src", "main.go")) {
		t.Fatalf("folder reference was not removed: root=%q folders=%v", h.a.root, h.a.workspaceFolders)
	}
	if got, err := os.ReadFile(filepath.Join(client, "src", "main.go")); err != nil || string(got) != "package main\n// shared needle\n" {
		t.Fatalf("removing the root changed its file: contents=%q err=%v", got, err)
	}
	saved := h.a.cfg.Workspaces[workspaceFile].Folders
	if len(saved) != 1 || saved[0].Path != server {
		t.Fatalf("saved workspace references = %+v", saved)
	}
}
