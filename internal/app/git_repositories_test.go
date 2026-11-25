package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/internal/git"
	"see/locales"
)

func nestedGitRepo(t *testing.T, root, name, base, changed string) {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q", "-b", "main")
	writeFile(t, root, name, base)
	gitRun(t, root, "add", name)
	gitRun(t, root, "commit", "-qm", filepath.Base(root)+" first")
	writeFile(t, root, name, changed)
}

func TestGitRepositoriesNavigator(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "feature")
	gitRun(t, root, "tag", "v1.0")
	gitRun(t, root, "remote", "add", "origin", "../elsewhere")
	gitRun(t, root, "update-ref", "refs/remotes/origin/release/next", "HEAD")
	h := newHarness(t, root)
	h.readGit()
	h.a.git.autoFetch = false
	for _, label := range []string{"Show the Git Navigator", locales.GitRepositories()} {
		if err := h.tt.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
	}
	for _, label := range []string{locales.GitRepositoryBranches(), locales.GitRepositoryTags(), locales.GitRepositoryRemotes(), "origin", "release/next", "v1.0"} {
		if !h.tt.HasText(label) {
			t.Errorf("repository tree is missing %q", label)
		}
	}
	if err := h.tt.Click("origin"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.tt.HasText("release/next") {
		t.Fatal("remote branch is still shown after collapsing origin")
	}
	if err := h.tt.RightClick("feature"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(locales.GitSwitchToBranch()); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.git.repository.Current == "feature" })
	if err := h.tt.RightClick(locales.GitCurrentBranch("feature")); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ChooseMenuItem(locales.GitSwitchToBranch()); err == nil || !strings.Contains(err.Error(), "disabled") {
		t.Fatalf("switching to the current branch should be disabled, got %v", err)
	}
}

func TestGitRepositoriesTabsUseKeyboardAndSelectedState(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"})
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	if _, ok := h.tt.AccessByRole(ui.RoleTabList, ""); !ok {
		t.Fatal("Git navigator tabs are not exposed as a tab list")
	}
	working, ok := h.tt.AccessByRole(ui.RoleTab, locales.GitRepositoryChanges())
	if !ok || !working.Selected {
		t.Fatalf("Working Copy is not exposed as the selected tab: %+v (%v)", working, ok)
	}
	if err := h.tt.Click(locales.GitRepositoryChanges()); err != nil {
		t.Fatal(err)
	}
	h.tt.Key(0, ui.KeyRight)
	h.tt.Frame()
	repositories, ok := h.tt.AccessByRole(ui.RoleTab, locales.GitRepositories())
	if h.a.git.repositoryView.page != 1 || !ok || !repositories.Selected {
		t.Fatalf("Right did not select Repositories through tab navigation: page %d, tab %+v (%v)", h.a.git.repositoryView.page, repositories, ok)
	}
}

func TestChildGitRepositoryDiscoveryIsOptInAndSelectionStaysExplicit(t *testing.T) {
	workspace := t.TempDir()
	frontend := filepath.Join(workspace, "frontend")
	backend := filepath.Join(workspace, "backend")
	nestedGitRepo(t, frontend, "frontend.txt", "frontend base\n", "frontend change\n")
	nestedGitRepo(t, backend, "backend.txt", "backend base\n", "backend change\n")

	h := newHarnessWithConfig(t, workspace, &config.Config{ConfirmClose: true})
	h.readGit()
	if h.a.git.root != "" || len(h.a.git.repositories) != 0 {
		t.Fatalf("discovery changed the default-off Git scope: root=%q repositories=%v", h.a.git.root, h.a.git.repositories)
	}

	h.a.cfg.GitAutoDiscover = true
	h.a.refreshGitDiscovery()
	h.settle(func() bool { return h.a.git.repositoriesLoaded })
	if len(h.a.git.repositories) != 2 || h.a.git.root != "" {
		t.Fatalf("discovery should list children without selecting one: root=%q repositories=%v", h.a.git.root, h.a.git.repositories)
	}
	for _, label := range []string{"Show the Git Navigator", locales.GitRepositories()} {
		if err := h.tt.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
	}
	if err := h.tt.Click("frontend"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.git.root == frontend && !h.a.git.loading })
	if len(h.a.git.changes) != 1 || h.a.git.changes[0].Path != "frontend.txt" {
		t.Fatalf("frontend changes = %+v", h.a.git.changes)
	}
	h.a.openFile(filepath.Join(backend, "backend.txt"), nil)
	if h.a.root != workspace || h.a.git.root != frontend {
		t.Fatalf("opening a backend file changed the workspace or Git scope: workspace=%q Git=%q", h.a.root, h.a.git.root)
	}
	if err := h.tt.Click("backend"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return h.a.git.root == backend && !h.a.git.loading })
	if len(h.a.git.changes) != 1 || h.a.git.changes[0].Path != "backend.txt" {
		t.Fatalf("backend changes = %+v", h.a.git.changes)
	}
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading && len(h.a.git.history.commits) > 0 })
	if got := h.a.git.history.commits[0].Subject; got != "backend first" {
		t.Fatalf("history commit = %q, want backend repository history", got)
	}
	change := h.a.git.changes[0]
	h.a.showGitDiff(change, false)
	h.settle(func() bool {
		v := h.a.focus.diffs[diffTab(change.Path, false)]
		return v != nil && v.state != nil
	})
}

func TestGitRepositorySelectionRejectsStaleAsyncResults(t *testing.T) {
	workspace := t.TempDir()
	frontend := filepath.Join(workspace, "frontend")
	backend := filepath.Join(workspace, "backend")
	nestedGitRepo(t, frontend, "frontend.txt", "base\n", "changed\n")
	nestedGitRepo(t, backend, "backend.txt", "base\n", "changed\n")
	h := newHarnessWithConfig(t, "", &config.Config{ConfirmClose: true})
	h.a.cfg.GitAutoDiscover = true
	h.a.root = workspace
	h.a.wsCtx, h.a.wsCancel = context.WithCancel(context.Background())
	defer h.a.wsCancel()
	h.a.scanGitRepositories()
	workspaceResult := queuedGitResult(t, h)
	h.a.gen++
	h.a.git = gitState{}
	workspaceResult()
	if len(h.a.git.repositories) != 0 || h.a.git.repositoriesLoaded {
		t.Fatalf("stale workspace discovery was published: %+v", h.a.git.repositories)
	}
	h.a.git.repositories = []string{frontend, backend}
	h.a.git.repositoriesLoaded = true
	h.a.git.root = frontend

	h.a.scanGitRepositories()
	scanResult := queuedGitResult(t, h)
	h.a.selectGitRepository(backend)
	scanResult()
	if h.a.git.root != backend || len(h.a.git.repositories) != 2 {
		t.Fatalf("stale discovery changed the selected repository: root=%q repositories=%v", h.a.git.root, h.a.git.repositories)
	}

	statusResult := queuedGitResult(t, h)
	h.a.selectGitRepository(frontend)
	statusResult()
	if h.a.git.root != frontend || len(h.a.git.changes) != 0 {
		t.Fatalf("stale status changed the selected repository: root=%q changes=%+v", h.a.git.root, h.a.git.changes)
	}
	h.settle(func() bool { return !h.a.git.loading })
}

func queuedGitResult(t *testing.T, h *harness) func() {
	t.Helper()
	select {
	case fn := <-h.q:
		return fn
	case <-time.After(5 * time.Second):
		t.Fatal("Git worker did not publish a result")
		return nil
	}
}

func TestGitLogTagAndCherryPickActions(t *testing.T) {
	root := gitRepo(t, map[string]string{"base.txt": "base\n"})
	gitRun(t, root, "switch", "-qc", "feature")
	writeFile(t, root, "port.txt", "ported\n")
	gitRun(t, root, "add", "port.txt")
	gitRun(t, root, "commit", "-qm", "Port feature")
	ctx := context.Background()
	feature := git.Resolve(ctx, root, "HEAD")
	gitRun(t, root, "switch", "-q", "main")
	gitRun(t, root, "commit", "--allow-empty", "-qm", "Advance main")
	h := newHarness(t, root)
	h.readGit()
	for _, label := range []string{"Show the Git Navigator", locales.GitRepositories(), "feature"} {
		if err := h.tt.Click(label); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
	}
	h.settleFrames(func() bool { return h.tt.HasText("Port feature") })
	choose := func(label string) {
		t.Helper()
		if err := h.tt.RightClick("Port feature"); err != nil {
			t.Fatal(err)
		}
		if err := h.tt.ChooseMenuItem(label); err != nil {
			t.Fatal(err)
		}
		h.tt.Frame()
	}
	choose(locales.GitCreateTagAction())
	if !h.tt.HasText(locales.GitTagMessage()) {
		t.Fatal("tag dialog did not open")
	}
	h.tt.Type("feature-v1")
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool { return len(h.a.git.repository.Tags) == 1 && !h.a.git.busy })
	if h.a.git.repositoryView.tag != nil || !h.tt.HasText("feature-v1") || git.Resolve(ctx, root, "refs/tags/feature-v1") != feature {
		t.Fatal("tag was not created on the chosen commit and reflected in the tree")
	}
	choose(locales.GitCherryPick())
	h.settle(func() bool { return strings.HasPrefix(h.a.notice, "Cherry-picked ") || h.a.noticeError })
	head := git.Resolve(ctx, root, "HEAD")
	if h.a.noticeError || head == feature || !strings.HasPrefix(h.a.notice, "Cherry-picked ") {
		t.Fatalf("cherry-pick: %q (HEAD %q, source %q)", h.a.notice, head, feature)
	}
	h.settleFrames(func() bool {
		return h.a.focus.diffs[gitLogTab].log.selected == head
	})
}
