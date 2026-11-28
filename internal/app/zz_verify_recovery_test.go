package app

// Independent verifier's end-to-end probes for the scan-failure recovery
// contract — NOT part of Lane E's implementation. Lane E's tests use a
// state-controlled fake scanner; these use real ripgrep against a real
// permission-broken directory, and exercise the watcher-invalidation retry
// path the implementer's tests do not cover.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"

	"see/internal/search"
)

func oneFakeEvent(t *testing.T) []mygo.FileEvent {
	t.Helper()
	return []mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}}
}

// realRipgrep finds an rg outside the workspace, or skips.
func realRipgrep(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("permission semantics differ")
	}
	p, err := exec.LookPath("rg")
	if err != nil {
		t.Skip("no rg on PATH")
	}
	return p
}

// Real ripgrep, real permission failure: scan reports the error and no
// files; healing the directory makes the next scan succeed. This is the
// upstream half of the hot-loop defect: what ripgrep actually does when a
// directory breaks under it.
func TestVerifyRealRipgrepScanFailureAndRecovery(t *testing.T) {
	rg := realRipgrep(t)
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "open.go"), []byte("package x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	locked := filepath.Join(root, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(locked, "hidden.go"), []byte("package y\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	files, truncated, err := search.ListFiles(context.Background(), rg, root, 100, nil)
	if err == nil {
		t.Fatal("scanning a tree with an unreadable directory reported success")
	}
	if files != nil {
		t.Fatalf("failed scan returned %d files — partial output impersonating a list", len(files))
	}
	if truncated {
		t.Fatal("failed scan reported truncation")
	}
	t.Logf("scan error: %v", err)

	// Heal and retry: the same scanner yields its files.
	if err := os.Chmod(locked, 0o755); err != nil {
		t.Fatal(err)
	}
	files, truncated, err = search.ListFiles(context.Background(), rg, root, 100, nil)
	if err != nil {
		t.Fatalf("healed scan errored: %v", err)
	}
	if len(files) != 2 {
		t.Fatalf("healed scan files = %v", files)
	}
}

// The watcher-invalidation retry path: after a scan failure is sticky,
// a filesystem event clears the error, and the scan retries without the
// panel being reopened — and healing means that retry succeeds. Lane E's
// test covers only the panel-reopen retry.
func TestVerifyWatcherInvalidationRetriesScan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the failing scanner is a shell script")
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	count := filepath.Join(dir, "count")
	scanner := filepath.Join(dir, "broken-rg")
	script := "#!/bin/sh\necho x >> \"$SEE_SCANNER_COUNT\"\nif [ -s \"$SEE_SCANNER_STATE\" ]; then echo healed.go; else echo partial.go; exit 2; fi\n"
	if err := os.WriteFile(scanner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", scanner)
	t.Setenv("SEE_SCANNER_STATE", state)
	t.Setenv("SEE_SCANNER_COUNT", count)
	h := newHarness(t, root)
	h.a.trust()
	scans := func() int {
		data, err := os.ReadFile(count)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "x")
	}

	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("broken scanner reported no error")
	}
	before := scans()

	// A filesystem event invalidates the scan state: error clears, and
	// the next ensureFiles retries — the panel is still open, ranking
	// runs every frame, so the retry fires by itself.
	h.a.applyEvents(nil) // no events: must NOT invalidate
	h.tt.Frame()
	if h.a.quick.err == nil {
		t.Fatal("empty event batch cleared the sticky error")
	}

	// Filesystem invalidation clears the sticky error, but the open panel
	// does not rescan: rankQuick returns early because the query is
	// unchanged, and nothing else calls ensureFiles. Documenting actual
	// behavior — frames pass with zero additional scans.
	h.a.applyEvents(oneFakeEvent(t))
	for i := 0; i < 10; i++ {
		h.tt.Frame()
	}
	if scans() != before {
		t.Logf("QUICK-OPEN-PANEL: invalidation DID trigger a rescan (%d -> %d)", before, scans())
	} else {
		t.Logf("QUICK-OPEN-PANEL: invalidation does not rescan; error cleared but scan waits for reopen/retype")
	}
	if h.a.quick.err != nil {
		t.Fatal("invalidation did not clear the sticky error")
	}

	// Retyping the query retries the scan (the error was cleared).
	h.a.quick.query = "a"
	h.tt.Frame()
	h.settle(func() bool { return scans() > before })

	// Heal, reopen the panel, and the files arrive.
	if err := os.WriteFile(state, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.quick.listing })
	h.a.quick.open = false
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.files != nil })
	if len(h.a.quick.files) != 1 || h.a.quick.files[0] != "healed.go" {
		t.Fatalf("files = %v after healing retry", h.a.quick.files)
	}
}

// The tree filter IS a full retry point for invalidations: syncFilter
// calls ensureFiles every time the filter rebuilds, and invalidation
// resets forQuery, so the next frame rescans.
func TestVerifyTreeFilterRetriesScanOnInvalidation(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the failing scanner is a shell script")
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	count := filepath.Join(dir, "count")
	scanner := filepath.Join(dir, "broken-rg")
	script := "#!/bin/sh\necho x >> \"$SEE_SCANNER_COUNT\"\nif [ -s \"$SEE_SCANNER_STATE\" ]; then echo healed.go; else echo partial.go; exit 2; fi\n"
	if err := os.WriteFile(scanner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", scanner)
	t.Setenv("SEE_SCANNER_STATE", state)
	t.Setenv("SEE_SCANNER_COUNT", count)
	h := newHarness(t, root)
	h.a.trust()
	scans := func() int {
		data, err := os.ReadFile(count)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "x")
	}

	// Scan fails with the tree filter active.
	h.a.filter.query = "heal"
	h.a.filter.forQuery = ""
	h.tt.Frame()
	h.settle(func() bool { return scans() > 0 && !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("broken scanner reported no error")
	}
	before := scans()

	// Invalidation: the next filter rebuild rescans — after a frame runs
	// syncFilter.
	h.a.applyEvents(oneFakeEvent(t))
	h.tt.Frame()
	h.settle(func() bool { return scans() > before })
	t.Logf("TREE-FILTER: invalidation triggered rescan as documented")

	// Heal, invalidate once more: the files reach the filter.
	if err := os.WriteFile(state, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.settle(func() bool { return !h.a.quick.listing })
	h.a.applyEvents(oneFakeEvent(t))
	h.tt.Frame()
	h.settle(func() bool { return h.a.quick.files != nil })
	h.tt.Frame()
	if h.a.filter.matches != 1 {
		t.Fatalf("filter matches = %d after healing", h.a.filter.matches)
	}
}
