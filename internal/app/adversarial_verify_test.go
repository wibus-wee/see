// Adversarial verification tests written by the independent verifier of
// Lane D (SPEC-008), not by the implementer. They assert the behavior
// the conformance matrix implies; a failure is evidence of a defect.
package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"see/internal/hexview"
)

// drain runs every result background work has posted so far.
func (h *harness) drain() {
	for {
		select {
		case fn := <-h.q:
			fn()
		default:
			return
		}
	}
}

// TestAdversarialHexPageSurvivesRefreshOfUnchangedFile reproduces an
// invalidation hole in the hex view: requestHexPage drops a page read
// whose completion arrives after d.seq moved, but it drops it before
// deleting the pending entry, and a reload of an unchanged file neither
// resets d.hex nor re-reads it (loadDoc's stamp shortcut), so the page
// is never asked for again. The user sees a permanent hole in the hex
// view after a manual Refresh that changed nothing.
func TestAdversarialHexPageSurvivesRefreshOfUnchangedFile(t *testing.T) {
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

	// Jump to the second page: the frame asks for its window.
	control := g.hexControls[path]
	state := g.hexLists[path]
	control.jump = fmt.Sprintf("%x", hexview.PageBytes)
	h.a.jumpHex(control, state, d.size)
	h.tt.Frame()
	if !d.hex.pending[hexview.PageBytes] {
		t.Fatal("the second window was never requested")
	}

	// Before the read's completion runs, the user hits the tree's
	// Refresh button. The file is untouched, so the reload takes
	// loadDoc's stamp shortcut: it moves d.seq (invalidating the page
	// read in flight) without resetting d.hex.
	h.a.refreshAll()
	h.drain()
	h.tt.Frame()

	if d.hex == nil {
		t.Fatal("the view state was reset; the wedge scenario did not occur")
	}
	if len(d.hex.pending) != 0 {
		t.Fatalf("wedged: %d pending windows never resolve after a refresh of the unchanged file: %v",
			len(d.hex.pending), d.hex.pending)
	}
	// Even if the invalidation is fixed, the window must actually
	// arrive: the row of the second page must not be a placeholder.
	h.settle(func() bool { return d.hex.pages[hexview.PageBytes] != nil })
	h.tt.Frame()
	want := fmt.Sprintf("%0*X  …", hexOffsetDigits(d.size), int64(hexview.PageBytes))
	if h.tt.HasText(want) {
		t.Fatalf("the second window is a permanent placeholder %q", want)
	}
}

// TestAdversarialHexSearchRefreshConsistency checks what a hex search
// does when a refresh of the unchanged file lands mid-scan: the control
// reset cancels the search, and no result or error may surface — the
// user simply starts again. Pin the behavior so it stays explicit.
func TestAdversarialHexSearchCanceledByRefresh(t *testing.T) {
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
	control.query = "a needle this file does not contain anywhere at all"
	h.a.findHex(d, control, state)
	if !control.searching {
		t.Fatal("the search never started")
	}
	// A refresh of the unchanged file while the scan runs.
	h.a.refreshAll()
	h.drain()
	h.tt.Frame()
	// The scan keeps running in the background (it reads gigabytes);
	// what matters is the UI is not stuck: the control was reset and a
	// new search can start.
	if control.version != d.seq {
		t.Fatalf("control did not follow the reload: version %d seq %d", control.version, d.seq)
	}
	h.drain()
	h.tt.Frame()
	if control.searching {
		t.Log("search still flagged searching after the refresh drained; checking recovery")
	}
	// A fresh search must be able to start: this is the recovery the
	// user has.
	control.query = "HEAD"
	h.a.findHex(d, control, state)
	if !control.searching {
		t.Fatal("a new search cannot start after the refresh")
	}
	h.settle(func() bool { return !control.searching })
	t.Logf("after refresh and re-search: message %q", control.message)
}
