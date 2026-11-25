package app

import (
	"context"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
		if r.Path == commandLabel("git.revealLineCommit") {
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
	pinGitEnv(t)
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

func TestBlameHoverOnTrailingInlay(t *testing.T) {
	h := newHarness(t, gitRepo(t, map[string]string{"a.txt": "one\ntwo\n"}))
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.blameReady })
	st := h.a.activeEditor()
	// The caret at the first line's end gives the position where its
	// trailing blame starts; just past it the pointer is on the text.
	st.GoTo(editor.Pos{Line: 0, Col: 3})
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("line end caret is not available")
	}
	h.tt.Move(caret.X+caret.H, caret.Y+caret.H/2)
	h.tt.Advance(1500 * time.Millisecond)
	h.tt.Frame()
	if !h.tt.HasText("first") {
		t.Fatalf("blame hover missing subject: %q", h.tt.Texts())
	}
	if !h.tt.HasText(d.blame[0].Short) {
		t.Fatalf("blame hover missing hash: %q", h.tt.Texts())
	}
	if os.Getenv("SEE_BLAME_SCREENSHOTS") == "1" {
		writeBlameHoverScreenshots(t, h)
		d.blame[0].Subject = "feat: a rather long commit subject that should wrap across lines to test how the card handles it"
		h.tt.Move(caret.X-caret.H, caret.Y+caret.H/2)
		h.tt.Advance(1500 * time.Millisecond)
		h.tt.Frame()
		h.tt.Move(caret.X+caret.H, caret.Y+caret.H/2)
		h.tt.Advance(1500 * time.Millisecond)
		h.tt.Frame()
		writeBlameHoverScreenshot(t, h, false, "blame-hover-long.png")
	}
	// The hash opens the commit in the Git Log.
	if err := h.tt.Click(commandLabel("git.revealLineCommit") + ": " + d.blame[0].Short); err != nil {
		t.Fatalf("hash is not clickable: %v", err)
	}
	h.settle(func() bool { return h.a.focus != nil && h.a.focus.active == gitLogTab })
}

func TestBlameHoverDismissedOffTrailing(t *testing.T) {
	h := newHarness(t, gitRepo(t, map[string]string{"a.txt": "one\ntwo\n"}))
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.blameReady })
	st := h.a.activeEditor()
	st.GoTo(editor.Pos{Line: 0, Col: 3})
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("line end caret is not available")
	}
	h.tt.Move(caret.X+caret.H, caret.Y+caret.H/2)
	h.tt.Advance(1500 * time.Millisecond)
	h.tt.Frame()
	if !h.tt.HasText("first") {
		t.Fatalf("blame hover missing: %q", h.tt.Texts())
	}
	// Hovering the line's own text, past the hide grace, closes the card.
	h.tt.Move(caret.X-caret.H, caret.Y+caret.H/2)
	h.tt.Advance(1500 * time.Millisecond)
	h.tt.Frame()
	if h.tt.HasText("first") {
		t.Fatalf("blame hover survived moving into the text: %q", h.tt.Texts())
	}
}

func TestBlameHoverUncommitted(t *testing.T) {
	h := newHarness(t, gitRepo(t, map[string]string{"a.txt": "one\ntwo\n"}))
	h.readGit()
	d := h.open("a.txt")
	h.settle(func() bool { return d.blameReady })
	d.buf.Set("one\nchanged\n")
	h.tt.Frame()
	h.settle(func() bool { return d.blameReady })
	st := h.a.activeEditor()
	st.GoTo(editor.Pos{Line: 1, Col: len("changed")})
	h.tt.Frame()
	caret, ok := h.tt.TextCaret()
	if !ok {
		t.Fatal("line end caret is not available")
	}
	h.tt.Move(caret.X+caret.H, caret.Y+caret.H/2)
	h.tt.Advance(1500 * time.Millisecond)
	h.tt.Frame()
	if !h.tt.HasText(logText("blameUncommitted")) {
		t.Fatalf("uncommitted blame hover missing: %q", h.tt.Texts())
	}
}

func writeBlameHoverScreenshot(t *testing.T, h *harness, dark bool, name string) {
	t.Helper()
	h.tt.SetDark(dark)
	h.tt.Frame()
	dir := os.Getenv("SEE_BLAME_SCREENSHOT_DIR")
	if dir == "" {
		dir = os.Getenv("HOME")
	}
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, h.tt.Image()); err != nil {
		_ = f.Close()
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func writeBlameHoverScreenshots(t *testing.T, h *harness) {
	t.Helper()
	writeBlameHoverScreenshot(t, h, false, "blame-hover-light.png")
	writeBlameHoverScreenshot(t, h, true, "blame-hover-dark.png")
}
