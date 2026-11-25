package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/config"
	"see/internal/git"
	"see/locales"
)

// gitRepo is a repository of files with one commit on main, and the
// branches named.
func gitRepo(t *testing.T, files map[string]string, branches ...string) string {
	t.Helper()
	pinGitEnv(t)
	root := writeTree(t, files)
	for _, args := range append([][]string{
		{"init", "-q", "-b", "main"},
		{"add", "."},
		{"commit", "-qm", "first"},
	}, func() (out [][]string) {
		for _, b := range branches {
			out = append(out, []string{"branch", b})
		}
		return
	}()...) {
		args = append([]string{"-c", "user.name=See", "-c", "user.email=see@example.com", "-c", "commit.gpgsign=false"}, args...)
		if _, err := git.Command(context.Background(), root, args...); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func (h *harness) readGit() {
	h.t.Helper()
	h.a.refreshGit()
	h.settle(func() bool { return !h.a.git.loading && !h.a.git.busy })
}

func TestGitRangeSelectionAndStageAll(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n", "b.txt": "b\n", "c.txt": "c\n"})
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		writeFile(t, root, name, "changed\n")
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.historyClosed = true
	h.tt.Frame()
	if err := h.tt.Click("Unstaged diff: a.txt"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.ClickWith(ui.Shift, "Unstaged diff: c.txt"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if !h.a.git.selected[gitChangeKey(false, name)] {
			t.Fatalf("%s not selected: %v", name, h.a.git.selected)
		}
	}
	h.readGit()
	if len(h.a.git.selected) != 3 {
		t.Fatalf("refresh lost selection: %v", h.a.git.selected)
	}
	if err := h.tt.Click("Stage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	for _, ch := range h.a.git.changes {
		if ch.Index == ' ' || ch.Work != ' ' {
			t.Fatalf("not staged: %+v", ch)
		}
	}
	h.tt.Frame()
	if err := h.tt.Click("Unstage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	for _, ch := range h.a.git.changes {
		if ch.Index != ' ' || ch.Work == ' ' {
			t.Fatalf("not unstaged: %+v", ch)
		}
	}
}

func TestGitChangeRowsAreFlatAcrossFileKinds(t *testing.T) {
	names := []string{"main.go", "main_test.go", "README.md"}
	root := gitRepo(t, map[string]string{"main.go": "package main\n", "main_test.go": "package main\n", "README.md": "docs\n"})
	for _, name := range names {
		writeFile(t, root, name, "changed\n")
	}
	gitRun(t, root, "add", "main.go")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.historyClosed = true
	h.tt.Frame()
	if !h.tt.HasText("Staged Changes") || !h.tt.HasText("Changes") {
		t.Fatalf("status sections are missing: %q", h.tt.Texts())
	}
	for _, name := range names {
		label := "Unstaged diff: " + name
		if name == "main.go" {
			label = "Staged diff: " + name
		}
		if !h.tt.HasText(label) {
			t.Fatalf("flat status list is missing %s: %q", label, h.tt.Texts())
		}
	}
	for _, group := range []string{logText("gitKindTests"), logText("gitKindDocs"), logText("gitKindGenerated")} {
		if h.tt.HasText(group) {
			t.Fatalf("semantic file group %q remains in %q", group, h.tt.Texts())
		}
	}
	if err := h.tt.Click("Stage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	h.tt.Frame()
	for _, name := range names {
		if !h.tt.HasText("Staged diff: " + name) {
			t.Fatalf("flat staged list is missing %s: %q", name, h.tt.Texts())
		}
	}
}

func filenameBounds(t *testing.T, tt *ui.Tester, path string) ui.Rect {
	t.Helper()
	nameBounds, ok := tt.Find(filepath.Base(path))
	if !ok || nameBounds.W <= 0 {
		t.Fatalf("filename %q is not visible: %q", filepath.Base(path), tt.Texts())
	}
	return nameBounds
}

func assertBasenameUsesAvailableWidth(t *testing.T, tt *ui.Tester, path, rowLabel string, naturalWidth, availableWidth float32) {
	t.Helper()
	nameBounds := filenameBounds(t, tt, path)
	if naturalWidth <= availableWidth+1 {
		if nameBounds.W < naturalWidth-1 || nameBounds.W > naturalWidth+1 {
			t.Fatalf("basename %q shrank despite fitting in its text area: width %.1f, natural %.1f, available %.1f", filepath.Base(path), nameBounds.W, naturalWidth, availableWidth)
		}
	} else if nameBounds.W < availableWidth-1 || nameBounds.W > availableWidth+1 {
		t.Fatalf("overlong basename %q did not use only its bounded text area: width %.1f, available %.1f", filepath.Base(path), nameBounds.W, availableWidth)
	}
	accessible := false
	for _, access := range tt.AccessAll() {
		if strings.Contains(access.Name, path) {
			accessible = true
			break
		}
	}
	if !accessible {
		t.Fatalf("full path %q is missing from accessible names: %+v", path, tt.AccessAll())
	}
	if rowLabel != "" {
		rowBounds, ok := tt.Find(rowLabel)
		if !ok || nameBounds.X+nameBounds.W > rowBounds.X+rowBounds.W+1 {
			t.Fatalf("basename %q exceeds its row bounds: name %v, row %v", filepath.Base(path), nameBounds, rowBounds)
		}
	}
}

func directoryBounds(t *testing.T, tt *ui.Tester, path string) ui.Rect {
	t.Helper()
	dir := filepath.ToSlash(filepath.Dir(path))
	if dir == "." {
		t.Fatalf("%q has no directory", path)
	}
	pathBounds, ok := tt.Find("\u00a0" + dir)
	if !ok {
		t.Fatalf("directory %q is not visible: %q", dir, tt.Texts())
	}
	return pathBounds
}

func gitTextAreaWidth(t *testing.T, tt *ui.Tester, path string) float32 {
	t.Helper()
	rowBounds, ok := tt.Find("Unstaged diff: " + path)
	if !ok {
		t.Fatalf("Git row for %q is not visible: %q", path, tt.Texts())
	}
	nameBounds := filenameBounds(t, tt, path)
	return rowBounds.X + rowBounds.W - nameBounds.X
}

func branchDiffTextAreaWidth(t *testing.T, tt *ui.Tester, path, firstStatus string) float32 {
	t.Helper()
	nameBounds := filenameBounds(t, tt, path)
	statusBounds, ok := tt.Find(firstStatus)
	if !ok {
		t.Fatalf("comparison status %q is not visible: %q", firstStatus, tt.Texts())
	}
	return statusBounds.X - nameBounds.X - gitSidebarColumnGap
}

func TestGitChangeRowsPrioritizeBasenamesOverPaths(t *testing.T) {
	const nestedPath = "packages/web/src/features/conversations/components/MessageHistoryPanel.tsx"
	const longBasename = "ExtremelyLongGeneratedStateMachineSidebarAvailabilityControllerConfiguration.tsx"
	paths := []string{nestedPath, longBasename}
	root := gitRepo(t, map[string]string{nestedPath: "base\n", longBasename: "base\n"})
	for _, path := range paths {
		writeFile(t, root, path, "changed\n")
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.a.sidebarW = 800
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	naturalWidths := map[string]float32{}
	for _, path := range paths {
		naturalWidths[path] = filenameBounds(t, h.tt, path).W
	}
	naturalPathWidth := directoryBounds(t, h.tt, nestedPath).W
	for _, width := range []float32{420, gitSidebarMinimumWidth} {
		h.a.sidebarW = width
		h.tt.Frame()
		for _, path := range paths {
			assertBasenameUsesAvailableWidth(t, h.tt, path, "Unstaged diff: "+path, naturalWidths[path], gitTextAreaWidth(t, h.tt, path))
		}
		if width == 420 && directoryBounds(t, h.tt, nestedPath).W >= naturalPathWidth-1 {
			t.Fatalf("directory path did not give space back to the basename at normal width: natural %.1f, current %.1f", naturalPathWidth, directoryBounds(t, h.tt, nestedPath).W)
		}
	}
}

// The branch in the toolbar opens the branch picker; choosing a branch
// switches to it, and typing a new name makes one.
func TestBranchPickerSwitchesAndCreates(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	if h.a.git.branch != "main" {
		t.Fatalf("branch %q", h.a.git.branch)
	}
	if err := h.tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	if !h.tt.HasText("feature") {
		t.Fatalf("the picker shows %q", h.tt.Texts())
	}
	h.tt.Type("feat")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	h.readGit()
	if h.a.git.branch != "feature" || h.a.git.picker.open {
		t.Fatalf("on %q, picker open %v, notice %q", h.a.git.branch, h.a.git.picker.open, h.a.notice)
	}

	h.a.openBranchPicker(h.a.git.picker.anchor)
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Type("topic/new")
	h.tt.Frame()
	if !h.tt.HasText("Create Branch “topic/new”") {
		t.Fatalf("no create row in %q", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.busy })
	h.readGit()
	if h.a.git.branch != "topic/new" {
		t.Fatalf("on %q after creating, notice %q", h.a.git.branch, h.a.notice)
	}
}

// pinGitEnv keeps GIT_AUTHOR_* and GIT_COMMITTER_* exported by the
// caller's shell from overriding the -c identity flags the fixtures
// pass, and unsets GIT_DIR and GIT_WORK_TREE, which git cannot empty.
func pinGitEnv(t *testing.T) {
	t.Helper()
	for _, kv := range [][2]string{
		{"GIT_AUTHOR_NAME", "See"}, {"GIT_AUTHOR_EMAIL", "see@example.com"},
		{"GIT_COMMITTER_NAME", "See"}, {"GIT_COMMITTER_EMAIL", "see@example.com"},
	} {
		t.Setenv(kv[0], kv[1])
	}
	// GIT_DIR and GIT_WORK_TREE would send commands to another
	// repository, and GIT_CONFIG_KEY_*/GIT_CONFIG_VALUE_* would inject
	// config the fixture did not write; git cannot empty them, so unset
	// and restore.
	for _, v := range []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_CONFIG_COUNT"} {
		if old, ok := os.LookupEnv(v); ok {
			t.Cleanup(func() { os.Setenv(v, old) })
		}
		os.Unsetenv(v)
	}
	for _, v := range os.Environ() {
		if name, _, ok := strings.Cut(v, "="); ok && (strings.HasPrefix(name, "GIT_CONFIG_KEY_") || strings.HasPrefix(name, "GIT_CONFIG_VALUE_")) {
			if old, ok := os.LookupEnv(name); ok {
				t.Cleanup(func() { os.Setenv(name, old) })
			}
			os.Unsetenv(name)
		}
	}
}

// gitRun runs git in root as a test's committer.
func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
	pinGitEnv(t)
	args = append([]string{"-c", "user.name=See", "-c", "user.email=see@example.com", "-c", "commit.gpgsign=false"}, args...)
	if _, err := git.Command(context.Background(), root, args...); err != nil {
		t.Fatal(err)
	}
}

// historyRepo has a branch merged into main, and a change not committed.
func historyRepo(t *testing.T) string {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	gitRun(t, root, "switch", "-qc", "side")
	writeFile(t, root, "b.go", "package a\n\nfunc B() {}\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "Add B on the side")
	gitRun(t, root, "switch", "-q", "main")
	writeFile(t, root, "a.go", "package a\n\nfunc A() {}\n")
	gitRun(t, root, "commit", "-qam", "Add A")
	gitRun(t, root, "merge", "-q", "--no-ff", "-m", "Merge side", "side")
	writeFile(t, root, "a.go", "package a\n\nfunc A() { return }\n")
	return root
}

func writeFile(t *testing.T, root, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The Git navigator shows the changes over the history, not one or the
// other; each folds away on its own without adding a duplicate changes row.
func TestGitNavigatorShowsChangesOverHistory(t *testing.T) {
	h := newHarness(t, historyRepo(t)) // a.go is changed
	h.readGit()
	h.a.navigator = navGit
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })
	for _, s := range []string{"Changes", "History", "a.go", "Add A"} {
		if !h.tt.HasText(s) {
			t.Fatalf("the navigator shows no %q: %q", s, h.tt.Texts())
		}
	}
	if h.tt.HasText("Uncommitted Changes") {
		t.Fatal("the uncommitted changes stand over the history beside their own section")
	}
	if err := h.tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.a.git.changesClosed || h.tt.HasText("a.go") || h.tt.HasText("Uncommitted Changes") || !h.tt.HasText("Add A") {
		t.Fatalf("folded changes: %q", h.tt.Texts())
	}
	if err := h.tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.a.git.historyClosed || h.tt.HasText("Add A") {
		t.Fatalf("folded history: %q", h.tt.Texts())
	}
}

func TestGitNavigatorStashesCollapseIndependently(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	writeFile(t, root, "a.go", "package a\n// saved\n")
	gitRun(t, root, "stash", "push", "-q", "-m", "saved work")
	writeFile(t, root, "a.go", "package a\n// current\n")

	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })

	if !h.tt.HasText("Stash: saved work") || !h.tt.HasText("a.go") {
		t.Fatalf("initial Git sections: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Stashed Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.tt.HasText("Stash: saved work") || !h.tt.HasText("a.go") {
		t.Fatalf("folding stashes affected changes: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.tt.HasText("a.go") || !h.tt.HasText("Stashed Changes") || !h.tt.HasText("History") {
		t.Fatalf("folding changes affected secondary sections: %q", h.tt.Texts())
	}
	if err := h.tt.Click("Stashed Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if !h.tt.HasText("Stash: saved work") || h.tt.HasText("a.go") {
		t.Fatalf("unfolded stashes alongside folded changes: %q", h.tt.Texts())
	}
}

func TestGitCommitFooterStaysOutsideTreeScrollAndRetainsDraft(t *testing.T) {
	root := gitRepo(t, map[string]string{"saved.txt": "saved\n"})
	writeFile(t, root, "saved.txt", "saved stash\n")
	gitRun(t, root, "stash", "push", "-q", "-m", "saved work")
	for i := 0; i < 30; i++ {
		writeFile(t, root, "change-"+strconv.Itoa(i)+".go", "package changes\n")
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.tt.SetSize(harnessWidth, 700)
	h.tt.Frame()

	commitField := func() (ui.AccessInfo, bool) {
		for _, access := range h.tt.AccessAll() {
			if access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage() {
				return access, true
			}
		}
		return ui.AccessInfo{}, false
	}
	if _, ok := commitField(); ok {
		t.Fatal("empty commit field shown without staged changes or a draft")
	}
	if err := h.tt.Click("Stage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	file, fileOK := h.tt.Find("Staged diff: change-0.go")
	commit, commitOK := commitField()
	commitAction, actionOK := h.tt.AccessByRole(ui.RoleButton, locales.MsgCommit())
	fieldCenter := commit.Bounds.Y + commit.Bounds.H/2
	actionCenter := commitAction.Bounds.Y + commitAction.Bounds.H/2
	if !fileOK || !commitOK || commit.Bounds.Y <= file.Y || !actionOK || commitAction.Bounds.W != 22 || commitAction.Bounds.H != 22 ||
		actionCenter < fieldCenter-1 || actionCenter > fieldCenter+1 {
		t.Fatalf("commit footer or inline action is not visible after staging: file %v (%v), commit %v (%v), action %v (%v), texts %q", file, fileOK, commit, commitOK, commitAction, actionOK, h.tt.Texts())
	}
	if err := h.tt.Click(locales.MsgCommitMessage()); err != nil {
		t.Fatal(err)
	}
	h.tt.Type("draft")
	h.tt.Frame()
	if h.a.git.message != "draft" {
		t.Fatalf("commit draft was not retained: %q", h.a.git.message)
	}
	if err := h.tt.Click("Unstage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	if _, ok := commitField(); !ok {
		t.Fatal("commit draft field disappeared when changes were unstaged")
	}
	commitBefore, _ := commitField()
	historyBefore, historyOK := h.tt.Find("History")
	file, fileOK = h.tt.Find("Unstaged diff: change-0.go")
	if !historyOK || !fileOK {
		t.Fatalf("scroll anchors missing: History %v (%v), file %v (%v), texts %q", historyBefore, historyOK, file, fileOK, h.tt.Texts())
	}
	x, y := file.X+file.W/2, file.Y+file.H/2
	for i := 0; i < 32 && !h.tt.HasText("Stash: saved work"); i++ {
		h.tt.Wheel(x, y, 0, 120)
		h.tt.Advance(150 * time.Millisecond)
	}
	if !h.tt.HasText("Stash: saved work") {
		t.Fatalf("stash row did not enter the tree viewport: %q", h.tt.Texts())
	}
	commitAfter, commitOK := commitField()
	historyAfter, historyOK := h.tt.Find("History")
	if !commitOK || commitAfter.Bounds != commitBefore.Bounds || !historyOK || historyAfter.Y != historyBefore.Y {
		t.Fatalf("scrolling the file/stash tree moved the footer or History: commit %v -> %v, History %v -> %v", commitBefore, commitAfter, historyBefore, historyAfter)
	}
}

func TestGitConflictHidesFooterUntilResolvedWithStashAndHistory(t *testing.T) {
	root := gitRepo(t, map[string]string{"settings.go": "base\n", "saved.txt": "saved\n"}, "feature")
	writeFile(t, root, "saved.txt", "stashed\n")
	gitRun(t, root, "stash", "push", "-q", "-m", "conflict scenario stash")
	gitRun(t, root, "switch", "-q", "feature")
	writeFile(t, root, "settings.go", "incoming\n")
	gitRun(t, root, "add", "settings.go")
	gitRun(t, root, "commit", "-qm", "feature change")
	gitRun(t, root, "switch", "-q", "main")
	writeFile(t, root, "settings.go", "current\n")
	gitRun(t, root, "add", "settings.go")
	gitRun(t, root, "commit", "-qm", "main change")
	if _, err := git.Command(context.Background(), root, "merge", "feature"); err == nil {
		t.Fatal("expected a merge conflict")
	}
	for i := 0; i < 28; i++ {
		writeFile(t, root, "work-"+strconv.Itoa(i)+".go", "package work\n")
	}

	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	const draft = "merge draft"
	h.a.git.message = draft
	h.tt.SetSize(harnessWidth, 900)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })

	reason := gitConflictText("gitResolveBeforeCommit")
	if !h.tt.HasText("Conflicted Files") || len(h.a.git.stashes) != 1 {
		t.Fatalf("conflicts or stash missing: %q, stashes %+v", h.tt.Texts(), h.a.git.stashes)
	}
	var field, commitButton, conflictStatus, treeScroll, historyDivider ui.AccessInfo
	var fieldOK, buttonOK, statusOK, treeScrollOK, dividerOK bool
	for _, access := range h.tt.AccessAll() {
		switch {
		case access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage():
			field, fieldOK = access, true
		case access.Role == ui.RoleButton && access.Name == locales.MsgCommit():
			commitButton, buttonOK = access, true
		case access.Role == ui.RoleText && access.Name == reason:
			conflictStatus, statusOK = access, true
		case access.Role == ui.RoleScroll && access.Bounds.H > 100 && !treeScrollOK:
			treeScroll, treeScrollOK = access, true
		case access.Role == ui.RoleSplitter && access.Bounds.W > 100 && access.Bounds.H <= 2:
			historyDivider, dividerOK = access, true
		}
	}
	gap := float32(0)
	if treeScrollOK && dividerOK {
		gap = historyDivider.Bounds.Y - (treeScroll.Bounds.Y + treeScroll.Bounds.H)
	}
	if fieldOK || buttonOK || statusOK || !treeScrollOK || !dividerOK || gap > 3 || h.a.git.message != draft {
		t.Fatalf("conflict should hide the composer without reserving footer space and retain the draft: field %+v (%v), Commit %+v (%v), status %+v (%v), tree %v (%v), History divider %v (%v), gap %v, draft %q, texts %q", field, fieldOK, commitButton, buttonOK, conflictStatus, statusOK, treeScroll.Bounds, treeScrollOK, historyDivider.Bounds, dividerOK, gap, h.a.git.message, h.tt.Texts())
	}
	historyBefore, historyOK := h.tt.Find("History")
	file, fileOK := h.tt.Find("Unstaged diff: work-0.go")
	if !historyOK || !fileOK {
		t.Fatalf("conflict scroll anchors missing: History %v (%v), file %v (%v)", historyBefore, historyOK, file, fileOK)
	}
	x, y := file.X+file.W/2, file.Y+file.H/2
	for i := 0; i < 32 && !h.tt.HasText("Stash: conflict scenario stash"); i++ {
		h.tt.Wheel(x, y, 0, 120)
		h.tt.Advance(150 * time.Millisecond)
	}
	if !h.tt.HasText("Stash: conflict scenario stash") {
		t.Fatalf("stash row did not enter the conflict tree viewport: %q", h.tt.Texts())
	}
	var fieldAfter ui.AccessInfo
	fieldAfterOK := false
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage() {
			fieldAfter = access
			fieldAfterOK = true
			break
		}
	}
	historyAfter, historyOK := h.tt.Find("History")
	var treeScrollAfter ui.AccessInfo
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleScroll && access.Bounds.H > 100 {
			treeScrollAfter = access
			break
		}
	}
	if fieldAfterOK || !historyOK || historyAfter.Y != historyBefore.Y || treeScrollAfter.Bounds != treeScroll.Bounds || h.a.git.message != draft {
		t.Fatalf("conflict tree scrolling changed the absent composer, tree viewport, History, or draft: field %v (%v), tree %v -> %v, History %v -> %v, draft %q", fieldAfter, fieldAfterOK, treeScroll.Bounds, treeScrollAfter.Bounds, historyBefore, historyAfter, h.a.git.message)
	}

	writeFile(t, root, "settings.go", "resolved\n")
	gitRun(t, root, "add", "settings.go")
	h.readGit()
	h.settle(func() bool { return !h.a.git.loading && !h.a.git.busy })
	field, fieldOK = h.tt.AccessByRole(ui.RoleTextField, locales.MsgCommitMessage())
	commitButton, buttonOK = h.tt.AccessByRole(ui.RoleButton, locales.MsgCommit())
	if !h.a.git.merging || !fieldOK || field.Value != draft || !buttonOK || commitButton.Disabled || h.a.git.message != draft {
		t.Fatalf("composer did not restore with its draft after conflict resolution: merging %v, field %+v (%v), Commit %+v (%v), draft %q", h.a.git.merging, field, fieldOK, commitButton, buttonOK, h.a.git.message)
	}
	previousHead := h.a.git.head
	if err := h.tt.Click(locales.MsgCommit()); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.loading && !h.a.git.busy })
	if h.a.git.merging || h.a.git.head == previousHead {
		t.Fatalf("restored composer could not create the merge commit: merging %v, HEAD %q -> %q, notice %q", h.a.git.merging, previousHead, h.a.git.head, h.a.notice)
	}
}

func gitSidebarDivider(t *testing.T, h *harness) ui.AccessInfo {
	t.Helper()
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleSplitter && access.Bounds.W <= 2 && access.Bounds.H > 10 {
			return access
		}
	}
	t.Fatalf("Git sidebar divider not found: %q", h.tt.Texts())
	return ui.AccessInfo{}
}

func TestGitSidebarMinimumDuringDrag(t *testing.T) {
	const minWidth = 260
	root := gitRepo(t, map[string]string{"a.go": "base\n"})
	writeFile(t, root, "a.go", "staged\n")
	gitRun(t, root, "add", "a.go")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.message = "drag draft"
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()

	field, fieldOK := h.tt.AccessByRole(ui.RoleTextField, locales.MsgCommitMessage())
	button, buttonOK := h.tt.AccessByRole(ui.RoleButton, locales.MsgCommit())
	if !fieldOK || !buttonOK || button.Disabled {
		t.Fatalf("staged commit controls unavailable before drag: field %+v (%v), button %+v (%v)", field, fieldOK, button, buttonOK)
	}
	divider := gitSidebarDivider(t, h)
	x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+divider.Bounds.H/2
	targetX := float32(70)
	h.tt.Press(x, y)
	h.tt.Move(targetX, y)
	h.tt.Frame()

	divider = gitSidebarDivider(t, h)
	field, fieldOK = h.tt.AccessByRole(ui.RoleTextField, locales.MsgCommitMessage())
	button, buttonOK = h.tt.AccessByRole(ui.RoleButton, locales.MsgCommit())
	if divider.Bounds.X < minWidth-1 || h.a.sidebarW < minWidth || !fieldOK || field.Bounds.W < 100 || field.Offscreen || !buttonOK || button.Disabled || button.Bounds.W != 22 || button.Bounds.H != 22 || button.Offscreen {
		t.Fatalf("Git sidebar became unusable during an active left drag: divider %v, saved width %v, field %+v (%v), Commit %+v (%v)", divider.Bounds, h.a.sidebarW, field, fieldOK, button, buttonOK)
	}
	h.tt.Release(targetX, y)
	h.tt.Frame()
}

func TestGitSidebarMinimumFromNarrowSavedWidthIsGitOnly(t *testing.T) {
	const minWidth, savedWidth = 260, 180
	root := gitRepo(t, map[string]string{"a.go": "base\n"})
	cfg := &config.Config{ConfirmClose: true, SidebarWidth: savedWidth}
	h := newHarnessWithConfig(t, root, cfg)
	h.readGit()
	if h.a.sidebarW != savedWidth {
		t.Fatalf("saved sidebar width loaded as %v, want %v", h.a.sidebarW, savedWidth)
	}

	h.a.navigator = navGit
	h.tt.Advance(300 * time.Millisecond)
	h.tt.Frame()
	gitDivider := gitSidebarDivider(t, h)
	if gitDivider.Bounds.X < minWidth-1 || h.a.sidebarW != savedWidth || h.a.cfg.SidebarWidth != savedWidth {
		t.Fatalf("narrow startup width was not limited to Git display: divider %v, stored width %v, config width %v", gitDivider.Bounds, h.a.sidebarW, h.a.cfg.SidebarWidth)
	}

	h.a.navigator = navFiles
	h.tt.Frame()
	filesDivider := gitSidebarDivider(t, h)
	if filesDivider.Bounds.X < savedWidth-1 || filesDivider.Bounds.X > savedWidth+1 {
		t.Fatalf("non-Git navigator lost its saved sidebar width: divider %v, saved width %v", filesDivider.Bounds, savedWidth)
	}
}

func TestGitHistoryBottomRegionScrollsIndependently(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	for i := 0; i < 24; i++ {
		n := strconv.Itoa(i)
		writeFile(t, root, "a.go", "package a\n// "+n+"\n")
		gitRun(t, root, "add", "a.go")
		gitRun(t, root, "commit", "-qm", "Commit "+n)
	}
	writeFile(t, root, "a.go", "package a\n// saved work\n")
	gitRun(t, root, "stash", "push", "-q", "-m", "saved work")
	for i := 0; i < 28; i++ {
		writeFile(t, root, "work-"+strconv.Itoa(i)+".go", "package a\n")
	}

	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.tt.SetSize(harnessWidth, 900)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })

	history, ok := h.tt.Find("History")
	if !ok || history.Y < 250 || history.Y > 650 {
		t.Fatalf("expanded History is not in a stable bottom region: %v (%v), texts %q", history, ok, h.tt.Texts())
	}
	if h.a.git.split != 40 || !h.tt.HasText("Commit 23") {
		t.Fatalf("history split/list did not initialize: split %v, texts %q", h.a.git.split, h.tt.Texts())
	}
	before := strings.Join(h.tt.Texts(), "\x00")
	latest, latestOK := h.tt.Find("Commit 23")
	if !latestOK {
		t.Fatal("latest history row is not visible")
	}
	x, y := latest.X+latest.W/2, latest.Y+latest.H/2
	for i := 0; i < 12 && !h.tt.HasText("Commit 0"); i++ {
		h.tt.Wheel(x, y, 0, 120)
		h.tt.Advance(150 * time.Millisecond)
	}
	if !h.tt.HasText("Commit 0") || strings.Join(h.tt.Texts(), "\x00") == before {
		t.Fatalf("expanded History did not scroll independently: %q", h.tt.Texts())
	}
	historyAfterScroll, ok := h.tt.Find("History")
	if !ok || historyAfterScroll.Y != history.Y {
		t.Fatalf("scrolling history moved its bottom region: before %v, after %v", history, historyAfterScroll)
	}
	if err := h.tt.Click("Changes"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.tt.HasText("work-0.go") || !h.tt.HasText("Stashed Changes") || !h.tt.HasText("Stash: saved work") {
		t.Fatalf("collapsing Changes hid the independent stash group: %q", h.tt.Texts())
	}
	historyAfterCollapse, ok := h.tt.Find("History")
	if !ok || historyAfterCollapse.Y != history.Y {
		t.Fatalf("collapsing upper changes moved History: before %v, after %v", history, historyAfterCollapse)
	}
	if err := h.tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	foldedHistory, ok := h.tt.Find("History")
	if !h.a.git.historyClosed || !ok || foldedHistory.Y < 720 {
		t.Fatalf("collapsed History is not docked at the sidebar foot: closed %v, bounds %v, texts %q", h.a.git.historyClosed, foldedHistory, h.tt.Texts())
	}
	if err := h.tt.Click("History"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.history.loading })
	reopenedHistory, ok := h.tt.Find("History")
	if h.a.git.historyClosed || !ok || reopenedHistory.Y != history.Y || !h.tt.HasText("Commit 0") {
		t.Fatalf("reopened History lost its bottom region or scroll position: closed %v, bounds %v, texts %q", h.a.git.historyClosed, reopenedHistory, h.tt.Texts())
	}
}

func TestGitHistorySplitPersists(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.a.navigator = navGit
	h.a.git.split = 48
	h.a.captureSession()
	if got := h.a.cfg.Workspaces[h.a.root].GitSplit; got != 48 {
		t.Fatalf("session saved Git split %v, want 48", got)
	}
	h.a.git.split = 20
	h.a.restoreSession()
	if h.a.git.split != 48 {
		t.Fatalf("session restored Git split %v, want 48", h.a.git.split)
	}
}

func TestGitConflictSplitDoesNotChangeSavedGitSplit(t *testing.T) {
	root := gitRepo(t, map[string]string{"settings.go": "base\n"})
	gitRun(t, root, "switch", "-qc", "feature")
	writeFile(t, root, "settings.go", "incoming\n")
	gitRun(t, root, "commit", "-qam", "feature")
	gitRun(t, root, "switch", "-q", "main")
	writeFile(t, root, "settings.go", "current\n")
	gitRun(t, root, "commit", "-qam", "main")
	if _, err := git.Command(context.Background(), root, "merge", "feature"); err == nil {
		t.Fatal("expected a merge conflict")
	}

	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.split = 48
	h.tt.SetSize(harnessWidth, 900)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })
	if !h.tt.HasText("Conflicted Files") || h.a.git.conflictSplit < 62 || h.a.git.split != 48 {
		t.Fatalf("conflict split changed the saved ratio: normal %v, temporary %v, texts %q", h.a.git.split, h.a.git.conflictSplit, h.tt.Texts())
	}
	history, ok := h.tt.Find("History")
	if !ok {
		t.Fatalf("History not found: %q", h.tt.Texts())
	}
	before := h.a.git.conflictSplit
	var divider ui.AccessInfo
	best := float32(1000)
	for _, access := range h.tt.AccessAll() {
		if access.Role != ui.RoleSplitter || access.Bounds.H > 3 {
			continue
		}
		distance := history.Y - (access.Bounds.Y + access.Bounds.H)
		if distance < 0 {
			distance = -distance
		}
		if distance < best {
			divider, best = access, distance
		}
	}
	if best > 10 {
		t.Fatalf("History split divider not found near its header: %v, %q", history, h.tt.Texts())
	}
	x, y := divider.Bounds.X+divider.Bounds.W/2, divider.Bounds.Y+divider.Bounds.H/2
	h.tt.Press(x, y)
	h.tt.Move(x, y+80)
	h.tt.Release(x, y+80)
	h.tt.Frame()
	if h.a.git.conflictSplit <= before || h.a.git.split != 48 {
		t.Fatalf("dragging conflict split changed the saved ratio: normal %v, temporary %v (before %v)", h.a.git.split, h.a.git.conflictSplit, before)
	}
	h.a.captureSession()
	if got := h.a.cfg.Workspaces[h.a.root].GitSplit; got != 48 {
		t.Fatalf("session saved conflict ratio %v instead of normal ratio 48", got)
	}
}

// The Git navigator's history lists commits; a commit opens with its
// files, and a file of it as a diff.
func TestHistoryOpensACommitAndItsFile(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.navigator, h.a.git.changesClosed = navGit, true
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })
	for _, s := range []string{"History", "Merge side", "Add B on the side", "Add A", "first"} {
		if !h.tt.HasText(s) {
			t.Fatalf("the history shows no %q: %q", s, h.tt.Texts())
		}
	}
	if err := h.tt.Click("Add B on the side"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		v := h.a.focus.diffs[h.a.focus.active]
		return v != nil && v.commit != nil && !v.commit.loading
	})
	if !h.tt.HasText("1 file changed") {
		t.Fatalf("the commit shows %q", h.tt.Texts())
	}
	if err := h.tt.Click("b.go"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { v := h.a.focus.diffs[h.a.focus.active]; return v != nil && v.state != nil })
	if v := h.a.focus.diffs[h.a.focus.active]; v.name != "b.go" || v.rev == "" {
		t.Fatalf("diff tab %+v", v)
	}
	// The commit's tab stays beside its file's.
	if len(h.a.focus.order) != 2 {
		t.Fatalf("tabs %q", h.a.focus.order)
	}
}

// Push makes the branch track a new remote branch; Fetch counts what
// came in, and Pull brings it.
func TestPushFetchPull(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "add", "origin", bare)
	h := newHarness(t, root)
	h.readGit()
	idle := func() bool { return !h.a.git.busy && !h.a.git.loading }

	h.a.gitPush()
	h.settle(idle)
	h.readGit()
	if h.a.git.upstream != "origin/main" {
		t.Fatalf("upstream %q after the first push, notice %q", h.a.git.upstream, h.a.notice)
	}

	// Someone else pushes a commit.
	other := t.TempDir()
	gitRun(t, other, "clone", "-q", "-b", "main", bare, ".")
	writeFile(t, other, "b.go", "package a\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-qm", "theirs")
	gitRun(t, other, "push", "-q", "origin", "HEAD:main")

	h.a.gitFetch()
	h.settle(idle)
	h.readGit()
	if h.a.git.behind != 1 {
		t.Fatalf("behind %d after fetching, notice %q", h.a.git.behind, h.a.notice)
	}
	h.a.gitPull()
	h.settle(idle)
	h.readGit()
	if h.a.git.behind != 0 {
		t.Fatalf("behind %d after pulling, notice %q", h.a.git.behind, h.a.notice)
	}
	if _, err := os.Stat(filepath.Join(root, "b.go")); err != nil {
		t.Fatal("the pull brought no b.go")
	}
}

func TestGitAutoFetchUpdatesNavigatorTitleAndLog(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "add", "origin", bare)
	gitRun(t, root, "push", "-qu", "origin", "main")
	other := t.TempDir()
	gitRun(t, other, "clone", "-q", "-b", "main", bare, ".")
	writeFile(t, other, "incoming.go", "package a\n")
	gitRun(t, other, "add", ".")
	gitRun(t, other, "commit", "-qm", "incoming work")
	gitRun(t, other, "push", "-q", "origin", "main")

	h := newHarness(t, root)
	t.Cleanup(h.a.wsCancel)
	h.a.navigator = navGit
	h.a.note("Reading source", false)
	h.a.git.autoFetch = true
	h.readGit() // the same status refresh used by the periodic poll
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if h.a.git.behind != 1 || !h.tt.HasText("↓1") || !h.tt.HasText("incoming work") || !h.tt.HasText("↓") {
		t.Fatalf("incoming state: behind=%d, UI=%q", h.a.git.behind, h.tt.Texts())
	}
	if h.a.notice != "Reading source" {
		t.Fatalf("background fetch replaced the notice: %q", h.a.notice)
	}
	if _, err := os.Stat(filepath.Join(root, "incoming.go")); !os.IsNotExist(err) {
		t.Fatalf("auto-fetch changed the checkout: %v", err)
	}
	h.a.openGitLog()
	h.a.sidebarOff = true
	h.tt.Frame()
	if !h.tt.HasText("incoming work") || !h.tt.HasText("↓") {
		t.Fatalf("log omits incoming commits: %q", h.tt.Texts())
	}

	h.a.sidebarOff = false
	gitRun(t, other, "commit", "--allow-empty", "-qm", "later work")
	gitRun(t, other, "push", "-q", "origin", "main")
	h.readGit()
	if h.a.git.behind != 1 || h.tt.HasText("later work") {
		t.Fatal("ordinary status refresh ignored the fetch interval")
	}
	h.a.git.lastFetch = time.Now().Add(-gitFetchInterval)
	h.readGit()
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if h.a.git.behind != 2 || !h.tt.HasText("↓2") || !h.tt.HasText("later work") {
		t.Fatalf("periodic fetch: behind=%d, UI=%q", h.a.git.behind, h.tt.Texts())
	}
	if err := h.tt.Click("Pull"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if h.a.git.behind != 0 || len(h.a.git.history.incoming) != 0 || h.tt.HasText("↓2") {
		t.Fatalf("pull left incoming state: %+v", h.a.git.history.incoming)
	}
}

func TestExternalGitRefsRefreshHistoryWithoutMovingHead(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "add", "origin", bare)
	gitRun(t, root, "push", "-qu", "origin", "main")
	gitRun(t, root, "commit", "--allow-empty", "-qm", "local work")
	h := newHarness(t, root)
	t.Cleanup(h.a.wsCancel)
	h.a.navigator = navGit
	h.readGit()
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	head := h.a.git.head
	if !h.a.git.history.unpushed[head] || !h.tt.HasText("↑1") {
		t.Fatal("local commit was not marked outgoing")
	}
	gitRun(t, root, "push", "-q")
	gitRun(t, root, "tag", "published")
	h.readGit()
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if h.a.git.head != head || len(h.a.git.history.unpushed) != 0 || h.tt.HasText("↑1") || !slices.Contains(h.a.git.history.commits[0].Refs, "published") {
		t.Fatalf("external push/tag left stale history: %q", h.tt.Texts())
	}
	gitRun(t, root, "tag", "-d", "published")
	h.readGit()
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if slices.Contains(h.a.git.history.commits[0].Refs, "published") {
		t.Fatal("deleted tag remained in history")
	}
	other := t.TempDir()
	gitRun(t, other, "clone", "-q", "-b", "main", bare, ".")
	gitRun(t, other, "commit", "--allow-empty", "-qm", "external incoming")
	gitRun(t, other, "push", "-q")
	gitRun(t, root, "fetch", "-q")
	h.readGit()
	h.settle(func() bool { return !h.a.git.history.loading && !h.a.git.history.stale })
	if h.a.git.head != head || h.a.git.behind != 1 || !h.tt.HasText("external incoming") {
		t.Fatalf("external fetch left stale history: %q", h.tt.Texts())
	}
}

func TestGitAutoFetchFailureRetriesAndDiscoversRemotes(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	t.Cleanup(h.a.wsCancel)
	h.a.git.autoFetch = true
	h.readGit()
	if !h.a.git.lastFetch.IsZero() {
		t.Fatal("fetched a repository without remotes")
	}
	gitRun(t, root, "remote", "add", "origin", filepath.Join(t.TempDir(), "missing"))
	h.a.note("Reading source", false)
	h.readGit()
	firstAttempt := h.a.git.lastFetch
	if firstAttempt.IsZero() || h.a.notice != "Reading source" || h.a.git.busy {
		t.Fatalf("failed fetch did not settle quietly: %q", h.a.notice)
	}
	writeFile(t, root, "a.go", "package changed\n")
	h.readGit()
	if !h.a.git.lastFetch.Equal(firstAttempt) || len(h.a.git.changes) != 1 {
		t.Fatal("failed fetch retried too soon or stopped local status updates")
	}
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "set-url", "origin", bare)
	gitRun(t, root, "push", "-qu", "origin", "main")
	gitRun(t, bare, "tag", "recovered", "main")
	h.a.git.lastFetch = time.Now().Add(-gitFetchInterval)
	h.readGit()
	if out, err := git.Command(context.Background(), root, "rev-parse", "--verify", "refs/tags/recovered"); err != nil || len(out) == 0 {
		t.Fatalf("auto-fetch did not recover: %v", err)
	}
}

// A fetch that cannot reach its remote stands auto-fetch down: the
// failure counts once, no retry follows, and the fetch the user runs
// when the network is back — which offline never blocks — lifts it.
func TestGitAutoFetchStandsDownOffline(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	// Port 1 of localhost answers nothing, so the fetch fails the way a
	// dropped connection does, fast and offline or not.
	gitRun(t, root, "remote", "add", "origin", "https://127.0.0.1:1/r.git")
	h := newHarness(t, root)
	t.Cleanup(h.a.wsCancel)
	h.a.git.autoFetch = true
	h.readGit()
	if !h.a.git.offline || h.a.git.fetchFailures != 0 {
		t.Fatalf("unreachable remote: offline %v, failures %d", h.a.git.offline, h.a.git.fetchFailures)
	}

	// Past every backoff, auto-fetch still does not try while offline.
	h.a.git.lastFetch = time.Now().Add(-time.Hour)
	before := h.a.git.lastFetch
	h.readGit()
	if !h.a.git.lastFetch.Equal(before) {
		t.Fatal("auto-fetch retried while offline")
	}

	// The user's own fetch still runs, and its failure keeps the state.
	h.a.gitFetch()
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	if !h.a.git.offline {
		t.Fatal("a manual fetch offline never blocks cleared the state")
	}

	// The remote answering again — the machine back on the network — the
	// user's fetch succeeds, and the next refresh's auto-fetch runs.
	bare := t.TempDir()
	gitRun(t, bare, "init", "-q", "--bare")
	gitRun(t, root, "remote", "set-url", "origin", bare)
	gitRun(t, root, "push", "-qu", "origin", "main")
	gitRun(t, bare, "tag", "recovered", "main")
	h.a.gitFetch()
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	if h.a.git.offline {
		t.Fatal("a successful manual fetch left the app offline")
	}
	h.a.git.lastFetch = time.Now().Add(-gitFetchInterval)
	h.readGit()
	if _, err := git.Command(context.Background(), root, "rev-parse", "--verify", "refs/tags/recovered"); err != nil {
		t.Fatalf("auto-fetch did not resume after the network recovered: %v", err)
	}
}

// The editor's links name the lines selected: a reference from the
// workspace's root, and their page on the host at HEAD.
func TestCodeLinks(t *testing.T) {
	root := gitRepo(t, map[string]string{"pkg/a b.go": "package a\n\nfunc A() {}\n\nfunc B() {}\n"})
	gitRun(t, root, "remote", "add", "origin", "git@github.com:o/r.git")
	h := newHarness(t, root)
	h.readGit()
	d := h.open("pkg/a b.go")
	st := h.a.focus.eds[d.path]
	st.Select(d.buf.Offset(editor.Pos{Line: 2}), d.buf.Offset(editor.Pos{Line: 4}))
	if got := h.a.codeReference(d, st); got != "pkg/a b.go:3-4" {
		t.Fatalf("reference %q", got)
	}
	want := "https://github.com/o/r/blob/" + h.a.git.head + "/pkg/a%20b.go#L3-L4"
	if got := h.a.webLink(d, st); got != want {
		t.Fatalf("link %q, want %q", got, want)
	}
	st.Select(d.buf.Offset(editor.Pos{Line: 2, Col: 3}), d.buf.Offset(editor.Pos{Line: 2, Col: 3}))
	if got := h.a.codeReference(d, st); got != "pkg/a b.go:3" {
		t.Fatalf("reference %q", got)
	}
}

// A change's diff chooses its file in the tree, as the file's own tab
// does, for when the project navigator shows again.
func TestDiffRevealsItsFile(t *testing.T) {
	root := gitRepo(t, map[string]string{"pkg/a.go": "package a\n"})
	writeFile(t, root, "pkg/a.go", "package a\n\nfunc A() {}\n")
	h := newHarness(t, root)
	h.readGit()
	ch := h.a.git.changes[0]
	h.a.showGitDiff(ch, false)
	h.settle(func() bool { return isDiffTab(h.a.focus.active) })
	if want := filepath.Join(root, "pkg", "a.go"); h.a.reveal != want || !h.a.tree.Open.Has(filepath.Join(root, "pkg")) {
		t.Fatalf("reveal %q, want %q", h.a.reveal, want)
	}
}

// A change's diff, a commit and a file of it come back in the next
// session, the active one active; a change that is gone closes.
func TestGitTabsSurviveARestart(t *testing.T) {
	root := historyRepo(t) // a.go is changed
	writeFile(t, root, "gone.go", "package a\n")
	h := newHarness(t, root)
	h.readGit()
	ch := func(path string) git.Change {
		for _, c := range h.a.git.changes {
			if c.Path == path {
				return c
			}
		}
		t.Fatalf("no change of %s", path)
		return git.Change{}
	}
	h.a.showGitDiff(ch("a.go"), false)
	h.settle(func() bool { return h.a.focus.has(diffTab("a.go", false)) })
	h.a.showGitDiff(ch("gone.go"), false)
	h.settle(func() bool { return h.a.focus.has(diffTab("gone.go", false)) })
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading })
	var side git.Commit
	for _, c := range h.a.git.history.commits {
		if c.Subject == "Add B on the side" {
			side = c
		}
	}
	h.a.openCommit(side)
	h.settle(func() bool { v := h.a.focus.diffs[commitTab(side.Hash)]; return v != nil && !v.commit.loading })
	h.a.openCommitFile(h.a.focus, side, git.FileStat{Path: "b.go"})
	h.settle(func() bool { return h.a.focus.active == commitFileTab(side.Hash, "b.go") })
	h.a.captureSession()
	if err := os.Remove(filepath.Join(root, "gone.go")); err != nil {
		t.Fatal(err)
	}

	next := newHarness(t, "")
	next.a.cfg = h.a.cfg
	next.a.openWorkspace(root)
	next.settle(func() bool { d := next.a.dirs[root]; return d != nil && d.loaded })
	next.readGit()
	next.settle(func() bool {
		for _, v := range next.a.focus.diffs {
			if v.pending {
				return false
			}
		}
		return true
	})
	want := []string{diffTab("a.go", false), commitTab(side.Hash), commitFileTab(side.Hash, "b.go")}
	if !slices.Equal(next.a.focus.order, want) {
		t.Fatalf("tabs %q, want %q", next.a.focus.order, want)
	}
	if next.a.focus.active != want[2] || next.a.focus.diffs[want[2]].state == nil {
		t.Fatalf("active %q", next.a.focus.active)
	}
	if v := next.a.focus.diffs[want[1]]; v.commit.commit.Subject != "Add B on the side" || len(v.commit.details.Files) != 1 {
		t.Fatalf("commit %+v", v.commit)
	}
}

// A diff shows side by side or inline, as chosen, for every diff and the
// next ones; Next and Previous Change step through its changes, around.
func TestDiffLayoutAndChanges(t *testing.T) {
	text := "a\nb\nc\nd\ne\nf\ng\nh\ni\nj\nk\nl\n"
	root := gitRepo(t, map[string]string{"a.txt": text})
	writeFile(t, root, "a.txt", strings.Replace(strings.Replace(text, "b\n", "B\n", 1), "k\n", "K\n", 1))
	h := newHarness(t, root)
	h.readGit()
	h.a.showGitDiff(h.a.git.changes[0], false)
	h.settle(func() bool { return isDiffTab(h.a.focus.active) })
	v := h.a.focus.diffs[h.a.focus.active]
	if v.state.Layout != editor.DiffSplit {
		t.Fatal("a diff starts side by side")
	}
	if err := h.tt.Click(locales.MsgInlineDiff()); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if v.state.Layout != editor.DiffUnified || !h.a.cfg.DiffUnified {
		t.Fatal("the button did not show the diff inline")
	}
	if n := len(v.state.Hunks()); n != 2 {
		t.Fatalf("%d hunks", n)
	}
	v.state.GoToHunk(0)
	for _, step := range []struct{ dir, want int }{{1, 1}, {1, 0}, {-1, 1}, {-1, 0}} {
		h.a.goToChange(step.dir)
		if i, _ := v.state.CurrentHunk(); i != step.want {
			t.Fatalf("after %+d: hunk %d, want %d", step.dir, i, step.want)
		}
	}

	// In the file's own editor, the same commands step through its
	// uncommitted changes, around, leaving the caret at their lines.
	d := h.open("a.txt")
	h.settle(func() bool { return d.gitChanges != nil && len(d.gitChanges.Hunks()) == 2 })
	st := h.a.focus.eds[d.path]
	for _, step := range []struct {
		dir, want int
	}{
		{1, 1}, {1, 10}, {1, 1}, {-1, 10}, {-1, 1}, {-1, 10},
	} {
		h.a.goToChange(step.dir)
		if got := st.CursorPos().Line; got != step.want {
			t.Fatalf("after %+d: caret line %d, want %d", step.dir, got, step.want)
		}
	}
}

type idleWatcher struct{}

func (idleWatcher) Next(ctx context.Context) ([]mygo.FileEvent, error) {
	<-ctx.Done()
	return nil, ctx.Err()
}
func (idleWatcher) Close() error { return nil }

// What git ignores stays out of the watch, folders whole, so that a big
// project's build output does not exhaust it; the tree dims it.
func TestWatchLeavesOutWhatGitIgnores(t *testing.T) {
	root := gitRepo(t, map[string]string{".gitignore": "out/\n*.log\n", "a.go": "package a\n"})
	writeFile(t, root, "x.log", "")
	if err := os.MkdirAll(filepath.Join(root, "out", "deep"), 0o755); err != nil {
		t.Fatal(err)
	}
	opts := make(chan mygo.FileWatchOptions, 1)
	old := watchFiles
	t.Cleanup(func() { watchFiles = old })
	watchFiles = func(ctx context.Context, root string, o mygo.FileWatchOptions) (fileWatcher, error) {
		opts <- o
		return idleWatcher{}, nil
	}
	h := newHarness(t, root)
	o := <-opts
	for rel, want := range map[string]bool{"out": true, "out/deep": true, "node_modules": true, "src": false} {
		if got := o.Ignore(rel, true); got != want {
			t.Errorf("Ignore(%q, dir) = %v, want %v", rel, got, want)
		}
	}
	if o.Ignore("x.log", false) {
		t.Error("the watch leaves out a single ignored file, which may be open")
	}
	h.settle(func() bool { return h.a.ignored != nil })
	if !h.a.gitIgnores(filepath.Join(root, "x.log")) || !h.a.gitIgnores(filepath.Join(root, "out")) || h.a.gitIgnores(filepath.Join(root, "a.go")) {
		t.Fatal("the tree dims the wrong entries")
	}
}
