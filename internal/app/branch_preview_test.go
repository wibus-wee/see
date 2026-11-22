package app

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/git"
	"see/locales"
)

func openSwitchBranchPicker(t *testing.T, h *harness) {
	t.Helper()
	if err := h.tt.Click("Switch Branch"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.git.picker.loading })
}

func hoverBranch(t *testing.T, h *harness, name string) {
	t.Helper()
	r, ok := h.tt.Find(name)
	if !ok {
		t.Fatalf("branch %q is not shown: %v", name, h.tt.Texts())
	}
	h.tt.Move(r.X+r.W/2, r.Y+r.H/2)
}

func TestBranchPickerHoverPreviewShowsOpenFileChangesWithoutSwitching(t *testing.T) {
	root := gitRepo(t, map[string]string{
		"sub/modified.go":   "package sub\n\nvar value = 1\n",
		"sub/deleted.go":    "package sub\n\nvar deleted = true\n",
		"sub/old.go":        "package sub\n\nfunc RenameMe() {\n\treturn\n}\n",
		"sub/unaffected.go": "package sub\n\nvar stable = true\n",
		"outside.go":        "package main\n\nvar outside = 1\n",
	}, "feature")
	gitRun(t, root, "switch", "-q", "feature")
	writeFile(t, root, "sub/modified.go", "package sub\n\nvar value = 2\n")
	gitRun(t, root, "rm", "--", "sub/deleted.go")
	gitRun(t, root, "mv", "--", "sub/old.go", "sub/new.go")
	writeFile(t, root, "outside.go", "package main\n\nvar outside = 2\n")
	writeFile(t, root, "sub/added.go", "package sub\n\nvar added = true\n")
	gitRun(t, root, "add", "--all")
	gitRun(t, root, "commit", "-qm", "Change files on feature")
	gitRun(t, root, "switch", "-q", "main")
	head := git.Head(context.Background(), root)

	workspaceRoot := filepath.Join(root, "sub")
	if runtime.GOOS != "windows" {
		workspaceAlias := filepath.Join(t.TempDir(), "repo")
		if err := os.Symlink(root, workspaceAlias); err != nil {
			t.Fatal(err)
		}
		workspaceRoot = filepath.Join(workspaceAlias, "sub")
	}
	h := newHarness(t, workspaceRoot)
	h.readGit()
	modified := h.open("modified.go")
	modifiedPath := modified.path
	h.open("old.go")
	h.a.focus.eds[modifiedPath].Insert("// unsaved edit\n")
	h.a.splitActive(sideRight)
	h.open("deleted.go")
	h.open("unaffected.go")
	outsidePath := filepath.Join(root, "outside.go")
	h.a.userOpen(outsidePath)
	outside := h.a.docs[outsidePath]
	h.settle(func() bool { return outside != nil && !outside.loading })
	virtual := diffTab("sub/modified.go", false)
	h.a.focus.addTab(virtual, len(h.a.focus.order))
	h.tt.Frame()

	openSwitchBranchPicker(t, h)
	hoverBranch(t, h, "feature")
	h.tt.Move(0, 0)
	p := &h.a.git.picker
	for i := 0; i < 3; i++ {
		h.settle(func() bool {
			return !p.previewLoading && ((p.preview != nil && p.preview.TargetRef == "refs/heads/feature") || p.previewErr != "")
		})
		if p.previewErr != "" {
			t.Fatal(p.previewErr)
		}
		if p.preview != nil && p.preview.TargetRef == "refs/heads/feature" && h.tt.HasText(branchPreviewText("gitBranchPreviewUnsaved")) {
			break
		}
	}
	if p.previewErr != "" {
		t.Fatal(p.previewErr)
	}
	if p.previewLoading || p.preview == nil || p.preview.TargetRef != "refs/heads/feature" || !h.tt.HasText(branchPreviewText("gitBranchPreviewUnsaved")) {
		t.Fatalf("preview did not remain settled after rendering: target %q, loading %v, selection %d, hovered %q, pointer %d/%d, query %q/%q", p.previewTarget, p.previewLoading, p.sel, p.hoveredBranchKey, p.pointerMoveSeq, p.hoveredBranchSeq, p.query, p.appliedQuery)
	}
	preview := p.preview
	previewFiles := append([]branchPreviewFile(nil), p.previewFiles...)
	previewOpenCount := p.previewOpenCount
	if preview == nil {
		t.Fatalf("preview disappeared after settling: target %q, loading %v", p.previewTarget, p.previewLoading)
	}
	if preview.TargetRef != "refs/heads/feature" || previewOpenCount != 4 || len(previewFiles) != 3 {
		t.Fatalf("preview target/open files = %q/%d/%d: %+v", preview.TargetRef, previewOpenCount, len(previewFiles), previewFiles)
	}
	paths := map[string]branchPreviewFile{}
	for _, file := range previewFiles {
		paths[file.Path] = file
	}
	if file, ok := paths["sub/modified.go"]; !ok || !file.unsaved || file.Status != 'M' {
		t.Errorf("modified open file = %+v, present %v", file, ok)
	}
	if file, ok := paths["sub/deleted.go"]; !ok || file.Status != 'D' {
		t.Errorf("deleted open file = %+v, present %v", file, ok)
	}
	if file, ok := paths["sub/new.go"]; !ok || file.Status != 'R' || file.OldPath != "sub/old.go" {
		t.Errorf("renamed open file = %+v, present %v", file, ok)
	}
	if _, ok := paths["outside.go"]; ok {
		t.Error("open file outside the workspace was included")
	}
	if _, ok := paths["sub/unaffected.go"]; ok {
		t.Error("unchanged open file was included")
	}
	if !h.tt.HasText(branchPreviewText("gitBranchPreviewBasis")) || !h.tt.HasText(branchPreviewText("gitBranchPreviewDirtyWarning")) || !h.tt.HasText(branchPreviewText("gitBranchPreviewUnsaved")) {
		t.Fatalf("preview contents do not describe the committed comparison and unsaved document: %v", h.tt.Texts())
	}
	if got := git.Head(context.Background(), root); got != head || h.a.git.branch != "main" {
		t.Fatalf("hover switched branches: branch %q, HEAD %q (want %q)", h.a.git.branch, got, head)
	}
}

func TestBranchPickerKeyboardSelectionUpdatesPreview(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	head := git.Head(context.Background(), root)
	openSwitchBranchPicker(t, h)
	h.tt.Key(0, ui.KeyDown)
	h.settle(func() bool {
		p := &h.a.git.picker
		return p.preview != nil && !p.previewLoading && p.preview.TargetRef == "refs/heads/feature"
	})
	if h.a.git.picker.previewBranch.Name != "feature" || git.Head(context.Background(), root) != head {
		t.Fatalf("keyboard selection previewed %q, HEAD %q (was %q)", h.a.git.picker.previewBranch.Name, git.Head(context.Background(), root), head)
	}
	h.tt.Key(0, ui.KeyUp)
	h.settle(func() bool {
		p := &h.a.git.picker
		return p.preview != nil && !p.previewLoading && p.preview.TargetRef == "refs/heads/main"
	})
	if h.a.git.picker.previewBranch.Name != "main" || git.Head(context.Background(), root) != head {
		t.Fatalf("reverse keyboard selection previewed %q, HEAD %q (was %q)", h.a.git.picker.previewBranch.Name, git.Head(context.Background(), root), head)
	}
}

func TestBranchPickerHoverKeepsRowBoundsStableWhilePreviewLoads(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	gitRun(t, root, "switch", "-q", "feature")
	for _, name := range []string{"one.go", "two.go", "three.go"} {
		writeFile(t, root, name, "package feature\n")
		gitRun(t, root, "add", "--", name)
		gitRun(t, root, "commit", "-qm", "add "+name)
	}
	gitRun(t, root, "switch", "-q", "main")

	for _, size := range []struct {
		name          string
		width, height int
		anchorY       int
	}{
		{name: "wide", width: 1100, height: 700},
		{name: "stacked", width: 620, height: 700},
		{name: "above", width: 1100, height: 700, anchorY: 648},
	} {
		t.Run(size.name, func(t *testing.T) {
			h := newHarness(t, root)
			h.tt = ui.NewTester(h.a.view, size.width, size.height)
			h.readGit()
			openSwitchBranchPicker(t, h)

			p := &h.a.git.picker
			h.settle(func() bool {
				return !p.previewLoading && (p.preview != nil || p.previewErr != "")
			})
			if size.anchorY != 0 {
				p.at.Y = float32(size.anchorY)
				h.tt.Frame()
				_, above, _ := branchPickerBounds(p.at, float32(size.width), float32(size.height))
				if !above {
					t.Fatal("picker did not open above its anchor")
				}
				panel := branchPickerPanelAccess(t, h)
				if panel.Bounds.Y < 8 || panel.Bounds.Y+panel.Bounds.H > float32(size.height-8) {
					t.Fatalf("above-anchor popover escaped viewport: %+v", panel.Bounds)
				}
			}
			if p.previewErr != "" {
				t.Fatal(p.previewErr)
			}
			before, ok := h.tt.Find("feature")
			if !ok {
				t.Fatal("feature branch is not shown")
			}
			x, y := before.X+before.W/2, before.Y+before.H/2
			h.tt.Move(x, y)
			loading, ok := h.tt.Find("feature")
			if !ok || loading != before {
				t.Fatalf("branch row moved when preview began loading: before %+v, loading %+v, found %v", before, loading, ok)
			}
			if !p.previewLoading || p.previewBranch.Name != "feature" {
				t.Fatalf("hover did not start feature preview: branch %q, loading %v", p.previewBranch.Name, p.previewLoading)
			}

			h.settle(func() bool {
				return !p.previewLoading && (p.preview != nil && p.preview.TargetRef == "refs/heads/feature" || p.previewErr != "")
			})
			if p.previewErr != "" {
				t.Fatal(p.previewErr)
			}
			if p.preview == nil || len(p.preview.Commits) != 3 {
				t.Fatalf("feature preview is incomplete: %+v", p.preview)
			}
			for i := 0; i < 3; i++ {
				h.tt.Frame()
				got, found := h.tt.Find("feature")
				if !found || got != before {
					t.Fatalf("branch row moved after preview frame %d: before %+v, got %+v, found %v", i+1, before, got, found)
				}
				if x < got.X || x > got.X+got.W || y < got.Y || y > got.Y+got.H {
					t.Fatalf("stationary pointer left branch row at frame %d: pointer %g,%g, row %+v", i+1, x, y, got)
				}
				if p.previewLoading || p.preview == nil || p.preview.TargetRef != "refs/heads/feature" {
					t.Fatalf("preview changed after frame %d: target %q loading %v", i+1, p.previewTarget, p.previewLoading)
				}
				if size.anchorY != 0 {
					panel := branchPickerPanelAccess(t, h)
					if panel.Bounds.Y < 8 || panel.Bounds.Y+panel.Bounds.H > float32(size.height-8) {
						t.Fatalf("above-anchor popover escaped viewport after frame %d: %+v", i+1, panel.Bounds)
					}
				}
			}
		})
	}
}

func TestBranchPickerWideColumnsAlignAtTopAndFavorPreview(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.tt = ui.NewTester(h.a.view, 1100, 700)
	h.readGit()
	openSwitchBranchPicker(t, h)

	p := &h.a.git.picker
	h.settle(func() bool {
		return !p.previewLoading && (p.preview != nil || p.previewErr != "")
	})
	if p.previewErr != "" {
		t.Fatal(p.previewErr)
	}

	branchList, ok := h.tt.AccessByRole(ui.RoleList, "Branches")
	if !ok {
		t.Fatal("branch list is not exposed to the UI tester")
	}
	var branchHeader ui.AccessInfo
	var previewTitle ui.AccessInfo
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleText && access.Name == "Branches" && access.Bounds.X >= branchList.Bounds.X && access.Bounds.X < branchList.Bounds.X+branchList.Bounds.W {
			branchHeader = access
		}
		if access.Role == ui.RoleText && access.Name == p.previewBranch.Name && access.Bounds.X >= branchList.Bounds.X+branchList.Bounds.W {
			previewTitle = access
		}
	}
	if branchHeader.Name == "" {
		t.Fatalf("visible branch list heading is not shown: %+v", h.tt.AccessAll())
	}
	if previewTitle.Name == "" {
		t.Fatalf("preview title %q is not shown in the preview column: %+v", p.previewBranch.Name, h.tt.AccessAll())
	}
	branchHeaderTextY := branchHeader.Bounds.Y + 8
	if branchHeaderTextY > previewTitle.Bounds.Y+2 || previewTitle.Bounds.Y > branchHeaderTextY+2 {
		t.Fatalf("branch and preview headers are not top-aligned: branches %+v (text y %g), preview %+v, list %+v", branchHeader.Bounds, branchHeaderTextY, previewTitle.Bounds, branchList.Bounds)
	}
	if branchList.Bounds.W < 260 || branchList.Bounds.W > 300 {
		t.Fatalf("branch list width = %g, want about 280 DIPs", branchList.Bounds.W)
	}
}

func TestBranchPickerHugsShortContent(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n\nvar value = 1\n"}, "feature")
	gitRun(t, root, "switch", "-q", "feature")
	writeFile(t, root, "a.go", "package a\n\nvar value = 2\n")
	gitRun(t, root, "add", "--", "a.go")
	gitRun(t, root, "commit", "-qm", "update a.go")
	gitRun(t, root, "switch", "-q", "main")

	h := newHarness(t, root)
	h.tt = ui.NewTester(h.a.view, 1100, 700)
	h.readGit()
	h.open("a.go")
	openSwitchBranchPicker(t, h)
	hoverBranch(t, h, "feature")
	p := &h.a.git.picker
	h.settle(func() bool {
		return !p.previewLoading && p.preview != nil && p.preview.TargetRef == "refs/heads/feature"
	})

	panel := branchPickerPanelAccess(t, h)
	if panel.Bounds.H >= 400 {
		t.Fatalf("short preview popover remained tall: %+v", panel.Bounds)
	}
	contentBottom := branchPickerContentBottom(h, panel.Bounds)
	blank := panel.Bounds.Y + panel.Bounds.H - contentBottom
	if blank > 24 {
		t.Fatalf("short preview leaves %g DIPs below its content: panel %+v, content bottom %g", blank, panel.Bounds, contentBottom)
	}
}

func TestBranchPickerCapsLongContentInNarrowViewport(t *testing.T) {
	files := make(map[string]string, 12)
	for i := 0; i < 12; i++ {
		files[fmt.Sprintf("file-%02d.go", i)] = "package a\n\nvar value = 1\n"
	}
	root := gitRepo(t, files, "feature")
	gitRun(t, root, "switch", "-q", "feature")
	for name := range files {
		writeFile(t, root, name, "package a\n\nvar value = 2\n")
	}
	gitRun(t, root, "add", "--all")
	gitRun(t, root, "commit", "-qm", "update files")
	gitRun(t, root, "switch", "-q", "main")
	for i := 0; i < 40; i++ {
		gitRun(t, root, "branch", fmt.Sprintf("long-branch-%02d", i))
	}

	h := newHarness(t, root)
	h.tt = ui.NewTester(h.a.view, 620, 380)
	h.readGit()
	for name := range files {
		h.open(name)
	}
	openSwitchBranchPicker(t, h)
	hoverBranch(t, h, "feature")
	p := &h.a.git.picker
	h.settle(func() bool {
		return !p.previewLoading && p.preview != nil && p.preview.TargetRef == "refs/heads/feature"
	})

	panel := branchPickerPanelAccess(t, h)
	maxBounds, _, stacked := branchPickerBounds(p.at, 620, 380)
	if !stacked {
		t.Fatal("narrow picker did not stack its columns")
	}
	if panel.Bounds.Y < 8 || panel.Bounds.Y+panel.Bounds.H > 372 || panel.Bounds.W > 604 {
		t.Fatalf("narrow popover escaped viewport: panel %+v", panel.Bounds)
	}
	if panel.Bounds.H > maxBounds.H+1 {
		t.Fatalf("narrow popover exceeded its viewport cap: panel %+v, cap %+v", panel.Bounds, maxBounds)
	}
	list, ok := h.tt.AccessByRole(ui.RoleList, locales.MsgBranches())
	if !ok {
		t.Fatal("branch list is not exposed to the UI tester")
	}
	listCap := min(float32(250), max(0, maxBounds.H-74)*0.45)
	if list.SetSize < 40 || list.Bounds.H > listCap+2 || list.Bounds.H < listCap-20 {
		t.Fatalf("long branch list was not capped to its narrow viewport: list %+v, cap %g", list, listCap)
	}
	if len(p.previewFiles) != len(files) {
		t.Fatalf("preview has %d open changed files, want %d", len(p.previewFiles), len(files))
	}
}

func branchPickerPanelAccess(t *testing.T, h *harness) ui.AccessInfo {
	t.Helper()
	list, ok := h.tt.AccessByRole(ui.RoleList, locales.MsgBranches())
	if !ok {
		t.Fatal("branch list is not exposed to the UI tester")
	}
	for _, access := range h.tt.AccessAll() {
		if access.Role == ui.RoleGroup && access.Name == locales.MsgBranches() &&
			access.Bounds.X <= list.Bounds.X && access.Bounds.Y <= list.Bounds.Y &&
			access.Bounds.X+access.Bounds.W >= list.Bounds.X+list.Bounds.W &&
			access.Bounds.Y+access.Bounds.H >= list.Bounds.Y+list.Bounds.H {
			return access
		}
	}
	t.Fatalf("branch picker bounds are not exposed to the UI tester: %+v", h.tt.AccessAll())
	return ui.AccessInfo{}
}

func branchPickerContentBottom(h *harness, panel ui.Rect) float32 {
	var bottom float32 = panel.Y
	for _, access := range h.tt.AccessAll() {
		if access.Role != ui.RoleText && access.Role != ui.RoleListItem {
			continue
		}
		if access.Bounds.X < panel.X || access.Bounds.X >= panel.X+panel.W || access.Bounds.Y < panel.Y || access.Bounds.Y >= panel.Y+panel.H {
			continue
		}
		bottom = max(bottom, access.Bounds.Y+access.Bounds.H)
	}
	return bottom
}

func TestBranchPickerKeyboardSelectionWinsOverStationaryHover(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	openSwitchBranchPicker(t, h)
	items := h.a.branchItems()
	indices := map[string]int{}
	for i, item := range items {
		if item.branch != nil {
			indices[item.branch.Name] = i
		}
	}
	featureIndex, featureOK := indices["feature"]
	mainIndex, mainOK := indices["main"]
	if !featureOK || !mainOK {
		t.Fatalf("branch items = %+v", items)
	}
	featureBounds, ok := h.tt.Find("feature")
	if !ok {
		t.Fatal("feature branch is not shown")
	}
	hoverBranch(t, h, "feature")
	p := &h.a.git.picker
	if p.sel != featureIndex || p.previewBranch.Name != "feature" {
		t.Fatalf("hover did not select its branch: selected %d, preview %q", p.sel, p.previewBranch.Name)
	}
	h.tt.Key(0, ui.KeyUp)
	if p.sel != mainIndex || p.previewBranch.Name != "main" {
		t.Fatalf("keyboard Up was overridden by stationary hover: selected %d, preview %q", p.sel, p.previewBranch.Name)
	}
	h.tt.Key(0, ui.KeyDown)
	if p.sel != featureIndex || p.previewBranch.Name != "feature" {
		t.Fatalf("keyboard Down was overridden by stationary hover: selected %d, preview %q", p.sel, p.previewBranch.Name)
	}
	h.tt.Key(0, ui.KeyUp)
	if p.sel != mainIndex || p.previewBranch.Name != "main" {
		t.Fatalf("second keyboard Up was overridden by stationary hover: selected %d, preview %q", p.sel, p.previewBranch.Name)
	}
	h.tt.Move(featureBounds.X+featureBounds.W/2+1, featureBounds.Y+featureBounds.H/2)
	if p.sel != featureIndex || p.previewBranch.Name != "feature" {
		t.Fatalf("actual pointer movement did not restore the hovered branch: selected %d, preview %q", p.sel, p.previewBranch.Name)
	}
	h.settle(func() bool {
		return !p.previewLoading && p.preview != nil && p.preview.TargetRef == "refs/heads/feature"
	})
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	h.readGit()
	if h.a.git.branch != "feature" || p.open {
		t.Fatalf("Enter chose %q instead of the previewed feature branch; picker open %v", h.a.git.branch, p.open)
	}
}

func TestBranchPickerDropsLatePreviewAfterSelectionChanges(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	openSwitchBranchPicker(t, h)
	t.Cleanup(h.a.closeBranchPicker)
	items := h.a.branchItems()
	var main, feature branchItem
	for _, item := range items {
		if item.branch == nil {
			continue
		}
		switch item.branch.Name {
		case "main":
			main = item
		case "feature":
			feature = item
		}
	}
	if main.branch == nil || feature.branch == nil {
		t.Fatalf("branch list = %+v", items)
	}
	h.a.clearBranchPreview()
	h.a.previewBranchItem(feature)
	oldKey, oldSeq, gen := h.a.git.picker.previewTarget, h.a.git.picker.previewSeq, h.a.gen
	h.a.previewBranchItem(main)
	h.tt.Frame()
	p := &h.a.git.picker
	if p.previewTarget != branchPreviewKey(*main.branch) || !p.previewLoading || p.preview != nil {
		t.Fatalf("new selection state = target %q, loading %v, preview %+v", p.previewTarget, p.previewLoading, p.preview)
	}
	h.a.finishBranchPreview(oldKey, gen, oldSeq, context.Background(), git.BranchPreview{TargetHash: "stale"}, nil, nil, nil)
	h.tt.Frame()
	if p.previewTarget != branchPreviewKey(*main.branch) || !p.previewLoading || p.preview != nil || p.previewErr != "" {
		t.Fatalf("late preview replaced the newer selection: target %q, loading %v, preview %+v, error %q", p.previewTarget, p.previewLoading, p.preview, p.previewErr)
	}
}

func TestBranchPickerPreviewEmptyCompareAndCreateRegressions(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	openSwitchBranchPicker(t, h)
	hoverBranch(t, h, "feature")
	h.tt.Move(0, 0)
	h.settle(func() bool {
		p := &h.a.git.picker
		return !p.previewLoading && ((p.preview != nil && p.preview.TargetRef == "refs/heads/feature") || p.previewErr != "")
	})
	if h.a.git.picker.previewErr != "" {
		t.Fatal(h.a.git.picker.previewErr)
	}
	if !h.tt.HasText(branchPreviewText("gitBranchPreviewNoChanges")) || !h.tt.HasText(branchPreviewText("gitBranchPreviewNoOpenFiles")) {
		t.Fatalf("empty preview states are missing: branch %q target %q loading %v preview %+v error %q: %v", h.a.git.picker.previewBranch.Name, h.a.git.picker.previewTarget, h.a.git.picker.previewLoading, h.a.git.picker.preview, h.a.git.picker.previewErr, h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()

	h.a.openBaseBranchPicker(h.a.git.picker.anchor, nil)
	h.settle(func() bool { return !h.a.git.picker.loading })
	hoverBranch(t, h, "feature")
	if !h.a.git.picker.compare || h.a.git.picker.previewLoading || h.a.git.picker.preview != nil || h.a.git.picker.previewTarget != "" {
		t.Fatalf("compare picker started a switch preview: %+v", h.a.git.picker)
	}
	h.tt.Key(0, ui.KeyEscape)
	h.tt.Frame()

	openSwitchBranchPicker(t, h)
	h.tt.Type("draft/topic")
	h.tt.Frame()
	if !h.tt.HasText("Create Branch “draft/topic”") || h.a.git.picker.previewLoading || h.a.git.picker.preview != nil || h.a.git.picker.previewBranch.Name != "" {
		t.Fatalf("new branch query retained a target preview: %v", h.tt.Texts())
	}
	h.tt.Key(0, ui.KeyEnter)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.busy })
	h.readGit()
	if h.a.git.branch != "draft/topic" || h.a.git.picker.open {
		t.Fatalf("branch creation regressed: current %q, picker open %v", h.a.git.branch, h.a.git.picker.open)
	}
}

func TestBranchPickerRejectsBranchPreviewError(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.go": "package a\n"}, "feature")
	h := newHarness(t, root)
	h.readGit()
	openSwitchBranchPicker(t, h)
	items := h.a.branchItems()
	var feature branchItem
	for _, item := range items {
		if item.branch != nil && item.branch.Name == "feature" {
			feature = item
			break
		}
	}
	if feature.branch == nil {
		t.Fatalf("branch list = %+v", items)
	}
	gitRun(t, root, "branch", "-D", "feature")
	h.a.previewBranchItem(feature)
	h.settle(func() bool {
		return !h.a.git.picker.previewLoading && h.a.git.picker.previewErr != ""
	})
	if !h.tt.HasText(branchPreviewText("gitBranchPreviewError")) || !strings.Contains(h.a.git.picker.previewErr, "feature") {
		t.Fatalf("preview error state = %q, texts %v", h.a.git.picker.previewErr, h.tt.Texts())
	}
}

func TestBranchPickerBoundsFitAndStackInNarrowWindows(t *testing.T) {
	for _, size := range []struct {
		width, height float32
		stack         bool
	}{
		{1100, 700, false},
		{600, 700, true},
		{320, 200, true},
	} {
		at := ui.Rect{X: size.width - 32, Y: size.height - 40, W: 24, H: 28}
		bounds, _, stack := branchPickerBounds(at, size.width, size.height)
		if stack != size.stack || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.W > size.width || bounds.Y+bounds.H > size.height {
			t.Errorf("picker bounds for %gx%g = %+v, stack %v", size.width, size.height, bounds, stack)
		}
	}
}
