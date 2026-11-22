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
	if err := h.tt.Click(logText("gitKindDocs")); err != nil {
		t.Fatal(err)
	}
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

func TestGitCommitInputFollowsStagedRows(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"})
	writeFile(t, root, "a.go", "package a\n// current\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator = navGit
	h.a.git.historyClosed = true
	h.tt.Frame()

	hasCommitField := func() bool {
		for _, access := range h.tt.AccessAll() {
			if access.Role == ui.RoleTextField && access.Name == locales.MsgCommitMessage() {
				return true
			}
		}
		return false
	}
	if hasCommitField() {
		t.Fatal("empty commit field shown without staged changes or a draft")
	}
	if err := h.tt.Click("Stage All Changes"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	file, fileOK := h.tt.Find("Staged diff: a.go")
	commit, commitOK := h.tt.Find(locales.MsgCommitMessage())
	if !fileOK || !commitOK || commit.Y <= file.Y {
		t.Fatalf("commit field is not after staged rows: file %v (%v), commit %v (%v), texts %q", file, fileOK, commit, commitOK, h.tt.Texts())
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
	if !hasCommitField() {
		t.Fatal("commit draft field disappeared when changes were unstaged")
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
	if err := h.tt.Click("Show Changes Inline"); err != nil {
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
