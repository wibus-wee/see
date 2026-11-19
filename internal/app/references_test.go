package app

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"see/internal/lsp"
	"see/internal/uri"

	"github.com/egoist/mygo/ui/editor"
)

// referencesFixture is a workspace whose a.go uses source, defined in
// b.go and used in c.go, so Find References of it finds several files.
func referencesFixture() map[string]string {
	return map[string]string{
		"a.go": "package a\n\nvar _ = source\n",
		"b.go": "package a\n\nvar source int\n",
		"c.go": "package a\n\nvar other = source + 1\n",
	}
}

// findReferencesOf asks the fake server for references of the symbol at
// the caret of a.go and waits for the popover's list, as Shift+F12 leaves it.
func findReferencesOf(t *testing.T, h *harness, d *doc) {
	t.Helper()
	h.fc.references = func(ctx context.Context, docURI string, pos lsp.Position) ([]lsp.Location, error) {
		root := h.a.root
		rng := func(line, c0, c1 int) lsp.Range {
			return lsp.Range{Start: lsp.Position{Line: line, Character: c0}, End: lsp.Position{Line: line, Character: c1}}
		}
		return []lsp.Location{
			{URI: uri.FromPath(filepath.Join(root, "a.go")), Range: rng(2, 8, 14)},
			{URI: uri.FromPath(filepath.Join(root, "b.go")), Range: rng(2, 4, 10)},
			{URI: uri.FromPath(filepath.Join(root, "c.go")), Range: rng(2, 12, 18)},
		}, nil
	}
	h.a.cfg.Trust(h.a.root)
	h.a.focus = h.a.groups()[0]
	h.a.activeEditor().GoTo(editor.Pos{Line: 2, Col: strings.Index("var _ = source", "source") + 2})
	h.tt.Frame()
	h.a.findUsages(false)
	h.settle(func() bool { return h.a.choices != nil && h.a.choices.previewEditor != nil })
	if len(h.a.choices.locs) != 3 {
		t.Fatalf("references: %d", len(h.a.choices.locs))
	}
}

// Pinning the popover's list moves it into the bottom panel: the peek
// closes, the panel opens on the references view, and the list survives.
func TestPinReferencesMovesListToPanel(t *testing.T) {
	root := writeTree(t, referencesFixture())
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	d := h.open("a.go")
	findReferencesOf(t, h, d)

	h.a.pinReferences(h.a.choices)
	h.tt.Frame()

	if h.a.choices != nil {
		t.Fatal("the peek's list was not closed")
	}
	if !h.a.term.open || h.a.term.view != panelReferences {
		t.Fatalf("panel: open=%v view=%v", h.a.term.open, h.a.term.view)
	}
	run := h.a.references.activeRun()
	if run == nil || len(run.choices.locs) != 3 {
		t.Fatalf("pinned run: %+v", run)
	}
	if run.symbol != "source" {
		t.Fatalf("run symbol: %q", run.symbol)
	}
}

// The filter narrows the rows to the paths, names or line text matching
// it, and the count beside the field says how many pass.
func TestReferencesFilterNarrowsList(t *testing.T) {
	root := writeTree(t, referencesFixture())
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	d := h.open("a.go")
	findReferencesOf(t, h, d)

	h.a.pinReferences(h.a.choices)
	h.a.references.filter = "b.go"
	n, files := referenceCount(h.a.references.activeRun().choices, h.a.references.filter)
	if n != 1 || files != 1 {
		t.Fatalf("filtered: %d in %d", n, files)
	}
	h.a.references.filter = "other"
	n, _ = referenceCount(h.a.references.activeRun().choices, h.a.references.filter)
	if n != 1 {
		t.Fatalf("line text filter: %d", n)
	}
	h.a.references.filter = "zzz"
	n, _ = referenceCount(h.a.references.activeRun().choices, h.a.references.filter)
	if n != 0 {
		t.Fatalf("unmatched filter: %d", n)
	}
}

// Space marks the row under the cursor read and previews the next, so
// the list is walked without the mouse and without leaving the panel.
func TestReferencesSpaceWalksRows(t *testing.T) {
	root := writeTree(t, referencesFixture())
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	d := h.open("a.go")
	findReferencesOf(t, h, d)

	h.a.pinReferences(h.a.choices)
	rl := &h.a.references
	rl.cursor = 0

	h.a.referenceAdvance()
	if !rl.read[0] || rl.cursor != 1 {
		t.Fatalf("after space: cursor=%d read=%v", rl.cursor, rl.read)
	}
	h.a.referenceAdvance()
	h.a.referenceAdvance()
	if rl.cursor != 0 || !rl.read[0] || !rl.read[1] || !rl.read[2] {
		t.Fatalf("wrapped walk: cursor=%d read=%v", rl.cursor, rl.read)
	}
}

// A list asked before the document changed is stale: the panel drops it
// on the next frame rather than show positions that no longer hold.
func TestReferencesStaleListDropped(t *testing.T) {
	root := writeTree(t, referencesFixture())
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	d := h.open("a.go")
	findReferencesOf(t, h, d)

	h.a.pinReferences(h.a.choices)
	run := h.a.references.activeRun()
	if h.a.referencesStale(run) {
		t.Fatal("fresh list is stale")
	}
	st := h.a.activeEditor()
	st.Insert("x")
	h.tt.Frame()
	if !h.a.referencesStale(run) {
		t.Fatal("edited document left the list fresh")
	}
	if h.a.references.activeRun() != nil {
		t.Fatal("stale list stayed active")
	}
}

// A second pin becomes the active list; the first stays in recents for
// the clock button, up to the cap of kept runs.
func TestSecondPinKeepsRecent(t *testing.T) {
	root := writeTree(t, referencesFixture())
	h := newHarness(t, root)
	defer h.a.lsp.StopWait(5e9)
	d := h.open("a.go")
	findReferencesOf(t, h, d)
	h.a.pinReferences(h.a.choices)
	first := h.a.references.activeRun()

	findReferencesOf(t, h, d)
	h.a.pinReferences(h.a.choices)

	rl := &h.a.references
	if len(rl.runs) != 2 || rl.runs[0] == first || rl.runs[1] != first {
		t.Fatalf("runs: %d first-kept=%v", len(rl.runs), len(rl.runs) > 1 && rl.runs[1] == first)
	}

	// Recents hold at most maxReferenceRuns.
	for i := 0; i < maxReferenceRuns+2; i++ {
		h.a.pushReferenceRun(&referenceRun{choices: &defChoices{title: fmt.Sprintf("r%d", i)}})
	}
	if len(rl.runs) != maxReferenceRuns {
		t.Fatalf("recents cap: %d", len(rl.runs))
	}
}
