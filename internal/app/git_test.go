package app

import (
	"context"
	"testing"

	"github.com/egoist/mygo/ui"

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
