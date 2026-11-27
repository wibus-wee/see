package app

import (
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestWhitespaceDiffHidesOnlyWhitespaceChanges(t *testing.T) {
	p := editor.FilePatch{Hunks: []editor.PatchHunk{{
		OldStart: 1, OldLines: 4, NewStart: 1, NewLines: 4,
		Lines: []string{" a", "-b", "+b  ", "-c", "+C", " d"},
	}}}
	filtered := withoutWhitespaceChanges(p)
	d := &editor.DiffState{}
	d.SetPatch(filtered)
	if got := len(d.Hunks()); got != 1 {
		t.Fatalf("filtered hunks = %d, want 1", got)
	}
	h := d.Hunks()[0]
	if h.OldEnd-h.OldStart != 1 || h.NewEnd-h.NewStart != 1 {
		t.Fatalf("filtered hunk = %+v, want one changed line", h)
	}
}

// A commit tab's embedded diffs follow the whitespace preference too,
// as the log's already did.
func TestCommitTabFollowsWhitespacePreference(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "a\n"})
	writeFile(t, root, "a.txt", "a \n")
	gitRun(t, root, "commit", "-qam", "trailing space")

	h := newHarness(t, root)
	h.readGit()
	h.a.cfg.HideWhitespaceChanges = false
	h.a.updateDiffWhitespace()
	h.a.loadHistory(true)
	h.settle(func() bool { return !h.a.git.history.loading })
	cm := h.a.git.history.commits[0]
	h.a.openCommit(cm)
	h.settle(func() bool {
		v := h.a.focus.diffs[commitTab(cm.Hash)]
		return v != nil && !v.commit.loading
	})
	v := h.a.focus.diffs[commitTab(cm.Hash)]
	if len(v.commit.files) != 1 || v.commit.files[0].diff == nil {
		t.Fatalf("commit files %+v", v.commit.files)
	}
	if h.a.diffChangesHidden(v.commit.files[0].diff) {
		t.Fatal("a whitespace change is hidden while the preference is off")
	}

	h.a.cfg.HideWhitespaceChanges = true
	h.a.updateDiffWhitespace()
	if !h.a.diffChangesHidden(v.commit.files[0].diff) {
		t.Fatal("the commit tab kept a whitespace-only change")
	}
	h.a.cfg.HideWhitespaceChanges = false
	h.a.updateDiffWhitespace()
	if h.a.diffChangesHidden(v.commit.files[0].diff) {
		t.Fatal("the commit tab did not restore its whitespace change")
	}
}

func TestBufferWhitespaceDiffDisablesLineSelection(t *testing.T) {
	d := &editor.DiffState{LineSelection: true}
	setBufferDiffWhitespace(d, editor.NewBuffer("a\nb\n"), editor.NewBuffer("a\nb \n"), true)
	if got := len(d.Hunks()); got != 0 {
		t.Fatalf("whitespace-only hunks = %d, want 0", got)
	}
	if d.LineSelection {
		t.Fatal("line selection remains enabled while whitespace changes are hidden")
	}
}
