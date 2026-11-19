package nav

import (
	"path/filepath"
	"slices"
	"strings"
)

// Bookmark is a named waypoint: a document and a line, the line from 0.
type Bookmark struct {
	Path string
	Line int
}

// DefaultBookmarkLimit is how many bookmarks a workspace keeps; the
// oldest leave it first.
const DefaultBookmarkLimit = 200

// Bookmarks is an ordered list of waypoints with a bound.
type Bookmarks struct {
	List  []Bookmark
	Limit int
}

func (b *Bookmarks) limit() int {
	if b.Limit <= 0 {
		return DefaultBookmarkLimit
	}
	return b.Limit
}

// Index is where a bookmark of path at line is, or -1.
func (b *Bookmarks) Index(path string, line int) int {
	return slices.Index(b.List, Bookmark{Path: path, Line: line})
}

// Has reports whether path at line is bookmarked.
func (b *Bookmarks) Has(path string, line int) bool { return b.Index(path, line) >= 0 }

// Add records a bookmark, moving a known one to the end as its latest.
func (b *Bookmarks) Add(bm Bookmark) {
	if bm.Path == "" || bm.Line < 0 {
		return
	}
	b.Remove(bm.Path, bm.Line)
	b.List = append(b.List, bm)
	if n := len(b.List); n > b.limit() {
		b.List = slices.Clone(b.List[n-b.limit():])
	}
}

// Remove drops a bookmark; false when there is none.
func (b *Bookmarks) Remove(path string, line int) bool {
	if i := b.Index(path, line); i >= 0 {
		b.List = slices.Delete(b.List, i, i+1)
		return true
	}
	return false
}

// Toggle records a bookmark, or drops it when there already is one.
func (b *Bookmarks) Toggle(path string, line int) (added bool) {
	if b.Remove(path, line) {
		return false
	}
	b.Add(Bookmark{Path: path, Line: line})
	return true
}

// Forget drops every bookmark of path and of what is under it, as of a
// file or folder that is gone.
func (b *Bookmarks) Forget(path string) {
	b.List = slices.DeleteFunc(b.List, func(bm Bookmark) bool { return within(path, bm.Path) })
}

// Renamed moves the bookmarks of old, and of what is under it, to dst,
// as of a file or folder that was renamed. Lines follow the path.
func (b *Bookmarks) Renamed(old, dst string) {
	for i := range b.List {
		bm := &b.List[i]
		if bm.Path == old {
			bm.Path = dst
		} else if rel, ok := relPath(old, bm.Path); ok {
			bm.Path = filepath.Join(dst, rel)
		}
	}
	// Two paths can rename into one: keep the later.
	seen := make(map[Bookmark]bool, len(b.List))
	out := b.List[:0]
	for _, bm := range b.List {
		if !seen[bm] {
			seen[bm] = true
			out = append(out, bm)
		}
	}
	b.List = out
}

// Clear empties the bookmarks.
func (b *Bookmarks) Clear() { b.List = nil }

// within reports whether p is path or under it, as one of a folder's.
func within(path, p string) bool {
	return p == path || strings.HasPrefix(p, path+string(filepath.Separator))
}

// relPath is p relative to path, with ok false when p is not under it.
func relPath(path, p string) (string, bool) {
	if !strings.HasPrefix(p, path+string(filepath.Separator)) {
		return "", false
	}
	return p[len(path)+1:], true
}
