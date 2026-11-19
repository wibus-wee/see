package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
)

func TestBranchDiffPickerAndTabs(t *testing.T) {
	lines := func(prefix string) string {
		var b strings.Builder
		for i := range 12 {
			fmt.Fprintf(&b, "%s-%03d\n", prefix, i)
		}
		return b.String()
	}
	baseFiles := map[string]string{"a.txt": lines("base-a"), "b.txt": lines("base-b"), "c.txt": lines("base-c")}
	for i := range 18 {
		baseFiles[fmt.Sprintf("z-%02d.txt", i)] = lines(fmt.Sprintf("base-z-%02d", i))
	}
	baseFiles["zz-target.txt"] = lines("base-target")
	root := gitRepo(t, baseFiles, "base", "other")
	for name := range baseFiles {
		writeFile(t, root, name, lines("head-"+strings.TrimSuffix(name, ".txt")))
	}
	gitRun(t, root, "add", "--all")
	gitRun(t, root, "commit", "-qm", "committed work")
	writeFile(t, root, "b.txt", lines("working-b"))
	writeFile(t, root, "untracked.txt", lines("untracked"))
	head := git.Head(context.Background(), root)
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.tt.SetSize(harnessWidth, 1000)
	h.tt.Frame()

	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Type("base")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool {
		return h.a.git.branchPanel != nil && !h.a.git.branchPanel.loading && h.a.git.branchPanel.result != nil
	})
	if h.a.git.branch != "main" || git.Head(context.Background(), root) != head {
		t.Fatalf("base selection changed HEAD: branch %q, HEAD %q, want %q", h.a.git.branch, git.Head(context.Background(), root), head)
	}
	if h.a.git.branchPanel.baseRef != "refs/heads/base" || !h.tt.HasText("Read-only") {
		t.Fatalf("base panel = %+v; visible text = %v", h.a.git.branchPanel, h.tt.Texts())
	}
	if h.tt.HasText("Stage All Changes") || h.tt.HasText("Unstage All Changes") {
		t.Fatalf("comparison mode exposes staging controls: %v", h.tt.Texts())
	}

	if err := h.tt.Click("zz-target.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	key := branchDiffTab("refs/heads/base")
	v := h.a.focus.diffs[key].branch
	if h.a.focus.active != key || v == nil || v.jumpPath != "" || v.scroll.Y <= 0 || v.shown <= branchDiffFilesAtOnce || v.collapsed["zz-target.txt"] {
		t.Fatalf("file click did not navigate the shared diff: active %q, view %+v", h.a.focus.active, v)
	}
	if !h.tt.HasText("Base: base") {
		t.Fatalf("branch-diff base control is not visible: %v", h.tt.Texts())
	}
	v.collapsed["zz-target.txt"] = true
	order := len(h.a.focus.order)
	if err := h.tt.Click("zz-target.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if len(h.a.focus.order) != order || h.a.focus.diffs[key].branch != v {
		t.Fatalf("reopening a base created another tab: %v", h.a.focus.order)
	}
	if v.collapsed["zz-target.txt"] || v.jumpPath != "" {
		t.Fatalf("sidebar navigation did not reveal a collapsed file: %+v", v)
	}

	if err := h.tt.Click("Change Base"); err != nil {
		t.Fatal(err)
	}
	if h.a.git.picker.at.W <= 0 || h.a.git.picker.at.H <= 0 {
		t.Fatalf("base picker is not anchored to its button: %+v", h.a.git.picker.at)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.git.picker.open || h.a.git.branchPanel.baseRef != "refs/heads/base" {
		t.Fatal("Escape did not cancel base selection")
	}
	if err := h.tt.Click("Change Base"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Type("other")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool {
		return h.a.git.branchPanel.baseRef == "refs/heads/other" && !h.a.git.branchPanel.loading && h.a.git.branchPanel.result != nil
	})
	if err := h.tt.Click("View Diff"); err != nil {
		t.Fatal(err)
	}
	if h.a.focus.active != branchDiffTab("refs/heads/other") || !h.a.focus.has(key) || h.a.focus.diffs[key].branch.baseRef != "refs/heads/base" {
		t.Fatalf("base tabs lost their identity: order %v active %q", h.a.focus.order, h.a.focus.active)
	}
	if h.a.git.branch != "main" || git.Head(context.Background(), root) != head {
		t.Fatal("selecting a comparison base changed the checked-out branch")
	}

	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.git.branchDiffMode != uncommittedMode || !h.tt.HasText("Stage All Changes") {
		t.Fatalf("returning to uncommitted mode did not restore staging controls: %v", h.tt.Texts())
	}
}

func TestBranchDiffRejectsStaleBaseResult(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "other")
	h := newHarness(t, root)
	h.readGit()
	old := &branchDiffView{baseRef: "refs/heads/main", baseName: "main", shown: branchDiffFilesAtOnce}
	old.seq = 2
	h.a.git.branchPanel = old
	h.a.acceptBranchDiff(old, 1, h.a.gen, "refs/heads/main", root, "stale", git.BranchDiffResult{Patch: []byte("stale")}, nil, nil)
	old.baseRef = "refs/heads/other"
	h.a.acceptBranchDiff(old, 2, h.a.gen, "refs/heads/main", root, "stale", git.BranchDiffResult{Patch: []byte("stale")}, nil, nil)
	if old.result != nil || old.loading {
		t.Fatalf("stale result was accepted: %+v", old)
	}
}

func TestBranchDiffEmptyState(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "same\n"})
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.a.git.branchDiffMode = compareBaseMode
	h.a.git.branchPanel = &branchDiffView{
		baseRef:  "refs/heads/main",
		baseName: "main",
		result:   &git.BranchDiffResult{},
	}
	h.tt.Frame()
	if !h.tt.HasText("No changes since this base.") || h.tt.HasText("Stage All Changes") {
		t.Fatalf("empty comparison state = %v", h.tt.Texts())
	}
}

func TestSplitLoadingBranchDiffLoadsIndependentView(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "base")
	writeFile(t, root, "a.txt", "current\n")
	gitRun(t, root, "add", "a.txt")
	gitRun(t, root, "commit", "-qm", "current")

	h := newHarness(t, root)
	h.readGit()
	originalGroup := h.a.focus
	key := branchDiffTab("refs/heads/base")
	h.a.openBranchDiff(originalGroup, "refs/heads/base", "base", "a.txt")
	original := originalGroup.diffs[key].branch
	if original == nil || !original.loading {
		t.Fatalf("expected original comparison to be loading: %+v", original)
	}
	h.a.splitActive(sideRight)
	cloneGroup := h.a.focus
	clone := cloneGroup.diffs[key].branch
	if clone == nil || clone == original || !clone.loading || clone.files != nil {
		t.Fatalf("split did not start an independent comparison: original %+v, clone %+v", original, clone)
	}
	h.settle(func() bool {
		return original.result != nil && clone.result != nil && !original.loading && !clone.loading
	})
	if len(original.files) != 1 || len(clone.files) != 1 || original.files[0].diff == nil || clone.files[0].diff == nil {
		t.Fatalf("split comparisons did not load diff states: original %+v, clone %+v", original.files, clone.files)
	}
	if &original.files[0] == &clone.files[0] || original.files[0].diff == clone.files[0].diff {
		t.Fatal("split comparisons share mutable file or diff state")
	}
}

func TestBranchDiffFingerprintIncludesIndexMetadata(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"})
	writeFile(t, root, "a.txt", "staged one\n")
	gitRun(t, root, "add", "a.txt")
	index, err := git.Command(context.Background(), root, "rev-parse", "--git-path", "index")
	if err != nil {
		t.Fatal(err)
	}
	indexPath := strings.TrimSpace(string(index))
	if !filepath.IsAbs(indexPath) {
		indexPath = filepath.Join(root, indexPath)
	}
	mtime := time.Unix(100, 0)
	if err := os.Chtimes(filepath.Join(root, "a.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(indexPath, mtime, mtime); err != nil {
		t.Fatal(err)
	}
	changes := []git.Change{{Index: 'M', Work: ' ', Path: "a.txt"}}
	first := gitBranchFingerprint(context.Background(), root, "head", "refs", changes)

	writeFile(t, root, "a.txt", "staged two\n")
	gitRun(t, root, "add", "a.txt")
	if err := os.Chtimes(filepath.Join(root, "a.txt"), mtime, mtime); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(indexPath, mtime.Add(time.Second), mtime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	second := gitBranchFingerprint(context.Background(), root, "head", "refs", changes)
	if first == second {
		t.Fatal("staging different contents with unchanged status and worktree metadata did not change fingerprint")
	}
}

func TestBranchDiffBinaryIndicatorWithoutEncodedBlob(t *testing.T) {
	root := gitRepo(t, map[string]string{"binary.dat": "old\x00binary"}, "base")
	writeFile(t, root, "binary.dat", "new\x00binary")
	h := newHarness(t, root)
	h.readGit()
	h.a.openBranchDiff(h.a.focus, "refs/heads/base", "base", "")
	view := h.a.focus.diffs[branchDiffTab("refs/heads/base")].branch
	h.settle(func() bool { return view.result != nil && !view.loading })
	if len(view.files) != 1 || !view.files[0].binary || view.files[0].diff != nil {
		t.Fatalf("binary file was not represented as a binary indicator: %+v", view.files)
	}
	h.tt.Frame()
	if !h.tt.HasText("Binary file diff is not shown.") || h.tt.HasText("GIT binary patch") {
		t.Fatalf("binary diff content = %v", h.tt.Texts())
	}
}
