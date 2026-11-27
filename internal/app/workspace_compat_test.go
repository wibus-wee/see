package app

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/languages"
	"see/internal/lsp"
	"see/internal/search"
	"see/internal/uri"
	"see/internal/workspace"
)

type channelWatcher struct {
	events chan []mygo.FileEvent
}

func (w channelWatcher) Next(ctx context.Context) ([]mygo.FileEvent, error) {
	select {
	case evs := <-w.events:
		return evs, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (w channelWatcher) Close() error { return nil }

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
			t.Errorf("replacement did not update %q: contents=%q err=%v note=%q", root, contents, err, h.a.find.replaceNote)
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
		return func(_ context.Context, root string, _ []lsp.WorkspaceFolder, _ func(error)) (langserver.Client, error) {
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
	if err != nil || len(folders.Folders) != 1 || folders.Folders[0].Path != server {
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

func TestLanguageServerReceivesEveryWorkspaceFolder(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	h := newHarness(t, "")
	var launched []lsp.WorkspaceFolder
	h.a.startLSP = func(func(string, ...any)) langserver.Starter {
		return func(_ context.Context, _ string, folders []lsp.WorkspaceFolder, _ func(error)) (langserver.Client, error) {
			launched = slices.Clone(folders)
			return h.fc, nil
		}
	}
	h.a.openWorkspace(workspaceFile)
	h.a.trust()
	if _, err := h.a.lsp.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(launched) != 2 || launched[0].URI != uri.FromPath(client) || launched[1].URI != uri.FromPath(server) {
		t.Fatalf("language server workspace folders: %+v", launched)
	}
}

func TestProjectDiagnosticsScanEveryWorkspaceFolder(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	if err := os.Remove(filepath.Join(client, "src", "main.go")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(client, "src", "notes.txt"), []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.a.trust()
	h.a.showProblems()
	h.settle(func() bool { return h.a.lsp.Current() != nil })
	if h.a.lsp.Current() == nil {
		t.Fatalf("project diagnostics never scanned the Go file in the second root %q", server)
	}
}

func TestReloadCodeWorkspaceAppliesExternalMembershipEdits(t *testing.T) {
	workspaceFile, client, server := codeWorkspaceFixture(t)
	extra := filepath.Join(filepath.Dir(client), "extra")
	if err := os.MkdirAll(extra, 0o755); err != nil {
		t.Fatal(err)
	}
	oldWatchFiles := watchFiles
	t.Cleanup(func() { watchFiles = oldWatchFiles })
	workspaceWatch := make(chan chan []mygo.FileEvent, 1)
	watchFiles = func(ctx context.Context, root string, opts mygo.FileWatchOptions) (fileWatcher, error) {
		events := make(chan []mygo.FileEvent, 1)
		if !opts.Recursive && root == filepath.Dir(workspaceFile) {
			workspaceWatch <- events
		}
		return channelWatcher{events: events}, nil
	}
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool { return h.a.dirs[client] != nil && h.a.dirs[server] != nil })
	var workspaceEvents chan []mygo.FileEvent
	select {
	case workspaceEvents = <-workspaceWatch:
	case <-time.After(2 * time.Second):
		t.Fatal("the workspace file itself was not watched")
	}

	updated := `{
		// Edited while See has the workspace open.
		"folders": [
			{"path": "../client"},
			{"path": "../server"},
			{"name": "Extra", "path": "../extra"},
		],
	}`
	if err := os.WriteFile(workspaceFile, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}
	workspaceEvents <- []mygo.FileEvent{{Path: filepath.Base(workspaceFile), Op: mygo.FileWrite}}
	h.settle(func() bool { return h.a.dirs[extra] != nil })
	if len(h.a.workspaceFolders) != 3 || h.a.workspaceFolders[2].Path != extra || h.a.workspaceFolders[2].Name != "Extra" {
		t.Fatalf("external membership edit was not applied: %+v", h.a.workspaceFolders)
	}

	if err := os.WriteFile(workspaceFile, []byte(`{"folders": [`), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.reloadCodeWorkspace(workspaceFile)
	if len(h.a.workspaceFolders) != 3 || h.a.workspaceFolders[2].Path != extra {
		t.Fatalf("a half-written workspace replaced the last valid membership: %+v", h.a.workspaceFolders)
	}
}

func TestCodeWorkspaceSettingsExcludeTreeSearchAndQuickOpen(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	workspaceFile, client, _ := codeWorkspaceFixture(t)
	for path, contents := range map[string]string{
		"build/hidden.txt":       "needle in an excluded folder\n",
		"src/search-hidden.txt":  "needle in a search exclusion\n",
		"src/visible.txt":        "needle visible\n",
		"src/search-visible.txt": "needle visible search\n",
		"generated/one.ts":       "needle excluded by brace\n",
		"generated/two.js":       "needle excluded by brace\n",
	} {
		path = filepath.Join(client, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(data), `"settings": {"files.exclude": {"**/build": true}}`, `"settings": {
		"files.exclude": {"**/build": true, "**/generated": true, "!**/generated/one.ts": true},
		"search.exclude": {"**/search-hidden.txt": true},
	}`, 1)
	if updated == string(data) {
		t.Fatal("workspace fixture settings did not patch")
	}
	if err := os.WriteFile(workspaceFile, []byte(updated), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool { return h.a.dirs[client] != nil && h.a.dirs[client].loaded })
	for _, path := range h.a.entryPaths(h.a.dirs[client]) {
		if filepath.Base(path) == "build" {
			t.Fatalf("files.exclude entry is visible in the tree: %v", h.a.entryPaths(h.a.dirs[client]))
		}
	}
	generated := filepath.Join(client, "generated")
	if !slices.Contains(h.a.entryPaths(h.a.dirs[client]), generated) {
		t.Fatal("the parent of an exclusion exception is not visible")
	}
	h.a.loadDir(generated)
	h.settle(func() bool { return h.a.dirs[generated] != nil && h.a.dirs[generated].loaded })
	generatedEntries := h.a.entryPaths(h.a.dirs[generated])
	if !slices.Contains(generatedEntries, filepath.Join(generated, "one.ts")) || slices.Contains(generatedEntries, filepath.Join(generated, "two.js")) {
		t.Fatalf("exception entries in tree: %v", generatedEntries)
	}

	h.a.ensureFiles()
	h.settle(func() bool { return !h.a.quick.listing && h.a.quick.files != nil })
	for _, path := range h.a.quick.files {
		base := filepath.Base(path)
		if strings.Contains(path, string(filepath.Separator)+"build"+string(filepath.Separator)) || base == "search-hidden.txt" || base == "two.js" {
			t.Fatalf("excluded file is in quick open: %q", path)
		}
	}

	h.a.find.opts = search.Options{Query: "needle"}
	h.a.runFind()
	h.settle(func() bool { return !h.a.find.running })
	for _, result := range h.a.find.results {
		base := filepath.Base(result.Path)
		if strings.Contains(result.Path, string(filepath.Separator)+"build"+string(filepath.Separator)) || base == "search-hidden.txt" || base == "two.js" {
			t.Fatalf("excluded file is in search results: %q", result.Path)
		}
	}
	if h.a.findFile(filepath.Join(client, "src", "visible.txt")) == nil || h.a.findFile(filepath.Join(client, "src", "search-visible.txt")) == nil || h.a.findFile(filepath.Join(client, "generated", "one.ts")) == nil {
		t.Fatalf("visible files missing from search results: %+v", h.a.find.results)
	}
	if !slices.Contains(h.a.quick.files, filepath.Join(client, "generated", "one.ts")) {
		t.Fatal("exclusion exception missing from quick open")
	}
}

func TestReloadCodeWorkspaceAppliesSettingsEdits(t *testing.T) {
	workspaceFile, client, _ := codeWorkspaceFixture(t)
	build := filepath.Join(client, "build")
	if err := os.MkdirAll(build, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(workspaceFile)
	if err != nil {
		t.Fatal(err)
	}
	initial := strings.Replace(string(data), `"**/build": true`, `"**/build": false`, 1)
	if err := os.WriteFile(workspaceFile, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, "")
	h.a.openWorkspace(workspaceFile)
	h.settle(func() bool { return h.a.dirs[client] != nil && h.a.dirs[client].loaded })
	if !slices.Contains(h.a.entryPaths(h.a.dirs[client]), build) {
		t.Fatal("test setup did not leave build visible")
	}

	included := strings.Replace(initial, `"**/build": false`, `"**/build": true`, 1)
	if err := os.WriteFile(workspaceFile, []byte(included), 0o600); err != nil {
		t.Fatal(err)
	}
	h.a.reloadCodeWorkspace(workspaceFile)
	h.settle(func() bool {
		return h.a.dirs[client] != nil && h.a.dirs[client].loaded && !slices.Contains(h.a.entryPaths(h.a.dirs[client]), build)
	})
}

func TestRealGoplsMultiRootProjectProblems(t *testing.T) {
	if os.Getenv("SEE_GOPLS") == "" {
		t.Skip("set SEE_GOPLS to the path of gopls")
	}
	base := t.TempDir()
	var paths []string
	for _, name := range []string{"client", "server"} {
		root := filepath.Join(base, name)
		if err := os.MkdirAll(filepath.Join(root, "checker"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.com/"+name+"\n\ngo 1.21\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(root, "checker", "check.go")
		if err := os.WriteFile(path, []byte("package checker\nvar _ = missingIdentifier\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, path)
	}
	workspaceFile := filepath.Join(base, "multi.code-workspace")
	if err := os.WriteFile(workspaceFile, []byte(`{"folders":[{"path":"client"},{"path":"server"}]}`), 0o600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, "")
	h.a.startLSP = languages.StartGopls
	h.a.cfg.Trust(filepath.Join(base, "client"))
	h.a.cfg.Trust(filepath.Join(base, "server"))
	h.a.openWorkspace(workspaceFile)
	defer h.a.lsp.StopWait(5 * time.Second)
	h.a.showProblems()
	h.settle(func() bool {
		seen := 0
		for _, row := range h.a.problemLocations() {
			for _, path := range paths {
				if row.path == path && strings.Contains(row.diagnostic.Message, "undefined: missingIdentifier") {
					seen++
				}
			}
		}
		return seen == len(paths)
	})
}

func TestAddingFolderToPlainWorkspaceCreatesVSCodeWorkspace(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "primary")
	secondary := filepath.Join(base, "secondary")
	for _, dir := range []string{primary, secondary} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	h := newHarness(t, primary)
	configPath := filepath.Join(t.TempDir(), "settings.json")
	h.a.cfg.SetPath(configPath)
	h.a.addWorkspaceFolder(secondary)
	workspaceFile := filepath.Join(base, "primary.code-workspace")
	h.settle(func() bool { return h.a.dirs[secondary] != nil && h.a.dirs[secondary].loaded })
	if h.a.workspaceID != workspaceFile || !h.a.isCodeWorkspace() || !slices.Equal(h.a.workspaceFolderPaths(), []string{primary, secondary}) {
		t.Fatalf("plain workspace did not become a workspace file: id=%q folders=%v notice=%q", h.a.workspaceID, h.a.workspaceFolderPaths(), h.a.notice)
	}
	parsed, err := workspace.ReadCodeWorkspace(workspaceFile)
	if err != nil || len(parsed.Folders) != 2 || parsed.Folders[0].Path != primary || parsed.Folders[1].Path != secondary {
		t.Fatalf("written workspace = %+v, error %v", parsed, err)
	}
	if len(h.a.cfg.Workspaces[workspaceFile].Folders) != 0 {
		t.Fatalf("workspace file membership duplicated into See-private config: %+v", h.a.cfg.Workspaces[workspaceFile].Folders)
	}
}

func TestPlainWorkspaceReusesExistingWorkspaceFileAndSettings(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "primary")
	secondary := filepath.Join(base, "secondary")
	for _, dir := range []string{primary, secondary} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	workspaceFile := filepath.Join(base, "primary.code-workspace")
	initial := `{"folders":[{"path":"primary"}],"settings":{"files.exclude":{"**/build":true}}}`
	if err := os.WriteFile(workspaceFile, []byte(initial), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, primary)
	h.a.addWorkspaceFolder(secondary)
	if h.a.workspaceID != workspaceFile || len(h.a.workspaceFolders) != 2 {
		t.Fatalf("existing workspace file was not reused: id=%q folders=%v notice=%q", h.a.workspaceID, h.a.workspaceFolders, h.a.notice)
	}
	data, err := os.ReadFile(workspaceFile)
	if err != nil || !strings.Contains(string(data), "files.exclude") || !strings.Contains(string(data), "secondary") {
		t.Fatalf("workspace file lost settings or membership: %s", data)
	}
}

func TestPlainWorkspaceConversionPreservesRestoredMembership(t *testing.T) {
	base := t.TempDir()
	primary := filepath.Join(base, "primary")
	second := filepath.Join(base, "second")
	third := filepath.Join(base, "third")
	for _, dir := range []string{primary, second, third} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{Workspaces: map[string]config.Workspace{
		primary: {Folders: []config.WorkspaceFolder{{Path: primary, Name: "primary"}, {Path: second, Name: "second"}}},
	}}
	h := newHarnessWithConfig(t, "", cfg)
	h.a.openWorkspace(primary)
	if !slices.Equal(h.a.workspaceFolderPaths(), []string{primary, second}) {
		t.Fatalf("restored membership missing: %v", h.a.workspaceFolderPaths())
	}
	h.a.addWorkspaceFolder(third)
	parsed, err := workspace.ReadCodeWorkspace(filepath.Join(base, "primary.code-workspace"))
	if err != nil || len(parsed.Folders) != 3 {
		t.Fatalf("conversion lost restored workspace members: %+v error=%v", parsed, err)
	}
	for i, path := range []string{primary, second, third} {
		if parsed.Folders[i].Path != path {
			t.Fatalf("member %d = %q, want %q in %+v", i, parsed.Folders[i].Path, path, parsed.Folders)
		}
	}
	if len(h.a.cfg.Workspaces[h.a.workspaceID].Folders) != 0 {
		t.Fatalf("converted workspace kept See-private membership: %+v", h.a.cfg.Workspaces[h.a.workspaceID].Folders)
	}
}

func TestWatchedFileEventsReachTheirLanguageServer(t *testing.T) {
	root := writeTree(t, map[string]string{"go.mod": "module example.com/watched\n", "main.go": "package main\n", "main.ts": "const x = 1\n"})
	h := newHarness(t, root)
	h.a.trust()
	if _, err := h.a.lsp.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	tsClient := &fakeClient{enc: lsp.UTF16}
	h.a.tsLSP = langserver.New(langserver.Options{
		Name: "tsrs", Root: root,
		Start: func(context.Context, string, []lsp.WorkspaceFolder, func(error)) (langserver.Client, error) {
			return tsClient, nil
		},
		Language: languages.TypeScriptLanguage,
	})
	if _, err := h.a.tsLSP.Get(context.Background()); err != nil {
		t.Fatal(err)
	}
	h.a.applyEventsAt(root, []mygo.FileEvent{
		{Path: "go.mod", Op: mygo.FileWrite},
		{Path: "main.ts", Op: mygo.FileWrite},
		{Path: "notes.txt", Op: mygo.FileWrite},
	})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (len(h.fc.watchedFiles()) == 0 || len(tsClient.watchedFiles()) == 0) {
		time.Sleep(5 * time.Millisecond)
	}
	goWatched, tsWatched := h.fc.watchedFiles(), tsClient.watchedFiles()
	if len(goWatched) != 1 || !strings.HasSuffix(goWatched[0].URI, "go.mod") {
		t.Fatalf("go watched changes: %+v", goWatched)
	}
	if len(tsWatched) != 1 || !strings.HasSuffix(tsWatched[0].URI, "main.ts") {
		t.Fatalf("typescript watched changes: %+v", tsWatched)
	}
}
