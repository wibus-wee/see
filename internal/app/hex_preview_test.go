package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/egoist/mygo/ui"
	"see/internal/hexview"
)

func TestHexPreviewLargeFile(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "large.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	const size = int64(8 << 30)
	if err = f.Truncate(size); err != nil {
		t.Fatal(err)
	}
	if _, err = f.WriteAt([]byte("END!"), size-16); err != nil {
		t.Fatal(err)
	}
	f.Close()
	h := newHarness(t, root)
	d := h.open("large.bin")
	h.click("Show File Bytes")
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) != 0 })
	list := h.a.focus.hexLists[path]
	list.ScrollTo(int(hexview.Rows(size)-1), ui.Start)
	h.tt.Frame()
	h.settle(func() bool { return d.hex.pages[size-hexview.PageBytes] != nil })
	if !h.tt.HasText("Byte 1FFFFFFF0 45") {
		t.Fatalf("last row missing: %q", h.tt.Texts())
	}
	if len(d.hex.pages) > hexCachePages {
		t.Fatal("cache exceeded limit")
	}
}

func TestHexPreviewToggleText(t *testing.T) {
	root := writeTree(t, map[string]string{"a.txt": "hello"})
	h := newHarness(t, root)
	d := h.open("a.txt")
	d.buf.Set("unsaved")
	h.click("Show File Bytes")
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) != 0 })
	if !h.tt.HasText("Byte 0 68") {
		t.Fatalf("disk bytes missing: %q", h.tt.Texts())
	}
	h.click("Show File")
	if d.buf.String() != "unsaved" || !d.buf.Modified() {
		t.Fatal("buffer changed")
	}
}

func BenchmarkHexViewFrame(b *testing.B) {
	for _, tc := range []struct {
		name string
		size int64
	}{{"1MiB", 1 << 20}, {"8GiB", 8 << 30}} {
		b.Run(tc.name, func(b *testing.B) {
			a := newApp(&config{})
			a.post = func(func()) {}
			d := &doc{path: "/benchmark.bin", size: tc.size, hex: &hexState{pages: map[int64]*hexPage{0: {data: make([]byte, hexview.PageBytes)}}, pending: map[int64]bool{}}}
			g := &group{}
			tt := ui.NewTester(func(c *ui.Context) { a.hexView(c, g, d) }, 1100, 700)
			tt.Frame()
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tt.Frame()
			}
		})
	}
}

func TestHexPreviewCacheEvictionAndReadFailure(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "pages.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.Truncate(int64((hexCachePages + 2) * hexview.PageBytes)); err != nil {
		t.Fatal(err)
	}
	f.Close()
	h := newHarness(t, root)
	d := h.open("pages.bin")
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) != 0 })
	for i := 1; i < hexCachePages+2; i++ {
		offset := int64(i * hexview.PageBytes)
		h.a.requestHexPage(d, d.hex, offset)
		h.settle(func() bool { return d.hex.pages[offset] != nil })
	}
	if len(d.hex.pages) > hexCachePages {
		t.Fatal("cache exceeded limit")
	}
	if d.hex.pages[hexview.PageBytes] != nil {
		t.Fatal("old page was not evicted")
	}
	if err := os.Truncate(path, 0); err != nil {
		t.Fatal(err)
	}
	h.a.requestHexPage(d, d.hex, hexview.PageBytes)
	h.settle(func() bool { return d.hex.err != nil })
	if !h.tt.HasText("Retry") {
		t.Fatal("read error has no retry")
	}
}

func TestHexSelectionCopyJumpAndFind(t *testing.T) {
	root := writeTree(t, map[string]string{"a.bin": "\x00hello hello"})
	h := newHarness(t, root)
	d := h.open("a.bin")
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) != 0 })
	h.click("Byte 1 68")
	if err := h.tt.ClickWith(ui.Shift, "Byte 4 6C"); err != nil {
		t.Fatal(err)
	}
	h.tt.Frame()
	state := h.a.focus.hexControls[d.path]
	if start, end := state.bounds(); !state.selected || start != 1 || end != 5 {
		t.Fatalf("selection %d..%d", start, end)
	}
	h.click("Copy Hex")
	h.settle(func() bool { return h.tt.Clipboard() != "" })
	if got := h.tt.Clipboard(); got != "68 65 6C 6C" {
		t.Fatalf("clipboard %q", got)
	}
	state.jump = "7"
	h.click("Go")
	if state.cursor != 7 || state.anchor != 7 {
		t.Fatalf("jump: %+v", state)
	}
	state.selected = false
	state.query = "hex: 68 65 6c 6c 6f"
	h.click("Find Next")
	h.settle(func() bool { return !state.searching })
	if start, end := state.bounds(); start != 1 || end != 6 {
		t.Fatalf("match %d..%d: %s", start, end, state.message)
	}
	h.click("Find Next")
	h.settle(func() bool { return !state.searching })
	if start, end := state.bounds(); start != 7 || end != 12 {
		t.Fatalf("second match %d..%d", start, end)
	}
}

func TestHexKeyboardSelectionAndCopy(t *testing.T) {
	root := writeTree(t, map[string]string{"a.bin": "\x00hello"})
	h := newHarness(t, root)
	d := h.open("a.bin")
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) != 0 })
	h.click("Byte 1 68")
	h.tt.Key(ui.Shift, ui.KeyRight)
	h.tt.Frame()
	s := h.a.focus.hexControls[d.path]
	if start, end := s.bounds(); start != 1 || end != 3 {
		t.Fatalf("keyboard selection %d..%d", start, end)
	}
	h.tt.Key(ui.Cmd, ui.KeyC)
	h.settle(func() bool { return h.tt.Clipboard() != "" })
	if h.tt.Clipboard() != "68 65" {
		t.Fatalf("copy %q", h.tt.Clipboard())
	}
}
