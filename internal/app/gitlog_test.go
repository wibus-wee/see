package app

import (
	"testing"
	"time"

	"see/internal/git"
)

// The Git log opens as a tab of its own, once: the commits in rows over
// the one chosen, the newest at first, whose files show as diffs one
// after another; the filter narrows the rows, and a click chooses another.
func TestGitLogShowsCommitsAndTheirDiffs(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.openGitLog()
	h.a.openGitLog()
	if n := len(h.a.focus.order); n != 1 || h.a.focus.active != gitLogTab {
		t.Fatalf("tabs %q, active %q", h.a.focus.order, h.a.focus.active)
	}
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return !h.a.git.history.loading }) // the next frame chooses the newest commit
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	for _, s := range []string{"Git Log", "Merge side", "Add B on the side", "Add A", "first", "See"} {
		if !h.tt.HasText(s) {
			t.Fatalf("the log shows no %q: %q", s, h.tt.Texts())
		}
	}
	if v.selected != h.a.git.history.commits[0].Hash {
		t.Fatalf("chose %q, not the newest commit", v.selected)
	}

	v.query = "side"
	h.tt.Frame()
	if h.tt.HasText("first") || !h.tt.HasText("Add B on the side") {
		t.Fatalf("filtered on %q: %q", v.query, h.tt.Texts())
	}

	if err := h.tt.Click("Add B on the side"); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool {
		return v.detail != nil && v.detail.hash != h.a.git.history.commits[0].Hash && !v.detail.loading
	})
	d := v.detail
	if len(d.files) != 1 || d.files[0].path != "b.go" || d.files[0].diff == nil {
		t.Fatalf("files %+v, err %q", d.files, d.err)
	}
	if !h.tt.HasText("b.go") || !h.tt.HasText("1 file changed") {
		t.Fatalf("the commit shows %q", h.tt.Texts())
	}

	// It is not a file to read again in the next session.
	if spec := diffTabSpec(h.a.focus.diffs[gitLogTab]); spec != nil {
		t.Fatalf("kept %+v", spec)
	}
}

func TestLogMatches(t *testing.T) {
	cm := git.Commit{Hash: "abcdef0123", Short: "abcdef0", Subject: "Fix the Tree", Author: "Ada Lovelace",
		Email: "ada@example.com", Refs: []string{"HEAD", "main", "v1.0"}, Date: time.Now(),
		CoAuthors: []git.Person{{Name: "Grace Hopper", Email: "grace@example.com"}}}
	for q, want := range map[string]bool{
		"":           true,
		"tree":       true,
		"ADA":        true,
		"abcdef":     true,
		"v1.0":       true,
		"grace":      true,
		"fix tree":   true, // every word, anywhere
		"fix branch": false,
		"0123":       false, // a hash from its start only
	} {
		if got := logMatches(cm, q); got != want {
			t.Errorf("logMatches(%q) = %v", q, got)
		}
	}
}

// The bar between the commits and the one chosen drags to share the
// height, and the session keeps where it was left.
func TestGitLogSplitDrags(t *testing.T) {
	h := newHarness(t, historyRepo(t))
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	if h.a.git.logSplit != 40 {
		t.Fatalf("split %v at first", h.a.git.logSplit)
	}
	list, ok := h.tt.Find("Commits")
	if !ok {
		t.Fatalf("no list of commits: %q", h.tt.Texts())
	}
	x, y := list.X+list.W/2, list.Y+list.H+1
	h.tt.Press(x, y)
	h.tt.Move(x, y+120)
	h.tt.Release(x, y+120)
	h.tt.Frame()
	if h.a.git.logSplit <= 45 {
		t.Fatalf("split %v after dragging the bar down", h.a.git.logSplit)
	}
	h.a.captureSession()
	if got := h.a.cfg.Workspaces[h.a.root].GitLogSplit; got != h.a.git.logSplit {
		t.Fatalf("session keeps %v, not %v", got, h.a.git.logSplit)
	}
}
