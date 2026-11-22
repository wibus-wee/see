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
