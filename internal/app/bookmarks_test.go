package app

import (
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/nav"
)

// Toggle Bookmark marks and unmarks the editor's line.
func TestToggleBookmarkCommand(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "one\ntwo\nthree\n"}))
	d := h.open("a.txt")
	st := h.a.focus.eds[d.path]
	st.GoTo(editor.Pos{Line: 1})
	h.tt.Key(ui.Alt|ui.Cmd, ui.KeyK)
	if !h.a.bookmarks.Has(d.path, 1) {
		t.Fatalf("toggle did not mark the line: %+v", h.a.bookmarks.List)
	}
	h.tt.Key(ui.Alt|ui.Cmd, ui.KeyK)
	if h.a.bookmarks.Has(d.path, 1) {
		t.Fatal("toggle did not unmark the line")
	}
}

// Bookmarks survive a session: capture, reopen the workspace, and the
// marks are back.
func TestBookmarksSurviveSession(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "one\ntwo\nthree\n"}))
	d := h.open("a.txt")
	h.a.focus.eds[d.path].GoTo(editor.Pos{Line: 2})
	h.a.toggleBookmark()
	h.a.captureSession()

	next := newHarness(t, "")
	next.a.cfg = h.a.cfg
	next.a.openWorkspace(h.a.root)
	next.settle(func() bool {
		for _, d := range next.a.docs {
			if d.loading {
				return false
			}
		}
		return len(next.a.docs) > 0
	})
	if !next.a.bookmarks.Has(d.path, 2) {
		t.Fatalf("bookmark did not survive the session: %+v", next.a.bookmarks.List)
	}
}

// openBookmark jumps to the marked line and keeps the trail.
func TestOpenBookmarkJumpsToLine(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "one\ntwo\nthree\n",
		"b.txt": "x\ny\nz\nw\nv\n",
	}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	h.a.openBookmark(nav.Bookmark{Path: b.path, Line: 4})
	st := h.a.focus.eds[b.path]
	if st == nil || h.a.focus.active != b.path {
		t.Fatal("bookmark jump did not open the file")
	}
	h.settle(func() bool { return st.CursorPos().Line == 4 })
	if len(h.a.hist.Back) == 0 {
		t.Fatal("bookmark jump did not keep the trail")
	}
	_ = a
}

// A removed file drops its marks; a renamed file keeps them on the new
// path.
func TestBookmarksFollowFileLifecycle(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"gone.txt": "x\n",
		"ren.txt":  "y\n",
	}))
	gone := h.open("gone.txt")
	ren := h.open("ren.txt")
	h.a.bookmarks.Add(nav.Bookmark{Path: gone.path, Line: 0})
	h.a.bookmarks.Add(nav.Bookmark{Path: ren.path, Line: 0})

	h.a.removed(gone.path)
	if h.a.bookmarks.Has(gone.path, 0) {
		t.Fatal("removed file kept its bookmark")
	}
	oldPath := ren.path // renamed() updates doc.path in place
	next := filepath.Join(h.a.root, "ren2.txt")
	h.a.renamed(oldPath, next)
	if !h.a.bookmarks.Has(next, 0) {
		t.Fatalf("renamed file lost its bookmark: %+v", h.a.bookmarks.List)
	}
	if slices.ContainsFunc(h.a.bookmarks.List, func(b nav.Bookmark) bool { return b.Path == oldPath }) {
		t.Fatal("old path still bookmarked after rename")
	}
}
