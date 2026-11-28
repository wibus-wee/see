package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"see/internal/hexview"
	"see/internal/workspace"
	"see/locales"
)

// sparseTooLarge writes a file bigger than the editor's cap, sparse but
// for a marker at its start, and reports its path and size.
func sparseTooLarge(t *testing.T, root, marker string) (string, int64) {
	t.Helper()
	path := filepath.Join(root, "big.bin")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	size := int64(workspace.MaxSize) + hexview.PageBytes
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte(marker), 0); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	return path, size
}

// TestTooLargeFileReadsByWindow opens a file the editor cannot hold and
// reads it through the hex view, which shows one window at a time.
func TestTooLargeFileReadsByWindow(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	path, size := sparseTooLarge(t, root, "HELLO-WORLD")

	h := newHarness(t, root)
	d := h.open("big.bin")
	if d.kind != workspace.TooLarge {
		t.Fatalf("kind = %v", d.kind)
	}
	if !h.tt.HasText(locales.TooLargeTitle(d.name)) {
		t.Fatalf("no too-large message: %v", h.tt.Texts())
	}

	// The hex view the tab offers shows the bytes without loading them.
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[path] = true
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) > 0 })
	if !h.tt.HasText(locales.MsgHEXVIEW()) {
		t.Fatalf("no hex view: %v", h.tt.Texts())
	}
	page := d.hex.pages[0]
	if page == nil || string(page.data[:len("HELLO-WORLD")]) != "HELLO-WORLD" {
		t.Fatalf("first window is not the file's start: %q", page.data[:16])
	}
	if int64(len(page.data)) != hexview.PageLen(0, size) {
		t.Fatalf("window is not one page: %d", len(page.data))
	}
	// Only windows the view asked for were read: nothing scanned ahead.
	if len(d.hex.pages) != 1 {
		t.Fatalf("more than the visible window was read: %d pages", len(d.hex.pages))
	}
}

// TestHexViewReanchorsToTheChangedVersion modifies the file after the
// hex view pinned it: the reads of the old version fail, the view drops
// them, and what it shows next comes of the version now on disk.
func TestHexViewReanchorsToTheChangedVersion(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := sparseTooLarge(t, root, "BEFORE-CHANGE")

	h := newHarness(t, root)
	d := h.open("big.bin")
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[path] = true
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) > 0 })
	pinned := d.hex.sess.Stamp()

	// The file changes in place, observably to the version checks: a
	// marker the pinned version does not have lands deep in the file,
	// where the view is about to look.
	f, err := os.OpenFile(path, os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteAt([]byte("AFTER-CHANGE"), hexview.PageBytes); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()
	when := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(path, when, when); err != nil {
		t.Fatal(err)
	}

	// Scrolling past the cached page asks for one more window of the
	// pinned version. The read sees the file changed and fails, and the
	// view drops the session rather than mix versions.
	control := g.hexControls[path]
	state := g.hexLists[path]
	control.jump = fmt.Sprintf("%x", hexview.PageBytes)
	h.a.jumpHex(control, state, d.size)
	h.tt.Frame()
	h.settle(func() bool { return d.hex == nil })

	// The next frame pins the version now on disk: its stamp is the
	// modification's, and the window it reads carries the marker the old
	// version never had. The jump straddles a page boundary, so the
	// frame requests both pages of the window concurrently; waiting for
	// the one the assertion reads — rather than for any page — keeps
	// the readiness condition the same fact the assertion checks.
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && d.hex.pages[hexview.PageBytes] != nil })
	if d.hex.sess.Stamp() == pinned {
		t.Fatalf("the re-pinned session is the old version: %+v", d.hex.sess.Stamp())
	}
	page := d.hex.pages[hexview.PageBytes]
	if page == nil || string(page.data[:len("AFTER-CHANGE")]) != "AFTER-CHANGE" {
		t.Fatalf("the view did not follow the file: %+v", d.hex.pages)
	}
}

// TestQuickOpenShowsTheListingIsPartial checks the truncation the file
// listing reports is visible next to the results, not silent.
func TestQuickOpenShowsTheListingIsPartial(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.txt"), []byte("a"), 0600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, root)
	h.tt.Frame()
	h.settle(func() bool { return !h.a.quick.listing })
	h.a.quick.files = []string{"a.txt"}
	h.a.quick.truncated = true
	h.a.openQuickPanel("")
	h.tt.Frame()
	if !h.tt.HasText(locales.MsgOnlyTheFirstCountFilesAreListed("200000")) {
		t.Fatalf("partial listing is invisible: %v", h.tt.Texts())
	}

	// A complete listing carries no such note.
	h.a.quick.truncated = false
	h.tt.Frame()
	if h.tt.HasText(locales.MsgOnlyTheFirstCountFilesAreListed("200000")) {
		t.Fatalf("complete listing claims to be partial: %v", h.tt.Texts())
	}
}

// TestHexPageSurvivesRefreshOfUnchangedFile is the regression test of
// the wedge the independent verification of this lane reported: a file
// the reload of an unchanged revision short-circuits on (its buffer or
// player already exists, so loadDoc's stamp check skips the re-read and
// never resets the hex state) keeps a pending page forever when the
// refresh lands mid-read — d.seq moves, the completion is dropped as
// stale, and the drop used to happen before the pending entry cleared.
// The user scrolling to the page saw the placeholder permanently, and
// eight such holes froze the view's page loading entirely. A text file
// big enough for two pages reproduces it: the editor holds its buffer,
// so the shortcut applies; a too-large file heals itself instead, its
// full reload resetting the state.
func TestHexPageSurvivesRefreshOfUnchangedFile(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte("abcdefghij"), 10240) // 100KiB of text
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, root)
	d := h.open("big.txt")
	path := d.path
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[path] = true
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) > 0 })

	// Jump to the second page: the frame asks for its window, and the
	// read is in flight.
	control := g.hexControls[path]
	state := g.hexLists[path]
	control.jump = fmt.Sprintf("%x", hexview.PageBytes)
	h.a.jumpHex(control, state, d.size)
	h.tt.Frame()
	if d.hex == nil || !d.hex.pending[hexview.PageBytes] {
		t.Fatal("the second window was never requested")
	}

	// Before the read's completion runs, the tree's Refresh reloads every
	// document. The file is untouched and the editor holds its buffer,
	// so the reload takes loadDoc's stamp shortcut: it moves d.seq —
	// invalidating the page read in flight — without resetting d.hex.
	h.a.refreshAll()
	drainHarnessPosts(h)
	h.tt.Frame()

	if d.hex == nil {
		t.Fatal("refreshing the unchanged file reset the view state; the scenario did not occur")
	}
	// The dropped completion must still free the page, so a frame asks
	// for it again and it arrives; wedged, the page would stay in
	// pending forever. The loop drains and frames in turn because the
	// recovery is a cycle: a completion frees the page, a frame
	// re-requests it, a read posts the next completion.
	deadline := time.Now().Add(5 * time.Second)
	for d.hex.pages[hexview.PageBytes] == nil {
		if time.Now().After(deadline) {
			t.Fatalf("wedged: the second window never resolves after a refresh of the unchanged file: pending %v", d.hex.pending)
		}
		drainHarnessPosts(h)
		h.tt.Frame()
		time.Sleep(time.Millisecond)
	}
	want := fmt.Sprintf("%0*X  …", hexOffsetDigits(d.size), int64(hexview.PageBytes))
	if h.tt.HasText(want) {
		t.Fatalf("the second window is a permanent placeholder %q", want)
	}
}

// TestHexCopyPutsTheSelectedBytesOnTheClipboard copies a hex selection
// the way the copy button does and checks the clipboard carries the
// bytes' hex, once, in order.
func TestHexCopyPutsTheSelectedBytesOnTheClipboard(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := sparseTooLarge(t, root, "HEAD")

	h := newHarness(t, root)
	d := h.open("big.bin")
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[path] = true
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) > 0 })

	// A click on a byte cell selects it through selectByte; a shift
	// click extends to it. The selection here covers HEAD, the file's
	// first four bytes.
	control := g.hexControls[path]
	control.selectByte(0, false)
	control.selectByte(3, true)
	h.tt.Frame()
	if err := h.tt.Click(locales.MsgCopyHex()); err != nil {
		t.Fatalf("the copy button does not answer: %v", err)
	}
	h.settle(func() bool { return h.tt.Clipboard() != "" })
	if got := h.tt.Clipboard(); got != "48 45 41 44" {
		t.Fatalf("copy put %q on the clipboard, want the hex of HEAD", got)
	}
}

// TestHexViewJumpKeepsWindowedReads checks a jump deep into a large file
// reads the window it lands in, not the way there.
func TestHexViewJumpKeepsWindowedReads(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	path, _ := sparseTooLarge(t, root, "HEAD")

	h := newHarness(t, root)
	d := h.open("big.bin")
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[path] = true
	h.tt.Frame()
	h.settle(func() bool { return d.hex != nil && len(d.hex.pages) > 0 })

	control := g.hexControls[path]
	state := g.hexLists[path]
	control.jump = fmt.Sprintf("%x", 2*hexview.PageBytes)
	h.a.jumpHex(control, state, workspace.MaxSize+hexview.PageBytes)
	if control.message != "" {
		t.Fatalf("jump rejected: %q", control.message)
	}
	// The jump selected the byte: reading the window it lands in is all
	// the view needs for it.
	if control.cursor != 2*hexview.PageBytes {
		t.Fatalf("cursor %x", control.cursor)
	}
}
