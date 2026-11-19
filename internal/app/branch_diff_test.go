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

func settleBranchDiff(t *testing.T, h *harness) *branchDiffView {
	t.Helper()
	h.settle(func() bool {
		v := h.a.git.branchPanel
		return v == nil || !v.loading
	})
	v := h.a.git.branchPanel
	if v == nil {
		t.Fatal("base selection did not create a comparison")
	}
	if v.err != "" {
		t.Fatalf("comparison for base %q failed: %s", v.baseRef, v.err)
	}
	if v.result == nil {
		t.Fatalf("comparison for base %q completed without a result: %+v", v.baseRef, v)
	}
	return v
}

func settleBranchDiffTab(t *testing.T, h *harness, g *group, key string) *branchDiffView {
	t.Helper()
	h.settle(func() bool {
		d := g.diffs[key]
		return d == nil || d.branch == nil || !d.branch.loading
	})
	d := g.diffs[key]
	if d == nil || d.branch == nil {
		t.Fatalf("comparison tab %q was not opened", key)
	}
	if d.branch.err != "" {
		t.Fatalf("comparison tab for base %q failed: %s", d.branch.baseRef, d.branch.err)
	}
	if d.branch.result == nil {
		t.Fatalf("comparison tab for base %q completed without a result: %+v", d.branch.baseRef, d.branch)
	}
	return d.branch
}

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
	panel := settleBranchDiff(t, h)
	if h.a.git.branch != "main" || git.Head(context.Background(), root) != head {
		t.Fatalf("base selection changed HEAD: branch %q, HEAD %q, want %q", h.a.git.branch, git.Head(context.Background(), root), head)
	}
	key := branchDiffTab("refs/heads/base")
	v := settleBranchDiffTab(t, h, h.a.focus, key)
	if panel.baseRef != "refs/heads/base" || h.a.focus.active != key {
		t.Fatalf("base selection did not open its comparison: panel %+v, active %q", panel, h.a.focus.active)
	}
	if !h.tt.HasText("Base: base") || !h.tt.HasText(branchDiffText("gitWorkingTree")) || h.tt.HasText("View Diff") || h.tt.HasText(branchDiffText("gitReadOnly")) {
		t.Fatalf("comparison controls are misplaced: %v", h.tt.Texts())
	}
	baseButtons := 0
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleButton && access.Name == branchDiffText("gitChangeBase") {
			baseButtons++
		}
	}
	if baseButtons != 1 {
		t.Fatalf("comparison has %d base controls, want one", baseButtons)
	}
	if h.tt.HasText("Stage All Changes") || h.tt.HasText("Unstage All Changes") {
		t.Fatalf("comparison mode exposes staging controls: %v", h.tt.Texts())
	}

	if err := h.tt.Click("zz-target.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	v = h.a.focus.diffs[key].branch
	if h.a.focus.active != key || v == nil || v.jumpPath != "" || v.selectedPath != "zz-target.txt" || panel.selectedPath != "zz-target.txt" || v.scroll.Y <= 0 || v.shown <= branchDiffFilesAtOnce || v.collapsed["zz-target.txt"] {
		t.Fatalf("file click did not navigate the shared diff: active %q, view %+v", h.a.focus.active, v)
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
	if err := h.tt.Click("Close Since base"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.focus.has(key) {
		t.Fatalf("comparison tab %q remained open after close", key)
	}
	if err := h.tt.Click("zz-target.txt"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	v = h.a.focus.diffs[key].branch
	if h.a.focus.active != key || v == nil || v.selectedPath != "zz-target.txt" {
		t.Fatalf("clicking a navigator file did not reopen its comparison: active %q, view %+v", h.a.focus.active, v)
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
	panel = settleBranchDiff(t, h)
	otherKey := branchDiffTab("refs/heads/other")
	settleBranchDiffTab(t, h, h.a.focus, otherKey)
	if panel.baseRef != "refs/heads/other" || h.a.focus.active != otherKey || !h.a.focus.has(key) || h.a.focus.diffs[key].branch.baseRef != "refs/heads/base" {
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
	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.git.branchDiffMode != compareBaseMode || h.a.focus.active != otherKey {
		t.Fatalf("returning to comparison mode did not reopen its tab: mode %d active %q", h.a.git.branchDiffMode, h.a.focus.active)
	}
}

func TestBranchDiffCompareModeReopensClosedEmptyTab(t *testing.T) {
	root := gitRepo(t, map[string]string{"same.txt": "same\n"}, "base")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
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
	panel := settleBranchDiff(t, h)
	key := branchDiffTab("refs/heads/base")
	settleBranchDiffTab(t, h, h.a.focus, key)
	if len(panel.files) != 0 || h.a.focus.active != key || !h.tt.HasText(branchDiffText("gitNoBranchChanges")) {
		t.Fatalf("empty comparison did not open: panel %+v, active %q, text %v", panel, h.a.focus.active, h.tt.Texts())
	}
	if err := h.tt.Click("Close Since base"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.focus.has(key) {
		t.Fatalf("empty comparison tab %q remained open after close", key)
	}
	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	if h.a.git.branchDiffMode != compareBaseMode || h.a.focus.active != key || !h.a.focus.has(key) {
		t.Fatalf("returning to comparison mode did not reopen the empty tab: mode %d active %q order %v", h.a.git.branchDiffMode, h.a.focus.active, h.a.focus.order)
	}
}

func TestBranchDiffPickerCancellationCanBeRecoveredFromNavigator(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "base")
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.tt.Frame()

	if err := h.tt.Click("Uncommitted"); err != nil {
		t.Fatal(err)
	}
	if err := h.tt.Click("Compare Against Base"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()
	if h.a.git.picker.open || h.a.git.branchPanel != nil {
		t.Fatal("cancelling the initial base picker unexpectedly selected a base")
	}
	access, ok := h.tt.Access(branchDiffText("gitChooseBaseBranch"))
	if !ok || access.Role != ui.RoleButton {
		t.Fatalf("navigator has no recoverable base picker button: %v", h.tt.AccessAll())
	}
	if err := h.tt.Click(branchDiffText("gitChooseBaseBranch")); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
	if !h.a.git.picker.open || !h.a.git.picker.compare {
		t.Fatal("navigator base button did not reopen comparison picker")
	}
	h.tt.Type("base")
	h.tt.Frame()
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	panel := settleBranchDiff(t, h)
	if panel.baseRef != "refs/heads/base" {
		t.Fatalf("navigator recovery selected base %q, want refs/heads/base", panel.baseRef)
	}
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleButton && access.Name == branchDiffText("gitChooseBaseBranch") {
			t.Fatal("navigator retained the base picker button after selection")
		}
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

func TestCompareBasePickerClampsToWindowEdges(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "base")
	h := newHarness(t, root)
	h.readGit()
	h.a.openBaseBranchPicker(ui.Rect{X: harnessWidth - 16, Y: harnessHeight - 32, W: 16, H: 24}, nil)
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Frame()

	field, ok := h.tt.Access(branchDiffText("gitChooseBaseBranch"))
	if !ok {
		t.Fatalf("comparison picker field is not visible: %v", h.tt.Texts())
	}
	b := field.Bounds
	if b.X < 8 || b.Y < 8 || b.X+b.W > harnessWidth-8 || b.Y+b.H > harnessHeight-8 || b.W < 200 {
		t.Fatalf("comparison picker field is clipped at the window edge: %+v", b)
	}
	branch, ok := h.tt.Find("main")
	if !ok || branch.X < 8 || branch.Y < 8 || branch.X+branch.W > harnessWidth-8 || branch.Y+branch.H > harnessHeight-8 {
		t.Fatalf("comparison picker branch is clipped at the window edge: %+v, visible %v", branch, ok)
	}
}

func TestCompareBasePickerAnchorsShortFilteredListToFooter(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "base\n"}, "base")
	h := newHarness(t, root)
	h.readGit()
	anchor := ui.Rect{X: harnessWidth / 2, Y: harnessHeight - 36, W: 120, H: 26}
	h.a.openBaseBranchPicker(anchor, nil)
	h.settle(func() bool { return !h.a.git.picker.loading })
	h.tt.Frame()
	h.tt.Type("main")
	h.tt.Frame()
	if items := h.a.branchItems(); len(items) != 2 || items[1].branch == nil || items[1].branch.Name != "main" {
		t.Fatalf("filtered comparison branches = %+v, want one main branch", h.a.branchItems())
	}
	list, ok := h.tt.Access("Branches")
	if !ok {
		t.Fatalf("filtered branch list is not visible: %v", h.tt.Texts())
	}
	gap := anchor.Y - 6 - (list.Bounds.Y + list.Bounds.H)
	if gap < 0 || gap > 32 {
		t.Fatalf("short picker list is not bottom-anchored to its control: gap %.1f, list %+v, anchor %+v", gap, list.Bounds, anchor)
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

func TestBranchDiffNavigatorShowsComparisonError(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "same\n"})
	h := newHarness(t, root)
	h.readGit()
	h.a.navigator, h.a.git.historyClosed = navGit, true
	h.a.git.branchDiffMode = compareBaseMode
	h.a.git.branchPanel = &branchDiffView{
		baseRef:  "refs/heads/missing",
		baseName: "missing",
		err:      "resolve base failed",
	}
	h.tt.Frame()
	if !h.tt.HasText("resolve base failed") || h.tt.HasText("View Diff") {
		t.Fatalf("comparison error state = %v", h.tt.Texts())
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
	if original == nil || !original.loading || original.selectedPath != "a.txt" {
		t.Fatalf("expected original comparison to be loading: %+v", original)
	}
	h.a.splitActive(sideRight)
	cloneGroup := h.a.focus
	clone := cloneGroup.diffs[key].branch
	if clone == nil || clone == original || !clone.loading || clone.files != nil || clone.selectedPath != original.selectedPath {
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
