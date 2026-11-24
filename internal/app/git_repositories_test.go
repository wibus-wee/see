package app

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/locales"
)

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
