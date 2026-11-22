package app

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui"

	"see/internal/search"
)

func TestSearchExcerptRangesMergeNearbyMatches(t *testing.T) {
	got := searchExcerptRanges([]int{9, 0, 3, 20}, 22, 2)
	want := [][2]int{{0, 6}, {7, 12}, {18, 22}}
	if !slices.Equal(got, want) {
		t.Fatalf("ranges = %v, want %v", got, want)
	}
}

func TestSpliceLinesKeepsEndingsAndRejectsStaleLines(t *testing.T) {
	text := "a\r\nb\r\nc\r\nd\r\n"
	got, err := spliceLines(text, []lineEdit{
		{first: 0, old: []string{"a"}, new: []string{"A", "A2"}},
		{first: 2, old: []string{"c", "d"}, new: nil},
	})
	if err != nil || got != "A\r\nA2\r\nb\r\n" {
		t.Fatalf("splice = %q, %v", got, err)
	}
	if _, err := spliceLines(text, []lineEdit{{first: 1, old: []string{"x"}, new: []string{"y"}}}); !errors.Is(err, search.ErrStaleMatches) {
		t.Fatalf("stale lines: %v", err)
	}
}

func TestSearchResultsEditInPlaceAndSaveEachFile(t *testing.T) {
	if _, err := findRipgrep(""); err != nil {
		t.Skip(err)
	}
	root := writeTree(t, map[string]string{
		"a.txt": "one\nseed a\ntwo\n",
		"b.txt": "seed b\n",
		"c.txt": "seed c\n",
	})
	h := newHarness(t, root)
	open := h.open("a.txt")
	h.tt.Key(ui.Cmd|ui.Shift, ui.KeyF)
	h.tt.Type("seed")
	h.tt.Key(0, ui.KeyEnter)
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 3 })
	clickFindControl(t, h, "Edit Results")
	v := h.a.focus.diffs[searchEditTab]
	if v == nil || v.search == nil || h.a.focus.active != searchEditTab {
		t.Fatal("search results did not open as an editable tab")
	}
	h.settle(func() bool { return !v.search.loading })
	if got := len(v.search.files); got != 3 {
		t.Fatalf("files = %d", got)
	}
	excerpt := func(name string) *searchExcerpt {
		for _, f := range v.search.files {
			if filepath.Base(f.path) == name {
				return f.excerpts[0]
			}
		}
		t.Fatalf("no excerpt of %s", name)
		return nil
	}
	if got := excerpt("a.txt").st.Buffer.String(); got != "one\nseed a\ntwo" {
		t.Fatalf("excerpt shows %q", got)
	}
	excerpt("a.txt").st.Buffer.Set("one\nsprout a\nadded\ntwo")
	excerpt("b.txt").st.Buffer.Set("sprout b")
	writeFindTestFile(t, filepath.Join(root, "c.txt"), "seed changed\n")
	excerpt("c.txt").st.Buffer.Set("sprout c")
	h.tt.Frame()
	if !v.search.dirty() {
		t.Fatal("edits did not mark the results edited")
	}

	h.tt.Key(ui.Cmd, ui.KeyS)
	h.tt.Frame()
	if got := readFindTestFile(t, open.path); got != "one\nsprout a\nadded\ntwo\n" {
		t.Fatalf("open file = %q", got)
	}
	if got := open.buf.String(); got != "one\nsprout a\nadded\ntwo\n" || open.buf.Modified() {
		t.Fatalf("open document = %q, modified=%v", got, open.buf.Modified())
	}
	if got := readFindTestFile(t, filepath.Join(root, "b.txt")); got != "sprout b\n" {
		t.Fatalf("closed file = %q", got)
	}
	if got := readFindTestFile(t, filepath.Join(root, "c.txt")); got != "seed changed\n" {
		t.Fatalf("file changed on disk was overwritten: %q", got)
	}
	if !strings.Contains(v.search.note, "Saved 2 files.") || !v.search.noteErr {
		t.Fatalf("note = %q, err=%v", v.search.note, v.search.noteErr)
	}
	h.settle(func() bool { return !h.a.find.running && h.a.find.stats.Matches == 1 })
	if r := h.a.find.results; len(r) != 1 || filepath.Base(r[0].Path) != "c.txt" {
		t.Fatalf("navigator results were not refreshed from disk: %+v", r)
	}
	if !excerpt("c.txt").st.Buffer.Modified() || excerpt("b.txt").st.Buffer.Modified() {
		t.Fatal("only the unsaved file should stay edited")
	}

	h.a.closeTab(h.a.focus, searchEditTab)
	if !h.a.focus.has(searchEditTab) {
		t.Fatal("closing discarded unsaved edits")
	}
	clickFindControl(t, h, "Revert")
	h.a.closeTab(h.a.focus, searchEditTab)
	if h.a.focus.has(searchEditTab) {
		t.Fatal("reverted results did not close")
	}
}

func writeFindTestFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}
