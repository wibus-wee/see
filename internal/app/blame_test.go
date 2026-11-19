package app

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
)

func TestInlineBlameAndStatus(t *testing.T) {
	h := newHarness(t, gitRepo(t, map[string]string{"a.txt": "one\ntwo\n"}))
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.blameReady })
	st := h.a.activeEditor()
	d.languageInlays = []editor.Inlay{{Offset: 0, Text: "type hint"}}
	inlays := h.a.editorInlays(d, st)
	if len(inlays) != 2 || inlays[0].Text != "type hint" || !strings.Contains(inlays[1].Text, "See,") || !strings.Contains(inlays[1].Text, " • first") || inlays[1].Offset != 3 || !inlays[1].Trailing {
		t.Fatalf("inlays: %+v", inlays)
	}
	if !h.tt.HasText(d.blame[0].Short + " · See · " + blameDate(d.blame[0].Date)) {
		t.Fatalf("status missing: %q", h.tt.Texts())
	}
	st.Select(5, 5)
	h.tt.Frame()
	if got := h.a.editorInlays(d, st)[1].Offset; got != 7 {
		t.Fatalf("cursor line inlay offset: %d", got)
	}
	h.a.cfg.HideInlineBlame = true
	if got := h.a.editorInlays(d, st); len(got) != 1 || got[0].Text != "type hint" {
		t.Fatalf("toggle removed language hint: %+v", got)
	}
	h.a.cfg.HideInlineBlame = false
	d.buf.Set("one\nchanged\n")
	h.tt.Frame()
	if _, ok := h.a.lineBlame(d, st); ok {
		t.Fatal("stale blame survived edit")
	}
	h.settle(func() bool { return d.blameReady })
	st.Select(5, 5)
	h.tt.Frame()
	if b, ok := h.a.lineBlame(d, st); !ok || !b.Uncommitted {
		t.Fatalf("dirty line: %+v, %v", b, ok)
	}
	if got := h.a.editorInlays(d, st)[1].Text; !strings.Contains(got, logText("blameUncommitted")) {
		t.Fatalf("dirty inlay: %q", got)
	}
	if h.a.canRevealLineCommit() || !h.tt.HasText(logText("blameUncommitted")) {
		t.Fatal("uncommitted line exposed reveal or missing status")
	}
	h.a.revealLineCommit()
	if h.a.focus.active == gitLogTab {
		t.Fatal("revealed uncommitted line")
	}
	h.a.openQuickPanel(">")
	for _, r := range h.a.quick.results {
		if r.Path == commandLabel("Show Line's Commit in Git Log") {
			t.Fatal("uncommitted reveal command is present")
		}
	}
}

func TestBlameUntrackedAndEmpty(t *testing.T) {
	root := gitRepo(t, map[string]string{"empty.txt": ""})
	writeFile(t, root, "untracked.txt", "new\n")
	h := newHarness(t, root)
	h.readGit()
	for _, name := range []string{"untracked.txt", "empty.txt"} {
		d := h.open(name)
		h.settle(func() bool { return d.blameReady })
		if len(d.blame) != 0 || h.a.canRevealLineCommit() {
			t.Fatalf("blame for %s: %+v", name, d.blame)
		}
	}
}

func TestRevealBlameBeyondFirstPage(t *testing.T) {
	root := gitRepo(t, map[string]string{"a.txt": "old\n"})
	ctx := context.Background()
	hash := git.Head(ctx, root)
	for i := 0; i < historyPage+1; i++ {
		if _, err := git.Command(ctx, root, "-c", "user.name=See", "-c", "user.email=see@example.com", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-qm", fmt.Sprintf("new %d", i)); err != nil {
			t.Fatal(err)
		}
	}
	h := newHarness(t, root)
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	v.query = "obstructing filter"
	h.tt.Frame()
	h.a.revealLogCommit(hash)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.git.history.loading })
	h.settle(func() bool { return v.selected == hash && v.detail != nil && !v.detail.loading })
	if v.query != "" || len(h.a.git.history.commits) <= historyPage || !h.tt.HasText("first") {
		t.Fatalf("reveal: query=%q commits=%d texts=%q", v.query, len(h.a.git.history.commits), h.tt.Texts())
	}
}

func TestRevealCommitOutsideHistoryAndError(t *testing.T) {
	root := historyRepo(t)
	h := newHarness(t, root)
	h.readGit()
	h.a.openGitLog()
	h.settle(func() bool { return !h.a.git.history.loading })
	v := h.a.focus.diffs[gitLogTab].log
	h.settle(func() bool { return v.detail != nil && !v.detail.loading })
	cm := h.a.git.history.commits[len(h.a.git.history.commits)-1]
	h.a.git.history.commits = h.a.git.history.commits[:1]
	h.a.git.history.done = true
	h.a.revealLogCommit(cm.Hash)
	h.tt.Frame()
	h.settle(func() bool { return v.selected == cm.Hash && v.detail != nil && !v.detail.loading })
	h.a.revealLogCommit(strings.Repeat("f", 40))
	h.tt.Frame()
	h.settle(func() bool { return v.pending == "" })
	if !strings.HasPrefix(h.a.notice, logText("blameRevealFailed")) {
		t.Fatalf("missing error: %q", h.a.notice)
	}
}
