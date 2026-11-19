package nav

import (
	"path/filepath"
	"testing"
)

func TestBookmarksToggle(t *testing.T) {
	var b Bookmarks
	if added := b.Toggle("/w/a.txt", 3); !added || !b.Has("/w/a.txt", 3) {
		t.Fatal("toggle did not add")
	}
	if added := b.Toggle("/w/a.txt", 3); added || b.Has("/w/a.txt", 3) || len(b.List) != 0 {
		t.Fatal("toggle did not remove")
	}
}

func TestBookmarksAddMovesLatestToEnd(t *testing.T) {
	var b Bookmarks
	b.Add(Bookmark{Path: "/w/a.txt", Line: 1})
	b.Add(Bookmark{Path: "/w/b.txt", Line: 2})
	b.Add(Bookmark{Path: "/w/a.txt", Line: 1})
	if len(b.List) != 2 || b.List[0].Path != "/w/b.txt" || b.List[1].Path != "/w/a.txt" {
		t.Fatalf("re-add should move the mark to the end: %+v", b.List)
	}
}

func TestBookmarksLimitEvictsOldest(t *testing.T) {
	b := Bookmarks{Limit: 3}
	for i := 0; i < 5; i++ {
		b.Add(Bookmark{Path: "/w/f.txt", Line: i})
	}
	if len(b.List) != 3 || b.List[0].Line != 2 {
		t.Fatalf("oldest should leave first: %+v", b.List)
	}
}

func TestBookmarksForgetDropsPathAndChildren(t *testing.T) {
	var b Bookmarks
	dir := filepath.Join("/w", "dir")
	b.Add(Bookmark{Path: "/w/keep.txt", Line: 1})
	b.Add(Bookmark{Path: dir, Line: 1})
	b.Add(Bookmark{Path: filepath.Join(dir, "in.txt"), Line: 1})
	b.Add(Bookmark{Path: "/w/dirx/notchild.txt", Line: 1}) // same prefix, not a child
	b.Forget(dir)
	if len(b.List) != 2 || !b.Has("/w/keep.txt", 1) || !b.Has("/w/dirx/notchild.txt", 1) {
		t.Fatalf("forget removed wrong entries: %+v", b.List)
	}
}

func TestBookmarksRenamedFollowsPath(t *testing.T) {
	var b Bookmarks
	old := filepath.Join("/w", "old")
	b.Add(Bookmark{Path: "/w/keep.txt", Line: 1})
	b.Add(Bookmark{Path: old, Line: 5})
	b.Add(Bookmark{Path: filepath.Join(old, "in.txt"), Line: 7})
	b.Renamed(old, filepath.Join("/w", "new"))
	if !b.Has("/w/keep.txt", 1) {
		t.Fatal("unrelated bookmark moved")
	}
	if !b.Has(filepath.Join("/w", "new"), 5) || !b.Has(filepath.Join("/w", "new", "in.txt"), 7) {
		t.Fatalf("rename did not move marks: %+v", b.List)
	}
}

func TestBookmarksRenamedDedupes(t *testing.T) {
	var b Bookmarks
	old := filepath.Join("/w", "old")
	b.Add(Bookmark{Path: filepath.Join("/w", "new"), Line: 2})
	b.Add(Bookmark{Path: old, Line: 2})
	b.Renamed(old, filepath.Join("/w", "new"))
	if len(b.List) != 1 {
		t.Fatalf("rename into an existing path should not duplicate: %+v", b.List)
	}
}
