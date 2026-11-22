package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

func TestGitConflictResolverStagesEachChoice(t *testing.T) {
	const name = "settings.go"
	content := func(prefix string) string {
		var b strings.Builder
		b.WriteString("package demo\n")
		for hunk := 1; hunk <= 3; hunk++ {
			fmt.Fprintf(&b, "// %s %d\n", prefix, hunk)
			for i := range 12 {
				fmt.Fprintf(&b, "// unchanged %d.%d\n", hunk, i)
			}
		}
		return b.String()
	}
	root := gitRepo(t, map[string]string{name: content("base"), "sibling.go": "base\n", "unrelated.go": "clean\n"})
	gitRun(t, root, "config", "core.autocrlf", "false")
	gitRun(t, root, "switch", "-qc", "feature")
	writeFile(t, root, name, content("incoming"))
	writeFile(t, root, "sibling.go", "incoming\n")
	gitRun(t, root, "commit", "-qam", "feature")
	gitRun(t, root, "switch", "-q", "main")
	writeFile(t, root, name, content("current"))
	writeFile(t, root, "sibling.go", "current\n")
	gitRun(t, root, "commit", "-qam", "main")
	ctx := context.Background()
	if _, err := git.Command(ctx, root, "-c", "merge.conflictStyle=merge", "merge", "feature"); err == nil {
		t.Fatal("expected conflicts")
	}
	writeFile(t, root, "unrelated.go", "unstaged\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := h.tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	unmerged := func() string {
		t.Helper()
		out, err := git.Command(ctx, root, "diff", "--name-only", "--diff-filter=U")
		if err != nil {
			t.Fatal(err)
		}
		return string(out)
	}
	click("Stage All Changes")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	if got := unmerged(); got != "settings.go\nsibling.go\n" {
		t.Fatalf("Stage All touched conflicts: %q", got)
	}
	if !h.tt.HasText("Conflicted Files") || h.tt.HasText("Unstaged diff: "+name) || h.tt.HasText("Staged diff: "+name) {
		t.Fatalf("conflict not isolated in the navigator: %v", h.tt.Texts())
	}
	click("Resolve conflict in " + name)
	h.settle(func() bool {
		v := h.a.focus.diffs[h.a.focus.active]
		return v != nil && v.conflict != nil && !v.conflict.loading
	})
	if !h.tt.HasText("Conflict 1 of 3") || !h.tt.HasText("0 of 3 resolved") {
		t.Fatalf("wrong initial state: %v", h.tt.Texts())
	}
	_ = h.tt.Click("Save & Stage")
	if h.a.git.busy {
		t.Fatal("Save & Stage accepted incomplete choices")
	}
	click("Keep Current")
	click("Next Conflict")
	click("Keep Incoming")
	click("Next Conflict")
	click("Keep Both")
	click("Previous Conflict")
	if !h.tt.HasText("Conflict 2 of 3") || !h.tt.HasText("3 of 3 resolved") {
		t.Fatalf("navigation lost decisions: %v", h.tt.Texts())
	}
	doc := h.open(name)
	doc.buf.Edit(editor.Edit{Text: "// unsaved\n"})
	click("Resolve conflict in " + name)
	click("Save & Stage")
	if h.a.git.busy || !doc.buf.Modified() || unmerged() != "settings.go\nsibling.go\n" {
		t.Fatal("resolver overwrote unsaved edits")
	}
	doc.buf.Undo()
	if doc.buf.Modified() {
		t.Fatal("undo did not restore a clean buffer")
	}
	click("Save & Stage")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading && !doc.loading })
	if !h.tt.HasText("Resolution staged") || h.tt.HasText("Resolve conflict in "+name) || unmerged() != "sibling.go\n" {
		t.Fatalf("wrong resolved state: %v", h.tt.Texts())
	}
	want := strings.NewReplacer("// base 1\n", "// current 1\n", "// base 2\n", "// incoming 2\n",
		"// base 3\n", "// current 3\n// incoming 3\n").Replace(content("base"))
	out, err := git.Command(ctx, root, "show", ":"+name)
	if err != nil || string(out) != want {
		t.Fatalf("staged contents = %q, %v", out, err)
	}
	work, err := os.ReadFile(filepath.Join(root, name))
	if err != nil || string(work) != want || doc.buf.String() != want {
		t.Fatalf("working file/editor not updated: disk %q, buffer %q, %v", work, doc.buf.String(), err)
	}
}

func TestGitConflictKeepCurrentAllowsMergeCommitWithoutStagedChanges(t *testing.T) {
	const name = "settings.go"
	root := gitRepo(t, map[string]string{name: "base\n"})
	gitRun(t, root, "switch", "-qc", "feature")
	writeFile(t, root, name, "incoming\n")
	gitRun(t, root, "commit", "-qam", "feature")
	gitRun(t, root, "switch", "-q", "main")
	writeFile(t, root, name, "current\n")
	gitRun(t, root, "commit", "-qam", "main")
	ctx := context.Background()
	if _, err := git.Command(ctx, root, "merge", "feature"); err == nil {
		t.Fatal("expected a conflict")
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.a.git.message = "Merge feature"
	h.tt.Frame()
	click := func(label string) {
		t.Helper()
		if err := h.tt.Click(label); err != nil {
			t.Fatal(err)
		}
	}
	click("Resolve conflict in " + name)
	h.settle(func() bool {
		v := h.a.focus.diffs[h.a.focus.active]
		return v != nil && v.conflict != nil && !v.conflict.loading
	})
	click("Keep Current")
	click("Save & Stage")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	if out, err := git.Command(ctx, root, "status", "--porcelain"); err != nil || len(out) != 0 {
		t.Fatalf("expected a clean index and worktree, got %q, %v", out, err)
	}
	click("Commit")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	parents, err := git.Command(ctx, root, "rev-list", "--parents", "-n", "1", "HEAD")
	if err != nil || len(strings.Fields(string(parents))) != 3 {
		t.Fatalf("expected a two-parent merge commit, got %q, %v", parents, err)
	}
}
