package app

import (
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
)

func TestWebLinkAt(t *testing.T) {
	h := newHarness(t, "")
	h.a.git.web = "https://github.com/o/r"
	link := h.a.webLinkAt("abc123", "dir/file.go", 10, 20)
	want := "https://github.com/o/r/blob/abc123/dir/file.go#L10-L20"
	if link != want {
		t.Fatalf("got %q, want %q", link, want)
	}
	if got := h.a.webLinkAt("abc123", "dir/file.go", 5, 5); !strings.HasSuffix(got, "#L5") {
		t.Fatalf("single line: %q", got)
	}
	if got := h.a.webLinkAt("abc123", "dir/file.go", 0, 0); strings.Contains(got, "#") {
		t.Fatalf("no lines should have no anchor: %q", got)
	}
	if got := h.a.webLinkAt("", "dir/file.go", 1, 1); got != "" {
		t.Fatalf("no hash: %q", got)
	}
	h.a.git.web = ""
	if got := h.a.webLinkAt("abc123", "dir/file.go", 1, 1); got != "" {
		t.Fatalf("no host: %q", got)
	}
}

func TestPatchFileLine(t *testing.T) {
	// A full diff (--unified=max): lines are the file's.
	full := editor.FilePatch{Hunks: []editor.PatchHunk{{OldStart: 1, NewStart: 1, OldLines: 10, NewLines: 10}}}
	if got := patchFileLine(full, true, 4); got != 5 {
		t.Fatalf("full diff line 4: got %d, want 5", got)
	}
	// A partial diff of two hunks: lines 10-12 and 40-42 of the file.
	partial := editor.FilePatch{Hunks: []editor.PatchHunk{
		{OldStart: 10, NewStart: 10, OldLines: 3, NewLines: 3},
		{OldStart: 40, NewStart: 42, OldLines: 3, NewLines: 3},
	}}
	// The new side's text is the hunks' new lines one after another:
	// text lines 0-2 are file lines 10-12, text lines 3-5 file 42-44.
	for textLine, want := range map[int]int{0: 10, 1: 11, 2: 12, 3: 42, 4: 43, 5: 44} {
		if got := patchFileLine(partial, false, textLine); got != want {
			t.Fatalf("text line %d: got %d, want %d", textLine, got, want)
		}
	}
}

func TestDiffLinkURL(t *testing.T) {
	h := newHarness(t, "")
	h.a.git.web = "https://github.com/o/r"
	target := diffLinkTarget{hash: "abc123", rel: "file.go", full: true}
	v := &gitDiffTab{}

	// No selection: the file alone.
	if got := h.a.diffLinkURL(v, target); !strings.HasSuffix(got, "/blob/abc123/file.go") {
		t.Fatalf("no selection: %q", got)
	}
	// A selection on the new side links its lines.
	v.lineSelOn = true
	v.lineSel = editor.DiffLineRange{Side: editor.DiffNew, Start: 9, End: 19}
	if got := h.a.diffLinkURL(v, target); !strings.HasSuffix(got, "#L10-L20") {
		t.Fatalf("new side selection: %q", got)
	}
	// One on the old side leaves them out: they are the parent's.
	v.lineSel = editor.DiffLineRange{Side: editor.DiffOld, Start: 9, End: 19}
	if got := h.a.diffLinkURL(v, target); strings.Contains(got, "#") {
		t.Fatalf("old side selection should have no anchor: %q", got)
	}
}

func TestRevisionLinkTarget(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "one\n"})
	writeFile(t, root, "a.txt", "two\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "second")
	h := newHarness(t, root)
	d := h.open("a.txt")
	h.readGit()
	h.a.showRevision(h.a.focus, d, "")
	v := h.a.focus.diffs[revisionPrefix+d.path]
	h.settle(func() bool { return !v.revision.loading })
	if v.revision.err != nil || len(v.revision.commits) == 0 {
		t.Fatalf("load: %v", v.revision.err)
	}
	target := h.a.revisionLinkTarget(v)
	if target.hash != v.revision.commits[0].Hash {
		t.Fatalf("hash: got %q, want %q", target.hash, v.revision.commits[0].Hash)
	}
	if target.rel != "a.txt" {
		t.Fatalf("rel: got %q", target.rel)
	}
	if !target.full {
		t.Fatal("revision comparisons are of whole files")
	}
	// Selecting an older commit pins the link to it.
	h.a.selectRevision(v, len(v.revision.commits)-1)
	h.settle(func() bool { return !v.revision.loading })
	target = h.a.revisionLinkTarget(v)
	last := v.revision.commits[len(v.revision.commits)-1]
	if target.hash != last.Hash {
		t.Fatalf("older commit: got %q, want %q", target.hash, last.Hash)
	}
}
