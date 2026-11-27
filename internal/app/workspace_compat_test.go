package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/search"
	"see/internal/workspace"
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
	if !h.tt.HasText("comparison") || h.tt.HasText("client") {
		t.Fatalf("workspace title does not use its workspace identity: %q", h.tt.Texts())
	}

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
	for _, label := range []string{filepath.Join("Client", "src"), filepath.Join("Server", "src")} {
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

func TestSingleRootCodeWorkspaceKeepsItsProjectItemInTree(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "Project")
	src := filepath.Join(root, "src")
	if err := os.MkdirAll(src, 0o700); err != nil {
		t.Fatal(err)
	}
	workspaceFile := filepath.Join(base, "comparison.code-workspace")
	if err := os.WriteFile(workspaceFile, []byte(`{"folders":[{"name":"Project","path":"Project"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool { return h.a.dirs[root] != nil && h.a.dirs[root].loaded })
	h.tt.Frame()
	if !h.tt.HasText("Project") || !h.tt.HasText("src") {
		t.Fatalf("single-root workspace item and child folder not shown: %q", h.tt.Texts())
	}
	if h.a.tree.Rows() != 2 || h.a.tree.Item(0) != root || h.a.tree.Depth(0) != 0 || h.a.tree.Item(1) != src || h.a.tree.Depth(1) != 1 {
		t.Fatalf("single-root workspace tree rows = %d, text=%q", h.a.tree.Rows(), h.tt.Texts())
	}
}

func TestDuplicateTabSuffixesQualifyCollidingWorkspaceRoots(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool {
		return h.a.dirs[client] != nil && h.a.dirs[client].loaded && h.a.dirs[server] != nil && h.a.dirs[server].loaded
	})
	for _, path := range []string{filepath.Join(client, "src", "main.go"), filepath.Join(server, "src", "main.go")} {
		h.a.userOpen(path)
		d := h.a.docs[path]
		if d == nil {
			t.Fatalf("file was not opened: %q", path)
		}
		h.settle(func() bool { return !d.loading })
	}
	h.tt.Frame()
	if len(h.a.focus.order) != 2 || !h.tt.HasText("main.go") || !h.tt.HasText("Client/src") || !h.tt.HasText("Server/src") {
		t.Fatalf("colliding tab suffixes do not identify both workspace roots: %q", h.tt.Texts())
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

	oldServer := h.a.lsp
	var launchedRoot string
	h.a.startLSP = func(func(string, ...any)) langserver.Starter {
		return func(_ context.Context, root string, _ func(error)) (langserver.Client, error) {
			launchedRoot = root
			return h.fc, nil
		}
	}
	h.a.removeWorkspaceFolder(client)
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 1 })
	if len(h.a.find.results) != 1 || h.a.find.results[0].Path != filepath.Join(server, "src", "main.go") {
		t.Fatalf("search results after removing folder = %+v", h.a.find.results)
	}
	if len(h.a.workspaceFolders) != 1 || h.a.root != server || h.a.withinWorkspace(filepath.Join(client, "src", "main.go")) {
		t.Fatalf("folder reference was not removed: root=%q folders=%v", h.a.root, h.a.workspaceFolders)
	}
	h.tt.Frame()
	if h.a.tree.Rows() == 0 || h.a.tree.Item(0) != server || h.a.tree.Depth(0) != 0 || !h.tt.HasText("Server") {
		t.Fatalf("remaining workspace member is not shown as a root item: rows=%d text=%q", h.a.tree.Rows(), h.tt.Texts())
	}
	if h.a.lsp == nil || h.a.lsp == oldServer {
		t.Fatal("removing the first root did not replace its root-scoped language server")
	}
	if _, err := h.a.lsp.Get(context.Background()); err != nil || launchedRoot != server {
		t.Fatalf("language server launched for %q, want %q; error %v", launchedRoot, server, err)
	}
	if got, err := os.ReadFile(filepath.Join(client, "src", "main.go")); err != nil || string(got) != "package main\n// shared needle\n" {
		t.Fatalf("removing the root changed its file: contents=%q err=%v", got, err)
	}
	folders, err := workspace.ReadCodeWorkspace(workspaceFile)
	if err != nil || len(folders) != 1 || folders[0].Path != server {
		t.Fatalf("workspace file references = %+v, error %v", folders, err)
	}
	if len(h.a.cfg.Workspaces[workspaceFile].Folders) != 0 {
		t.Fatalf("See config still overrides workspace file membership: %+v", h.a.cfg.Workspaces[workspaceFile].Folders)
	}
}

func TestCodeWorkspaceUsesDiskMembershipAndExternalEditsDuringAdd(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	third := filepath.Join(filepath.Dir(client), "tools")
	if err := os.Mkdir(third, 0o755); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(filepath.Dir(filepath.Dir(client)), "stale")
	if err := os.Mkdir(stale, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{Workspaces: map[string]config.Workspace{
		workspaceFile: {Folders: []config.WorkspaceFolder{{Path: stale, Name: "Stale"}}},
	}}
	cfg.SetPath(filepath.Join(t.TempDir(), "settings.json"))
	h := newHarnessWithConfig(t, "", cfg)
	h.a.openWorkspace(workspaceFile)
	if !slices.Equal(h.a.workspaceFolderPaths(), []string{client, server}) {
		t.Fatalf("opening .code-workspace used stale See config: %v", h.a.workspaceFolderPaths())
	}

	data := `{"folders":[{"name":"Server","path":"../server"}],"settings":{"editor.tabSize":2}}`
	if err := os.WriteFile(workspaceFile, []byte(data), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.addWorkspaceFolder(third)
	if !slices.Equal(h.a.workspaceFolderPaths(), []string{server, third}) {
		data, _ := os.ReadFile(workspaceFile)
		t.Fatalf("add used stale in-memory folders instead of disk edit: folders=%v notice=%q error=%v file=%s", h.a.workspaceFolderPaths(), h.a.notice, h.a.noticeError, data)
	}
	got, err := os.ReadFile(workspaceFile)
	if err != nil || !strings.Contains(string(got), `"settings":{"editor.tabSize":2}`) || !strings.Contains(string(got), `"path":"../tools"`) {
		t.Fatalf("workspace file after external edit/add = %s, error %v", got, err)
	}
}

func TestCodeWorkspaceEditFailureDoesNotChangeMembership(t *testing.T) {
	workspaceFile, client, _ := codeWorkspaceFixture(t)
	third := filepath.Join(filepath.Dir(filepath.Dir(client)), "tools")
	if err := os.Mkdir(third, 0o755); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "settings.json"))
	h.a.openWorkspace(workspaceFile)
	before := slices.Clone(h.a.workspaceFolderPaths())
	invalid := []byte(`{"folders":[`)
	if err := os.WriteFile(workspaceFile, invalid, 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.addWorkspaceFolder(third)
	if !slices.Equal(h.a.workspaceFolderPaths(), before) {
		t.Fatalf("failed persistence changed in-memory membership: %v", h.a.workspaceFolderPaths())
	}
	if !h.a.noticeError || h.a.notice == "" {
		t.Fatalf("persistence failure was not shown: error=%v notice=%q", h.a.noticeError, h.a.notice)
	}
	got, err := os.ReadFile(workspaceFile)
	if err != nil || !slices.Equal(got, invalid) {
		t.Fatalf("failed persistence changed workspace file: %q, error %v", got, err)
	}
}

func TestQuickOpenMatchesRootAliasesAndKeepsDuplicateFileIdentity(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.a.quick.files = []string{filepath.Join(client, "src", "main.go"), filepath.Join(server, "src", "main.go")}
	for _, test := range []struct {
		query, root string
	}{
		{query: "Client main.go", root: client},
		{query: "server main.go", root: server},
		{query: "client main.go", root: client},
	} {
		h.a.quick.query, h.a.quick.ranked = test.query, "\x00"
		h.a.rankQuick()
		if len(h.a.quick.results) == 0 || h.a.quick.results[0].Path != filepath.Join(test.root, "src", "main.go") {
			t.Fatalf("quick-open %q results = %+v", test.query, h.a.quick.results)
		}
	}
	h.a.openQuickPanel("main.go")
	h.tt.Frame()
	if len(h.a.quick.results) != 2 || h.a.quick.results[0].Path == h.a.quick.results[1].Path {
		t.Fatalf("duplicate quick-open files lost absolute identity: %+v", h.a.quick.results)
	}
	for _, label := range []string{"Client/src", "Server/src"} {
		if !h.tt.HasText(label) {
			t.Errorf("quick-open row does not show workspace label %q: %q", label, h.tt.Texts())
		}
	}
}

func TestQuickOpenDropsListingCompletedAfterRootMembershipChanges(t *testing.T) {
	workspaceFile, client, _ := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	h.a.cfg.SetPath(filepath.Join(t.TempDir(), "settings.json"))
	h.a.openWorkspace(workspaceFile)
	h.a.quick.listing = true
	h.a.quick.filesSeq = 7
	gen, rootsEpoch, seq := h.a.gen, h.a.workspaceRootsEpoch, h.a.quick.filesSeq
	h.a.removeWorkspaceFolder(client)
	cancelled := false
	h.a.finishQuickFileListing(gen, rootsEpoch, seq, []string{filepath.Join(client, "src", "stale.go")}, false, nil, func() { cancelled = true })
	if h.a.quick.listing || h.a.quick.files != nil {
		t.Fatalf("stale file listing populated after membership edit: listing=%v files=%v", h.a.quick.listing, h.a.quick.files)
	}
	if cancelled {
		t.Fatal("stale listing callback was accepted")
	}
}

func TestWorkspaceMembershipRejectsStaleSearchResults(t *testing.T) {
	workspaceFile, client, _ := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.a.find.opts = search.Options{Query: "needle"}
	h.a.find.rgErr = errors.New("ripgrep unavailable")
	stale := search.File{Path: filepath.Join(client, "src", "main.go")}
	h.a.find.results = []search.File{stale}
	cancelled := false
	h.a.find.cancel = func() { cancelled = true }
	h.a.find.running = true
	gen, seq := h.a.gen, h.a.find.seq

	h.a.removeWorkspaceFolder(client)
	if !cancelled || h.a.find.seq == seq || h.a.find.running || len(h.a.find.results) != 0 {
		t.Fatalf("membership edit did not invalidate in-flight search: cancelled=%v seq=%d/%d running=%v results=%v", cancelled, h.a.find.seq, seq, h.a.find.running, h.a.find.results)
	}
	if h.a.findResultCurrent(gen, seq) {
		t.Fatal("search completion remained current after workspace roots changed")
	}
	h.a.addFindResultsForSearch(gen, seq, []search.File{stale})
	if len(h.a.find.results) != 0 {
		t.Fatalf("stale search completion repopulated results: %v", h.a.find.results)
	}
}

func TestAddedRootRequiresWorkspaceTrustBeforeLSP(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	cfg := &config.Config{Trusted: []string{client}}
	h := newHarnessWithConfig(t, "", cfg)
	h.a.openWorkspace(workspaceFile)
	h.a.userOpen(filepath.Join(server, "src", "main.go"))
	h.settle(func() bool {
		d := h.a.docs[filepath.Join(server, "src", "main.go")]
		return h.a.trustAsk && d != nil && d.buf != nil
	})
	if h.a.workspaceTrusted() || !h.a.trustAsk || h.a.lsp == nil || h.a.lsp.Current() != nil {
		t.Fatalf("untrusted independent root was served: trusted=%v trustAsk=%v server=%v", h.a.workspaceTrusted(), h.a.trustAsk, h.a.lsp.Current())
	}
}

func TestNestedCodeWorkspaceRootsKeepOneFileIdentity(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	base := t.TempDir()
	parent := filepath.Join(base, "parent")
	child := filepath.Join(parent, "child")
	if err := os.MkdirAll(filepath.Join(child, "src"), 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(child, "src", "main.go")
	if err := os.WriteFile(file, []byte("package main\n// needle\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	workspaceFile := filepath.Join(base, "nested.code-workspace")
	if err := os.WriteFile(workspaceFile, []byte(`{"folders":[{"path":"parent"},{"path":"parent/child"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool {
		return h.a.dirs[parent] != nil && h.a.dirs[parent].loaded && h.a.dirs[child] != nil && h.a.dirs[child].loaded
	})
	src := filepath.Join(child, "src")
	h.a.tree.Open.Add(src)
	h.a.loadDir(src)
	h.settle(func() bool { return h.a.dirs[src] != nil && h.a.dirs[src].loaded })
	h.tt.Frame()
	if !h.treeHas(parent) || !h.treeHas(child) || !h.treeHas(file) {
		t.Fatalf("nested workspace tree lost a root or file: %q", h.tt.Texts())
	}

	h.a.ensureFiles()
	h.settle(func() bool { return !h.a.quick.listing && h.a.quick.files != nil })
	quickCount := 0
	for _, path := range h.a.quick.files {
		if path == file {
			quickCount++
		}
	}
	if quickCount != 1 {
		t.Fatalf("quick open lists the overlapping file %d times: %v", quickCount, h.a.quick.files)
	}

	h.a.find.opts = search.Options{Query: "needle"}
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running })
	resultCount := 0
	for _, result := range h.a.find.results {
		if result.Path == file {
			resultCount++
		}
	}
	if resultCount != 1 || h.a.find.stats.Files != 1 || h.a.find.stats.Matches != 1 {
		t.Fatalf("overlapping search duplicated the file: count=%d stats=%+v results=%+v", resultCount, h.a.find.stats, h.a.find.results)
	}
}
