package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

// gitRepo is a repository of files with one commit on main, and the
// branches named.
func gitRepo(t *testing.T, files map[string]string, branches ...string) string {
	t.Helper()
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

// gitRun runs git in root as a test's committer.
func gitRun(t *testing.T, root string, args ...string) {
	t.Helper()
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

// The Git navigator's history lists the commits under the uncommitted
// changes; a commit opens with its files, and a file of it as a diff.
func TestHistoryOpensACommitAndItsFile(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.navigator, h.a.git.mode = navGit, gitModeHistory
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })
	for _, s := range []string{"Uncommitted Changes", "Merge side", "Add B on the side", "Add A", "first"} {
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
	h.a.git.mode = gitModeHistory
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
