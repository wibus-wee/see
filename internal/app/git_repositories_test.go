package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/internal/git"
	"see/locales"
)

func nestedGitRepo(t *testing.T, root, name, base, changed string) string {
	t.Helper()
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	gitRun(t, root, "init", "-q", "-b", "main")
	writeFile(t, root, name, base)
	gitRun(t, root, "add", name)
	gitRun(t, root, "commit", "-qm", filepath.Base(root)+" first")
	writeFile(t, root, name, changed)
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	return resolved
}

func TestGitRepositoriesNavigator(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "feature")
	gitRun(t, root, "tag", "v1.0")
	gitRun(t, root, "remote", "add", "origin", "../elsewhere")
	gitRun(t, root, "update-ref", "refs/remotes/origin/release/next", "HEAD")
	h := newHarness(t, root)
	h.readGit()
	h.a.git.autoFetch = false
	for _, label := range []string{locales.MsgGitNavigator(), locales.GitRepositories()} {
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
	frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "frontend.txt", "frontend base\n", "frontend change\n")
	backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "backend.txt", "backend base\n", "backend change\n")

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
	for _, label := range []string{locales.MsgGitNavigator(), locales.GitRepositories()} {
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

func TestDiscoveredRepositoryChangesStayGroupedAndScoped(t *testing.T) {
	workspace := t.TempDir()
	frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "shared.txt", "base\n", "frontend change\n")
	backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "shared.txt", "base\n", "backend change\n")
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	h := newHarnessWithConfig(t, workspace, cfg)
	h.a.cfg.GitAutoDiscover = true
	h.a.refreshGitDiscovery()
	h.settle(func() bool {
		return h.a.git.repositoriesLoaded && len(h.a.git.repositories) == 2 && len(h.a.git.workingCopies) == 2
	})
	h.a.git.branchDiffMode = uncommittedMode
	h.a.refreshGitWorkingCopies()
	h.settle(func() bool {
		for _, root := range []string{frontend, backend} {
			if wc := h.a.git.workingCopies[root]; wc == nil || wc.loading || len(wc.changes) != 1 {
				return false
			}
		}
		return true
	})
	if h.a.git.root != "" || !h.a.showsRepositoryChanges() {
		t.Fatalf("discovered repositories should show together without implicit selection: root=%q repositories=%v", h.a.git.root, h.a.git.repositories)
	}
	h.a.navigator = navGit
	h.tt.Frame()
	for _, label := range []string{"frontend", "backend", "shared.txt"} {
		if !h.tt.HasText(label) {
			t.Fatalf("grouped Changes view is missing %q: %q", label, h.tt.Texts())
		}
	}

	h.a.selectGitRepository(backend)
	h.settle(func() bool { return h.a.git.root == backend && !h.a.git.loading })
	frontCopy, backCopy := h.a.git.workingCopies[frontend], h.a.git.workingCopies[backend]
	frontChange, backChange := frontCopy.changes[0], backCopy.changes[0]
	frontKey := h.a.gitChangeTabKey(frontend, frontChange, false)
	backKey := h.a.gitChangeTabKey(backend, backChange, false)
	if frontKey == backKey {
		t.Fatalf("same relative paths in different repositories share a diff key: %q", frontKey)
	}
	h.a.showGitWorkingCopyDiff(frontend, frontChange, false, h.a.focus, sideNone, len(h.a.focus.order), frontCopy)
	h.a.showGitWorkingCopyDiff(backend, backChange, false, h.a.focus, sideNone, len(h.a.focus.order), backCopy)
	h.settle(func() bool {
		front, back := h.a.focus.diffs[frontKey], h.a.focus.diffs[backKey]
		return front != nil && front.state != nil && back != nil && back.state != nil
	})
	if h.a.focus.diffs[frontKey].root != frontend || h.a.focus.diffs[backKey].root != backend {
		t.Fatalf("duplicate-name diffs lost their owning repository: frontend=%q backend=%q", h.a.focus.diffs[frontKey].root, h.a.focus.diffs[backKey].root)
	}
	frontView, backView := h.a.focus.diffs[frontKey], h.a.focus.diffs[backKey]
	if frontView.partial == nil || backView.partial == nil {
		t.Fatal("initial group diffs should expose partial-stageable hunks")
	}

	h.a.stageGitWorkingCopyChanges(backend, false, []git.Change{backChange})
	h.settle(func() bool {
		return !backCopy.busy && !backCopy.loading && backView.partial == nil && len(backView.state.Hunks()) == 0
	})
	backendState, err := git.Sync(context.Background(), backend, false)
	if err != nil || len(backendState.Changes) != 1 || backendState.Changes[0].Index != 'M' || backendState.Changes[0].Work != ' ' {
		t.Fatalf("staging backend group targeted the wrong change: state=%+v err=%v", backendState.Changes, err)
	}
	frontendState, err := git.Sync(context.Background(), frontend, false)
	if err != nil || len(frontendState.Changes) != 1 || frontendState.Changes[0].Index != ' ' || frontendState.Changes[0].Work != 'M' {
		t.Fatalf("staging backend group changed frontend: state=%+v err=%v", frontendState.Changes, err)
	}
	writeFile(t, backend, "shared.txt", "backend staged plus worktree change\n")
	h.a.refreshGit()
	h.settle(func() bool {
		wc := h.a.git.workingCopies[backend]
		return !wc.loading && !wc.busy && len(wc.changes) == 1 && wc.changes[0].Index == 'M' && wc.changes[0].Work == 'M'
	})
	backendState, err = git.Sync(context.Background(), backend, false)
	if err != nil || len(backendState.Changes) != 1 || backendState.Changes[0].Index != 'M' || backendState.Changes[0].Work != 'M' {
		t.Fatalf("backend staged and worktree changes were not kept distinct: state=%+v err=%v", backendState.Changes, err)
	}
	h.tt.Frame()
	var backendHeader, frontendHeader ui.AccessInfo
	var backendSections, backendRows []ui.AccessInfo
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleButton && access.Name == "backend" {
			backendHeader = access
		}
		if access.Role == ui.RoleButton && access.Name == "frontend" {
			frontendHeader = access
		}
	}
	for _, access := range h.tt.AccessAll() {
		if access.Bounds.Y <= backendHeader.Bounds.Y || access.Bounds.Y >= frontendHeader.Bounds.Y {
			continue
		}
		if access.Role == ui.RoleText && (access.Name == locales.MsgChanges() || access.Name == locales.MsgStagedChanges()) {
			backendSections = append(backendSections, access)
		}
		if access.Role == ui.RoleButton && strings.HasSuffix(access.Name, "shared.txt") {
			backendRows = append(backendRows, access)
		}
	}
	if backendHeader.Bounds.Y >= frontendHeader.Bounds.Y || len(backendSections) != 2 || len(backendRows) != 2 {
		t.Fatalf("backend should show peer Staged Changes and Changes sections, each with a flat file row: headers=%+v/%+v sections=%+v rows=%+v", backendHeader, frontendHeader, backendSections, backendRows)
	}
	stagedKey := h.a.gitChangeTabKey(backend, backendState.Changes[0], true)
	h.a.showGitWorkingCopyDiff(backend, backendState.Changes[0], true, h.a.focus, sideNone, len(h.a.focus.order), backCopy)
	h.settle(func() bool {
		view := h.a.focus.diffs[stagedKey]
		return view != nil && view.state != nil && view.partial != nil
	})
	stagedView := h.a.focus.diffs[stagedKey]
	h.a.stageGitWorkingCopyChanges(backend, true, backendState.Changes)
	h.settle(func() bool {
		return !backCopy.busy && !backCopy.loading && stagedView.partial == nil && len(stagedView.state.Hunks()) == 0 && backView.partial != nil
	})
	backendState, err = git.Sync(context.Background(), backend, false)
	if err != nil || len(backendState.Changes) != 1 || backendState.Changes[0].Index != ' ' || backendState.Changes[0].Work != 'M' {
		t.Fatalf("unstaging backend group changed the wrong repository: state=%+v err=%v", backendState.Changes, err)
	}
	h.a.stageGitWorkingCopyChanges(backend, false, backendState.Changes)
	h.settle(func() bool { return !backCopy.busy && !backCopy.loading })
	h.a.runGitWorkingCopyAction(backend, "commit", "-m", "backend grouped commit")
	h.settle(func() bool { return !backCopy.busy && !backCopy.loading })
	commit, err := git.Command(context.Background(), backend, "log", "-1", "--format=%s")
	if err != nil || strings.TrimSpace(string(commit)) != "backend grouped commit" {
		t.Fatalf("backend group's commit went to the wrong repository: commit=%q err=%v", commit, err)
	}

	h.a.discardGitWorkingCopyChange(frontend, frontChange)
	if h.a.deleteConfirm == nil {
		t.Fatal("discarding a group change did not ask for confirmation")
	}
	h.a.deleteConfirm.actions[0].fn()
	h.settle(func() bool {
		return !frontCopy.busy && !frontCopy.loading && len(frontCopy.changes) == 0 && frontView.partial == nil && len(frontView.state.Hunks()) == 0
	})
	frontendState, err = git.Sync(context.Background(), frontend, false)
	if err != nil || len(frontendState.Changes) != 0 {
		t.Fatalf("discarding frontend group did not target frontend: state=%+v err=%v", frontendState.Changes, err)
	}

	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading && len(h.a.git.history.commits) > 0 })
	h.a.openGitFileIn(frontend, git.Change{Path: "shared.txt"})
	if h.a.git.root != backend {
		t.Fatalf("opening frontend file changed the explicitly selected History repository to %q", h.a.git.root)
	}
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading && len(h.a.git.history.commits) > 0 })
	if subject := h.a.git.history.commits[0].Subject; subject != "backend grouped commit" {
		t.Fatalf("History followed the opened frontend file: latest subject=%q", subject)
	}
}

func TestDiscoveredRepositoryCommitComposersShowForUnstagedChanges(t *testing.T) {
	workspace := t.TempDir()
	frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "frontend.md", "base\n", "frontend edit\n")
	backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "backend.md", "base\n", "backend edit\n")
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	h := newHarnessWithConfig(t, workspace, cfg)
	h.a.cfg.GitAutoDiscover = true
	h.a.refreshGitDiscovery()
	h.settle(func() bool {
		return h.a.git.repositoriesLoaded && len(h.a.git.repositories) == 2 && len(h.a.git.workingCopies) == 2
	})
	h.a.git.branchDiffMode = uncommittedMode
	h.a.refreshGitWorkingCopies()
	h.settle(func() bool {
		for _, root := range []string{frontend, backend} {
			wc := h.a.git.workingCopies[root]
			if wc == nil || wc.loading || len(wc.changes) != 1 || wc.changes[0].Index != ' ' || wc.changes[0].Work != 'M' {
				return false
			}
		}
		return true
	})
	h.a.navigator = navGit
	h.tt.Frame()
	var fields, buttons, headers, sections, fileRows []ui.AccessInfo
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage() {
			if access.Disabled || !access.Focusable {
				t.Fatalf("commit composer should remain editable: %+v", access)
			}
			fields = append(fields, access)
		}
		if access.Role == ui.RoleButton && access.Name == locales.MsgCommit() {
			buttons = append(buttons, access)
		}
		if access.Role == ui.RoleButton && (access.Name == "backend" || access.Name == "frontend") {
			headers = append(headers, access)
		}
		if access.Role == ui.RoleText && access.Name == locales.MsgChanges() {
			sections = append(sections, access)
		}
		if access.Role == ui.RoleButton && (strings.HasSuffix(access.Name, "backend.md") || strings.HasSuffix(access.Name, "frontend.md")) {
			fileRows = append(fileRows, access)
		}
	}
	if len(fields) != 2 || len(buttons) != 2 || len(headers) != 2 || len(sections) != 2 || len(fileRows) != 2 {
		t.Fatalf("unstaged-only groups should show Changes sections between composers and file rows: fields=%+v buttons=%+v headers=%+v sections=%+v file rows=%+v texts=%q", fields, buttons, headers, sections, fileRows, h.tt.Texts())
	}
	byY := func(items []ui.AccessInfo) {
		sort.Slice(items, func(i, j int) bool { return items[i].Bounds.Y < items[j].Bounds.Y })
	}
	byY(headers)
	byY(fields)
	byY(sections)
	byY(fileRows)
	for i := range headers {
		if !(headers[i].Bounds.Y < fields[i].Bounds.Y && fields[i].Bounds.Y < sections[i].Bounds.Y && sections[i].Bounds.Y < fileRows[i].Bounds.Y) {
			t.Fatalf("repository group %d is not ordered header -> composer -> Changes -> files: header=%+v composer=%+v Changes=%+v file=%+v", i, headers[i], fields[i], sections[i], fileRows[i])
		}
		path := []string{"backend.md", "frontend.md"}[i]
		file, ok := h.tt.Find(path)
		if !ok || sections[i].Bounds.X < file.X-1 || sections[i].Bounds.X > file.X+1 {
			t.Fatalf("repository group %d section should align with its file names: Changes=%+v file=%+v found=%v", i, sections[i], file, ok)
		}
	}
	for _, section := range sections {
		countVisible := false
		for _, access := range h.tt.AccessAll() {
			if access.Role == ui.RoleText && access.Name == "1" &&
				access.Bounds.Y <= section.Bounds.Y+section.Bounds.H && access.Bounds.Y+access.Bounds.H >= section.Bounds.Y {
				countVisible = true
				break
			}
		}
		if !countVisible {
			t.Fatalf("repository Changes section is missing its count: %+v", section)
		}
	}
	for _, button := range buttons {
		if !button.Disabled {
			t.Fatalf("commit became enabled without staged changes: %+v", button)
		}
	}
	for i, draft := range []string{"backend draft", "frontend draft"} {
		field := fields[i]
		h.tt.ClickAt(field.Bounds.X+field.Bounds.W/2, field.Bounds.Y+field.Bounds.H/2)
		h.tt.Type(draft)
	}
	h.tt.Frame()
	values := map[string]bool{}
	focused := false
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage() {
			values[access.Value] = true
			focused = focused || access.Focused
		}
		if access.Role == ui.RoleButton && access.Name == locales.MsgCommit() && !access.Disabled {
			t.Fatalf("draft enabled commit without staged changes: %+v", access)
		}
	}
	if !values["frontend draft"] || !values["backend draft"] {
		t.Fatalf("repository commit drafts are not independent: fields=%+v", values)
	}
	if !focused {
		t.Fatal("typing in a repository commit draft did not retain input focus")
	}
	for _, access := range h.tt.AccessAll() {
		if access.Role != ui.RoleTextField || access.Name != locales.MsgCommitMessage() {
			continue
		}
		path := strings.TrimSuffix(access.Value, " draft") + ".md"
		file, ok := h.tt.Find(path)
		if !ok || access.Bounds.X < file.X-1 || access.Bounds.X > file.X+1 {
			t.Fatalf("%q input text should align with %q: input=%+v file=%+v found=%v", access.Value, path, access.Bounds, file, ok)
		}
	}
}

func TestGitWorkingCopyRefreshRejectsReplacedRepositoryState(t *testing.T) {
	root := gitRepo(t, map[string]string{"shared.txt": "base\n"})
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	h := newHarnessWithConfig(t, "", cfg)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.a.wsCtx = ctx
	h.a.git.repositories = []string{root}
	h.a.git.repositoriesLoaded = true
	old := &gitWorkingCopy{changes: []git.Change{{Path: "old snapshot"}}}
	h.a.git.workingCopies = map[string]*gitWorkingCopy{root: old}
	h.a.refreshGitWorkingCopy(root)
	result := queuedGitResult(t, h)
	current := &gitWorkingCopy{changes: []git.Change{{Path: "current snapshot"}}}
	h.a.git.workingCopies[root] = current
	result()
	if got := h.a.git.workingCopies[root].changes[0].Path; got != "current snapshot" {
		t.Fatalf("stale status replaced the current repository snapshot with %q", got)
	}
}

func TestGitRepositorySelectionRejectsStaleAsyncResults(t *testing.T) {
	workspace := t.TempDir()
	frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "frontend.txt", "base\n", "changed\n")
	backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "backend.txt", "base\n", "changed\n")
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

func TestBranchPickerOperationsUseRepositorySelectedAtDispatch(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previousProcs)
	for _, tt := range []struct {
		name, action, branch string
	}{
		{name: "create", action: "create", branch: "created-in-frontend"},
		{name: "switch", action: "switch", branch: "feature"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			workspace := t.TempDir()
			frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "frontend.txt", "base\n", "changed\n")
			backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "backend.txt", "base\n", "changed\n")
			if tt.action == "switch" {
				gitRun(t, frontend, "branch", tt.branch)
				gitRun(t, backend, "branch", tt.branch)
			}
			cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
			cfg.GitAutoDiscover = true
			h := newHarnessWithConfig(t, workspace, cfg)
			h.a.git.repositories = []string{frontend, backend}
			h.a.git.repositoriesLoaded = true
			h.a.git.root = frontend

			if tt.action == "create" {
				h.a.chooseBranch(branchItem{create: tt.branch})
			} else {
				branch := git.Branch{Name: tt.branch}
				h.a.chooseBranch(branchItem{branch: &branch})
			}
			h.a.selectGitRepository(backend)

			if tt.action == "create" {
				h.settle(func() bool {
					if branchExists(backend, tt.branch) {
						t.Fatalf("branch %q was created in the newly selected backend repository", tt.branch)
					}
					return branchExists(frontend, tt.branch)
				})
			} else {
				h.settle(func() bool {
					if currentBranch(backend) == tt.branch {
						t.Fatalf("branch switch ran in the newly selected backend repository")
					}
					return currentBranch(frontend) == tt.branch
				})
			}
			if h.a.git.root != backend {
				t.Fatalf("active Git repository = %q, want backend %q", h.a.git.root, backend)
			}
		})
	}
}

func TestDiscardChangeDoesNotUseBaselineFromAnotherRepository(t *testing.T) {
	workspace := t.TempDir()
	frontend := nestedGitRepo(t, filepath.Join(workspace, "frontend"), "frontend.txt", "frontend base\n", "frontend changed\n")
	backend := nestedGitRepo(t, filepath.Join(workspace, "backend"), "backend.txt", "backend base\n", "backend changed\n")
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	h := newHarnessWithConfig(t, workspace, cfg)
	h.a.git.repositories = []string{frontend, backend}
	h.a.git.repositoriesLoaded = true
	h.a.git.root = backend
	h.a.refreshGit()
	h.settle(func() bool { return !h.a.git.loading && h.a.git.root == backend })
	d := h.open(filepath.Join("backend", "backend.txt"))
	h.a.loadGitBase(d)
	h.settle(func() bool { return d.gitBase != nil && d.gitChanges != nil && len(d.gitChanges.Hunks()) > 0 })
	contents := d.buf.String()
	if contents != "backend changed\n" {
		t.Fatalf("backend document = %q", contents)
	}

	h.a.selectGitRepository(frontend)
	h.settle(func() bool { return !h.a.git.loading && h.a.git.root == frontend })
	h.a.userOpen(d.path)
	if h.a.git.root != frontend {
		t.Fatalf("opening the backend file changed the selected repository to %q", h.a.git.root)
	}
	canDiscard := h.a.canDiscardChange()
	h.a.discardChangeAt(d, h.a.focus.eds[d.path], 0)
	if d.buf.String() != contents {
		t.Fatalf("discard applied another repository's baseline: got %q, want %q", d.buf.String(), contents)
	}
	if canDiscard || d.gitBase != nil || d.gitChanges != nil || h.a.focus.eds[d.path].DiffBase != nil {
		t.Fatalf("out-of-scope Git baseline remains available: canDiscard=%v base=%v changes=%v DiffBase=%v", canDiscard, d.gitBase, d.gitChanges, h.a.focus.eds[d.path].DiffBase)
	}
}

func TestGitAutoDiscoverSettingRefreshesEveryOpenWindow(t *testing.T) {
	root := gitRepo(t, map[string]string{"root.txt": "base\n"})
	child := nestedGitRepo(t, filepath.Join(root, "child"), "child.txt", "base\n", "changed\n")
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	first := newHarnessWithConfig(t, root, cfg)
	second := newHarnessWithConfig(t, root, cfg)
	apps := []*App{first.a, second.a}
	realRoot := first.a.realRoot
	for _, a := range apps {
		a.git.repositories = []string{root, child}
		a.git.repositoriesLoaded = true
		a.git.root = child
	}
	refreshAll := func() {
		for _, a := range apps {
			a.refreshGitDiscovery()
		}
	}
	first.a.gitDiscoveryChanged = refreshAll
	second.a.gitDiscoveryChanged = refreshAll
	cfg.GitAutoDiscover = false
	first.a.gitAutoDiscoverSettingChanged()
	first.settle(func() bool { return !first.a.git.loading && first.a.git.root == realRoot })
	second.settle(func() bool { return !second.a.git.loading && second.a.git.root == realRoot })
	for i, a := range apps {
		if a.git.root != realRoot || len(a.git.repositories) != 0 || a.git.repositoriesLoaded {
			t.Fatalf("window %d did not reset to its workspace repository: root=%q repositories=%v loaded=%v", i, a.git.root, a.git.repositories, a.git.repositoriesLoaded)
		}
	}
}

func TestAutoDiscoveryPreservesRestoredContainingRepositoryGitTab(t *testing.T) {
	root := gitRepo(t, map[string]string{"changed.txt": "base\n"})
	writeFile(t, root, "changed.txt", "changed\n")
	key := diffTab("changed.txt", false)
	cfg := config.Load(filepath.Join(t.TempDir(), "settings.json"))
	cfg.GitAutoDiscover = true
	cfg.Workspaces = map[string]config.Workspace{
		root: {Layout: &config.Layout{
			Tabs:   []config.Tab{{Diff: &config.DiffTab{Path: "changed.txt"}}},
			Active: key,
		}},
	}
	h := newHarnessWithConfig(t, root, cfg)
	g := h.a.focus
	v := g.diffs[key]
	if v == nil || !v.pending {
		t.Fatalf("restored Git placeholder is missing: %+v", v)
	}

	h.a.scanGitRepositories()
	queuedGitResult(t, h)()
	if h.a.git.root != h.a.realRoot {
		t.Fatalf("initial discovery selected %q, want containing repository %q", h.a.git.root, h.a.realRoot)
	}
	if g.diffs[key] != v || !g.has(key) {
		t.Fatal("initial repository activation closed the restorable Git tab")
	}
	h.settle(func() bool { return !v.pending && v.state != nil })
}

func branchExists(root, name string) bool {
	_, err := git.Command(context.Background(), root, "show-ref", "--verify", "--quiet", "refs/heads/"+name)
	return err == nil
}

func currentBranch(root string) string {
	out, err := git.Command(context.Background(), root, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
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
	for _, label := range []string{locales.MsgGitNavigator(), locales.GitRepositories(), "feature"} {
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
