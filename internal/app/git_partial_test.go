package app

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

func TestGitPartButtonsMoveOnlyOneHunk(t *testing.T) {
	const name = "source.go"
	gap := strings.Repeat("context\n", 8)
	base := "first\n" + gap + "middle\n" + gap + "last\n"
	index := strings.Replace(base, "middle", "MIDDLE", 1)
	work := strings.ReplaceAll(strings.ReplaceAll(index, "last", "LAST"), "first", "FIRST")
	root := gitRepo(t, map[string]string{name: base})
	writeFile(t, root, name, index)
	mustPartialGit(t, root, "add", "--", name)
	writeFile(t, root, name, work)
	h := newHarness(t, root)
	h.readGit()
	open := func(staged bool) *gitDiffTab {
		h.a.showGitDiff(h.a.git.changes[0], staged)
		h.settle(func() bool { return h.a.focus.active == diffTab(name, staged) })
		h.tt.Frame()
		return h.a.focus.diffs[diffTab(name, staged)]
	}
	stagedView := open(true)
	workingView := open(false)
	clickPartialGit(t, h, "Next Change")
	clickPartialGit(t, h, "Stage Hunk")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	checkPartialGit(t, root, name, strings.ReplaceAll(index, "last", "LAST"), work)
	if got := len(workingView.state.Hunks()); got != 1 {
		t.Fatalf("working diff still has %d hunks", got)
	}
	if got := len(stagedView.state.Hunks()); got != 2 {
		t.Fatalf("already-open staged diff has %d hunks, want 2", got)
	}
	originalGroup := h.a.focus
	h.a.splitActive(sideRight)
	clone := h.a.focus.diffs[diffTab(name, false)]
	if clone == workingView || clone.state == workingView.state || clone.partial == nil {
		t.Fatal("split did not retain partial staging with independent UI state")
	}
	h.a.closeTab(h.a.focus, diffTab(name, false))
	h.a.focus = originalGroup
	h.a.userActivateIn(h.a.focus, diffTab(name, true))
	h.tt.Frame()
	clickPartialGit(t, h, "Next Change")
	clickPartialGit(t, h, "Unstage Hunk")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	checkPartialGit(t, root, name, index, work)
}

func TestGitPartButtonStagesGutterSelection(t *testing.T) {
	for _, layout := range []string{"split", "unified"} {
		t.Run(layout, func(t *testing.T) {
			const name = "source.go"
			const work = "start\none\ntwo\nthree\nend\n"
			root := gitRepo(t, map[string]string{name: "start\nend\n"})
			writeFile(t, root, name, work)
			h := newHarness(t, root)
			h.tt.SetSize(1280, 800)
			h.a.cfg.DiffLayout = layout
			h.readGit()
			h.a.showGitDiff(h.a.git.changes[0], false)
			h.settle(func() bool { return h.a.focus.active == diffTab(name, false) })
			h.tt.Frame()
			pane, ok := h.tt.Find("New text")
			if !ok {
				t.Fatalf("new diff pane is missing: %q", h.tt.Texts())
			}
			h.tt.ClickAt(pane.X+4, pane.Y+36)
			h.tt.ClickAtWith(ui.Shift, pane.X+4, pane.Y+55)
			clickPartialGit(t, h, "Stage Selected Lines")
			h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
			checkPartialGit(t, root, name, "start\none\ntwo\nend\n", work)
			h.a.showGitDiff(h.a.git.changes[0], true)
			h.settle(func() bool { return h.a.focus.active == diffTab(name, true) })
			h.tt.Frame()
			pane, ok = h.tt.Find("New text")
			if !ok {
				t.Fatal("the staged diff pane is missing")
			}
			h.tt.ClickAt(pane.X+4, pane.Y+55)
			clickPartialGit(t, h, "Unstage Selected Lines")
			h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
			checkPartialGit(t, root, name, "start\none\nend\n", work)
		})
	}
}

func TestGitPartStagesAndUnstagesExactLines(t *testing.T) {
	for _, tt := range []struct {
		name, base, work, staged string
		exists                   bool
		side                     editor.DiffSide
	}{
		{"untracked file", "", "one\ntwo\nthree", "two\n", false, editor.DiffNew},
		{"normalized untracked file", "", "one\r\ntwo\r\nthree\r\n", "two\n", false, editor.DiffNew},
		{"deleted file", "one\ntwo\nthree", "", "one\nthree", true, editor.DiffOld},
		{"CRLF and no final newline", "one\r\nold", "one\r\nnew", "one\r\nold\nnew", true, editor.DiffNew},
		{"only a selected removal", "one\nold\nend\n", "one\nnew\nend\n", "one\nend\n", true, editor.DiffOld},
	} {
		t.Run(tt.name, func(t *testing.T) {
			const name = "résumé with space.txt"
			root := gitRepo(t, map[string]string{"other": "base\n"})
			mustPartialGit(t, root, "config", "core.autocrlf", "false")
			if tt.name == "normalized untracked file" {
				writeFile(t, root, ".gitattributes", "*.txt text eol=lf\n")
			}
			if tt.exists {
				writeFile(t, root, name, tt.base)
				mustPartialGit(t, root, "add", "--", name)
				mustPartialGit(t, root, "commit", "-qm", "initial file")
			}
			if tt.work == "" {
				if err := os.Remove(filepath.Join(root, name)); err != nil {
					t.Fatal(err)
				}
			} else {
				writeFile(t, root, name, tt.work)
			}
			part := gitDiffPart{selection: true, lines: editor.DiffLineRange{Side: tt.side, Start: 1, End: 1}}
			applyPartialGit(t, root, name, false, part)
			checkPartialGit(t, root, name, tt.staged, tt.work)
			if tt.name == "CRLF and no final newline" {
				return
			}
			view := readPartialGitForTest(t, root, name, true)
			state := &editor.DiffState{}
			setPatch(state, view.file)
			applyPartialGit(t, root, name, true, gitDiffPart{hunk: state.Hunks()[0]})
			if tt.exists {
				checkPartialGit(t, root, name, tt.base, tt.work)
			} else if got := mustPartialGit(t, root, "ls-files", "--", name); got != "" {
				t.Fatalf("unstaged new file still in index: %q", got)
			}
		})
	}
}

func TestGitPartRejectsStaleDiffAndRefreshesTab(t *testing.T) {
	const name = "source.go"
	root := gitRepo(t, map[string]string{name: "original\n"})
	writeFile(t, root, name, "first edit\n")
	h := newHarness(t, root)
	h.readGit()
	h.a.showGitDiff(h.a.git.changes[0], false)
	h.settle(func() bool { return h.a.focus.active == diffTab(name, false) })
	h.tt.Frame()
	writeFile(t, root, name, "external edit\n")
	clickPartialGit(t, h, "Stage Hunk")
	h.settle(func() bool { return !h.a.git.busy && !h.a.git.loading })
	checkPartialGit(t, root, name, "original\n", "external edit\n")
	v := h.a.focus.diffs[diffTab(name, false)]
	if !h.feedbackContains(gitPartialText("gitPartialDiffChanged")) || v.state.New.String() != "external edit\n" {
		t.Fatalf("stale diff did not reload: feedback %v, text %q", h.feedbackTexts(), v.state.New.String())
	}
}

func clickPartialGit(t *testing.T, h *harness, label string) {
	t.Helper()
	if err := h.tt.Click(label); err != nil {
		t.Fatal(err)
	}
}

func mustPartialGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	out, err := git.Command(context.Background(), root, args...)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func checkPartialGit(t *testing.T, root, name, index, work string) {
	t.Helper()
	if got := mustPartialGit(t, root, "show", ":"+name); got != index {
		t.Fatalf("index = %q, want %q", got, index)
	}
	got, err := os.ReadFile(filepath.Join(root, name))
	if string(got) != work || err != nil && !(work == "" && os.IsNotExist(err)) {
		t.Fatalf("working tree = %q (%v), want %q", got, err, work)
	}
}

func readPartialGitForTest(t *testing.T, root, name string, staged bool) gitChangeView {
	t.Helper()
	ctx := context.Background()
	ch, err := git.PathChange(ctx, root, name)
	if err != nil {
		t.Fatal(err)
	}
	view, err := readGitChangeView(ctx, root, ch, staged)
	if err != nil || view.partial == nil {
		t.Fatalf("expected partial patch for %+v, got %+v (%v)", ch, view, err)
	}
	return view
}

func applyPartialGit(t *testing.T, root, name string, staged bool, part gitDiffPart) {
	t.Helper()
	view := readPartialGitForTest(t, root, name, staged)
	if err := applyCurrentGitDiffPart(context.Background(), root, name, staged, view.partial.digest, part); err != nil {
		t.Fatal(err)
	}
}
