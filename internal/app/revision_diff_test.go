package app

import (
	"github.com/egoist/mygo/ui/editor"
	"os"
	"path/filepath"
	"testing"
)

func TestRevisionSymlinkWorkspace(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "committed\n"})
	alias := filepath.Join(t.TempDir(), "repo")
	if err := os.Symlink(root, alias); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	h := newHarness(t, alias)
	d := h.open("a.txt")
	h.readGit()
	h.a.showRevision(h.a.focus, d, "")
	v := h.a.focus.diffs[revisionPrefix+d.path]
	h.settle(func() bool { return !v.revision.loading })
	if v.revision.err != nil || v.state == nil || v.state.Old.String() != "committed\n" {
		t.Fatalf("symlink workspace comparison: %v", v.revision.err)
	}
}

func TestRevisionComparison(t *testing.T) {
	root := gitRepo(t, map[string]string{"old.txt": "first\n"})
	gitRun(t, root, "mv", "old.txt", "new.txt")
	gitRun(t, root, "commit", "-qm", "rename")
	writeFile(t, root, "new.txt", "latest\n")
	gitRun(t, root, "add", ".")
	gitRun(t, root, "commit", "-qm", "latest")
	h := newHarness(t, root)
	d := h.open("new.txt")
	h.readGit()
	d.buf.Edit(editor.Edit{Start: 0, End: 0, Text: "unsaved\n"})
	h.a.showRevision(h.a.focus, d, "")
	v := h.a.focus.diffs[revisionPrefix+d.path]
	h.settle(func() bool { return !v.revision.loading })
	if v.revision.err != nil || v.state == nil {
		t.Fatalf("load: %v", v.revision.err)
	}
	if v.revision.selected != 0 || v.state.Old.String() != "latest\n" || v.state.New.String() != "unsaved\nlatest\n" {
		t.Fatal("incorrect initial comparison")
	}
	h.a.selectRevision(v, len(v.revision.commits)-1)
	h.settle(func() bool { return !v.revision.loading })
	if v.revision.err != nil || v.state.Old.String() != "first\n" {
		t.Fatalf("renamed revision: %v", v.revision.err)
	}
	hash := v.revision.commits[1].Hash
	h.a.activateIn(h.a.focus, d.path)
	h.a.compareInspectorRevision(hash)
	h.settle(func() bool { return !v.revision.loading })
	if got := h.a.focus.diffs[revisionPrefix+d.path]; got == nil || got.revision.selected != 1 {
		t.Fatal("inspector compare action did not select the requested revision")
	}
	if got := h.a.inspectorFileDoc(); got != d {
		t.Fatal("inspector did not resolve the focused revision tab to its source document")
	}
	localKey := localHistoryTab(d.path)
	h.a.focus.diffs[localKey] = &gitDiffTab{localHistory: &localHistoryView{path: d.path}}
	h.a.focus.active = localKey
	if got := h.a.inspectorFileDoc(); got != d {
		t.Fatal("inspector did not resolve the focused local history tab to its source document")
	}
	h.a.activateIn(h.a.focus, revisionPrefix+d.path)
	d.buf.Edit(editor.Edit{Start: 0, End: 0, Text: "refresh\n"})
	h.a.refreshRevision(v)
	h.settle(func() bool { return !v.revision.loading })
	if v.state.New.String() != d.buf.String() {
		t.Fatal("refresh did not read buffer")
	}
}

func TestRevisionEmptyStates(t *testing.T) {
	for _, repo := range []bool{false, true} {
		t.Run(map[bool]string{false: "no repo", true: "untracked"}[repo], func(t *testing.T) {
			root := writeTree(t, map[string]string{"a.txt": "text\n"})
			if repo {
				root = gitRepo(t, map[string]string{"tracked.txt": "text\n"})
				writeFile(t, root, "a.txt", "text\n")
			}
			h := newHarness(t, root)
			d := h.open("a.txt")
			h.readGit()
			h.a.showRevision(h.a.focus, d, "")
			v := h.a.focus.diffs[revisionPrefix+d.path]
			h.settle(func() bool { return !v.revision.loading })
			if v.state != nil || len(v.revision.commits) != 0 {
				t.Fatal("expected empty comparison")
			}
		})
	}
}
