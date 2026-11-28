// Re-verification tests of Lane D written by the re-verifying engineer,
// after the verification round's fixes. They adjudicate the wedge
// scenarios: a too-large file's reload always takes the full re-read
// path, which resets the hex state and heals the view, while a text
// file (or a video) takes loadDoc's stamp shortcut, which resets
// nothing, so only clearing the pending entry of a dropped completion
// frees the page.
package app

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"see/internal/hexview"
	"see/internal/workspace"
)

// jumpToHexPage opens path in a hex view and jumps to the second page,
// leaving its read in flight.
func jumpToHexPage(t *testing.T, h *harness, d *doc) {
	t.Helper()
	g := h.a.focus
	if g.hexPreview == nil {
		g.hexPreview = map[string]bool{}
	}
	g.hexPreview[d.path] = true
	h.tt.Frame()
	deadline := time.Now().Add(10 * time.Second)
	for d.hex == nil || len(d.hex.pages) == 0 {
		if time.Now().After(deadline) {
			t.Fatal("the first window never arrived")
		}
		drainHarnessPosts(h)
		h.tt.Frame()
		time.Sleep(time.Millisecond)
	}
	control := g.hexControls[d.path]
	state := g.hexLists[d.path]
	control.jump = fmt.Sprintf("%x", hexview.PageBytes)
	h.a.jumpHex(control, state, d.size)
	h.tt.Frame()
	if d.hex == nil || !d.hex.pending[hexview.PageBytes] {
		t.Fatal("the second window was never requested")
	}
}

// TestReverifyTooLargeRefreshSelfHeals is the verifier's reproduction of
// the wedged page — a too-large file jumped to a second page, refreshed
// while its read is in flight — checked for what the user sees after
// the refresh completes. The reload of a too-large file never takes
// loadDoc's stamp shortcut: the editor holds no buffer for it, so the
// goroutine re-reads the file and the completion drops the hex state
// (app.go's `d.hex = nil` after a.loaded), whatever the refresh saw.
// The next frame pins the file again and the page arrives — the hole
// the verifier observed was transient. The orphaned state's pending
// entry is an implementation artifact, logged for both orderings of the
// completion's delete; it is not what the user sees.
func TestReverifyTooLargeRefreshSelfHeals(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "small.txt"), []byte("small"), 0600); err != nil {
		t.Fatal(err)
	}
	_, _ = sparseTooLarge(t, root, "HEAD")

	h := newHarness(t, root)
	d := h.open("big.bin")
	jumpToHexPage(t, h, d)
	oldHex := d.hex

	// The refresh lands while the second window's read is in flight.
	h.a.refreshAll()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if d.hex != nil && d.hex != oldHex && d.hex.pages[hexview.PageBytes] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("a too-large file did not self-heal: hex %p old %p pending %v", d.hex, oldHex, d.hex.pending)
		}
		drainHarnessPosts(h)
		h.tt.Frame()
		time.Sleep(time.Millisecond)
	}
	t.Logf("self-healed: the reload reset the hex state and the second window arrived; orphaned state pending %v", oldHex.pending)
	h.tt.Frame()
	want := fmt.Sprintf("%0*X  …", hexOffsetDigits(d.size), int64(hexview.PageBytes))
	if h.tt.HasText(want) {
		t.Fatalf("the second window is a placeholder %q", want)
	}
}

// TestReverifyTextPageHealsAfterRefresh is the wedge scenario that
// lasts: the text file's editor buffer makes the reload of an unchanged
// revision take loadDoc's stamp shortcut, so nothing resets the hex
// state, and a pending entry the dropped completion failed to clear
// keeps the page a placeholder forever. A frame after the refresh asks
// for the page again — the dropped completion freed it — and the window
// arrives. Asserted independently of TestHexPageSurvivesRefreshOfUnchangedFile:
// through the placeholder the user would face.
func TestReverifyTextPageHealsAfterRefresh(t *testing.T) {
	root := t.TempDir()
	big := bytes.Repeat([]byte("abcdefghij"), 10240) // 100KiB of text
	if err := os.WriteFile(filepath.Join(root, "big.txt"), big, 0600); err != nil {
		t.Fatal(err)
	}

	h := newHarness(t, root)
	d := h.open("big.txt")
	jumpToHexPage(t, h, d)
	hexBefore := d.hex

	h.a.refreshAll()
	// The page the refresh dropped must re-arrive. On a text file the
	// state survives — the reload of an unchanged revision ends in the
	// stamp shortcut — so a reset state means the scenario did not
	// happen, and a wedged pending entry means the page never resolves.
	deadline := time.Now().Add(10 * time.Second)
	for {
		drainHarnessPosts(h)
		h.tt.Frame()
		if d.hex == nil || d.hex != hexBefore {
			t.Fatal("the hex state was reset; the stamp shortcut did not apply, the test says nothing")
		}
		if d.hex.pages[hexview.PageBytes] != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("wedged: the second window never resolves, a permanent placeholder: pending %v", d.hex.pending)
		}
		time.Sleep(time.Millisecond)
	}
	h.tt.Frame()
	want := fmt.Sprintf("%0*X  …", hexOffsetDigits(d.size), int64(hexview.PageBytes))
	if h.tt.HasText(want) {
		t.Fatalf("the second window is a permanent placeholder %q", want)
	}
}

// TestReverifyMarkdownHeadingLineStatBound checks the markdown anchor
// lookup stats its target before reading: a file over the 48MiB cap is
// never held in memory. The size is observed through allocation —
// reading the file whole multiplies the heap by the file's size, while
// a stat answers in bytes.
func TestReverifyMarkdownHeadingLineStatBound(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "huge.md")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(workspace.MaxSize + 1024*1024); err != nil {
		f.Close()
		t.Fatal(err)
	}
	f.Close()

	a := &App{}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	line := a.markdownHeadingLine(path, "install")
	runtime.ReadMemStats(&after)
	if line != -1 {
		t.Fatalf("an over-cap target still answered a line: %d", line)
	}
	if growth := after.TotalAlloc - before.TotalAlloc; growth > 1<<20 {
		t.Fatalf("the over-cap target was read before being dropped: %d bytes allocated", growth)
	}

	// Within the cap the lookup still reads, finds, and answers.
	small := filepath.Join(root, "small.md")
	if err := os.WriteFile(small, []byte("# Install\n\nbody\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if line := a.markdownHeadingLine(small, "install"); line != 0 {
		t.Fatalf("a within-cap target answered line %d, want 0", line)
	}
}
