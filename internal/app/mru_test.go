package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/egoist/mygo/ui"
)

func quickResultPaths(a *App) []string {
	a.rankQuick()
	paths := make([]string, len(a.quick.results))
	for i, r := range a.quick.results {
		paths[i] = r.Path
	}
	return paths
}

func TestQuickOpenEmptyListsRecentFiles(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b", "c.txt": "c", "d.txt": "d",
	}))
	h.open("a.txt")
	h.open("b.txt")
	h.open("c.txt")
	h.a.openQuickly()
	h.tt.Frame()
	got := quickResultPaths(h.a)
	want := []string{"b.txt", "a.txt"} // the active file sits it out
	if !slices.Equal(got, want) {
		t.Fatalf("empty query recents: got %v want %v", got, want)
	}
}

func TestQuickOpenEmptyListsClosedAndDropsGone(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b", "c.txt": "c", "gone.txt": "g",
	}))
	a := h.open("a.txt")
	h.open("b.txt")
	h.open("gone.txt")
	if err := os.Remove(filepath.Join(h.a.root, "gone.txt")); err != nil {
		t.Fatal(err)
	}
	h.a.closeTab(h.a.focus, a.path)
	h.a.openQuickly()
	h.tt.Frame()
	got := quickResultPaths(h.a)
	// closed a.txt stays for reopening; gone.txt left disk and does not
	if !slices.Equal(got, []string{"b.txt", "a.txt"}) {
		t.Fatalf("recents after close: %v", got)
	}
	// Choosing a closed file opens it again.
	h.a.chooseQuick(1)
	h.settle(func() bool {
		d := h.a.docs[a.path]
		return d != nil && !d.loading
	})
	if h.a.activePath() != a.path {
		t.Fatalf("reopen did not activate %s", a.path)
	}
}

func TestQuickOpenTypedQueryStillRanksFiles(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"src/alpha.go": "package a", "src/beta.go": "package b",
	}))
	h.open("src/beta.go")
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Type("alpha")
	h.tt.Frame()
	got := quickResultPaths(h.a)
	if !slices.Equal(got, []string{"src/alpha.go"}) {
		t.Fatalf("typed query: %v", got)
	}
}

func TestCtrlTabFlipFlopsRecentPair(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b",
	}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	_ = b
	g := h.a.focus
	h.tt.Frame()
	if g.active != b.path {
		t.Fatal("setup: b not active")
	}
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != a.path {
		t.Fatal("first Ctrl+Tab did not reach the previous tab")
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != b.path {
		t.Fatal("Ctrl+Tab did not flip back")
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
}

func TestCtrlTabWalksDeeperWhileHeld(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b", "c.txt": "c",
	}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	c := h.open("c.txt")
	g := h.a.focus
	h.tt.Frame()
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != b.path {
		t.Fatal("first step did not reach b")
	}
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != a.path {
		t.Fatal("second step did not walk deeper to a")
	}
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyTab)
	h.tt.Frame()
	if g.active != b.path {
		t.Fatal("Ctrl+Shift+Tab did not step back toward the latest")
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	if want := []string{a.path, c.path, b.path}; !slices.Equal(g.mru, want) {
		t.Fatalf("landing put the reached tab most recent: %v want %v", g.mru, want)
	}
	// The next press flips between the last two again.
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != c.path {
		t.Fatal("after landing, Ctrl+Tab did not flip to c")
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
}

func TestCtrlTabNewWalkStartsFromLatest(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b", "c.txt": "c",
	}))
	a := h.open("a.txt")
	h.open("b.txt")
	c := h.open("c.txt")
	g := h.a.focus
	h.tt.Frame()
	// Two presses while held walk to a; a fresh hold starts from the
	// landing spot, not where the last walk left off.
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != a.path {
		t.Fatal("held presses did not reach a")
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != c.path {
		t.Fatalf("new walk did not start from the latest: active %s want %s", g.active, c.path)
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
}

func TestCycleTabLinearFallback(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{"a.txt": "a"}))
	h.open("a.txt")
	g := h.a.focus
	g.mru = nil // no recency history: cycle left to right
	h.a.cycleTab(1)
	if g.active == "" {
		t.Fatal("single-tab cycle lost the active tab")
	}
	// A second tab opened without activation history still cycles.
	b := filepath.Join(h.a.root, "b.txt")
	if err := os.WriteFile(b, []byte("b"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.addTab(b, len(g.order))
	g.mru = nil
	h.a.cycleTab(1)
	if g.active != b {
		t.Fatal("linear fallback did not move to the next tab")
	}
}

func TestCloseDuringCycle(t *testing.T) {
	h := newHarness(t, writeTree(t, map[string]string{
		"a.txt": "a", "b.txt": "b", "c.txt": "c",
	}))
	a := h.open("a.txt")
	b := h.open("b.txt")
	c := h.open("c.txt")
	g := h.a.focus
	h.tt.Frame()
	h.tt.HoldModifiers(ui.Ctrl)
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != b.path {
		t.Fatal("walk did not reach b")
	}
	h.a.closeTab(g, b.path)
	if g.active != c.path {
		t.Fatalf("closing the walked-to tab did not fall back: %s", g.active)
	}
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	h.tt.Frame()
	if g.active != a.path {
		t.Fatalf("walk did not continue to a: %s", g.active)
	}
	h.tt.HoldModifiers(0)
	h.tt.Frame()
}
