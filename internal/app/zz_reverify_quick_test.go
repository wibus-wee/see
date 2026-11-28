package app

// Independent re-verification probes for the quick-open invalidation
// retry respin (commit f649818). Written by the re-verifier: the harness
// is the project's own, but every scenario is constructed independently
// of the implementer's TestQuickOpenInvalidationRetriesScan, and the
// questions asked are the ones that test did not answer — what two
// invalidations in a row do, what an invalidation mid-scan does, whether
// the hot loop can re-arm through frames, and whether non-file queries
// keep their results.

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/egoist/mygo"
)

// scriptedScanner installs a shell script as SEE_RG that counts its
// invocations and reports failure until stateFile is non-empty. It
// returns the invocation counter. probeSleepMs, when positive, makes
// every invocation sleep that long — slow enough that an invalidation
// can land mid-scan.
func scriptedScanner(t *testing.T, probeSleepMs int) func() int {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the failing scanner is a shell script")
	}
	dir := t.TempDir()
	state := filepath.Join(dir, "state")
	count := filepath.Join(dir, "count")
	scanner := filepath.Join(dir, "probe-rg")
	script := "#!/bin/sh\necho x >> \"$SEE_PROBE_COUNT\"\n"
	if probeSleepMs > 0 {
		script += fmt.Sprintf("sleep 0.%03d\n", probeSleepMs)
	}
	script += "if [ -s \"$SEE_PROBE_STATE\" ]; then echo healed.go; else echo partial.go; exit 2; fi\n"
	if err := os.WriteFile(scanner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", scanner)
	t.Setenv("SEE_PROBE_STATE", state)
	t.Setenv("SEE_PROBE_COUNT", count)
	scans := func() int {
		data, err := os.ReadFile(count)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "x")
	}
	return scans
}

func writeProbeEvent() []mygo.FileEvent {
	return []mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}}
}

// One invalidation means one retry; a second invalidation after the
// retry failed sticky means a second scan — retries are per invalidation,
// not deduplicated away, and not multiplied either. Between them, plain
// frames change nothing: the hot loop must stay dead.
func TestReverifyTwoInvalidationsRetryTwiceNotMore(t *testing.T) {
	scans := scriptedScanner(t, 0)
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	h.a.trust()

	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("the broken scanner reported no error")
	}
	base := scans()
	if base == 0 {
		t.Fatal("the scanner never ran")
	}

	// First invalidation: exactly one retry.
	h.a.applyEvents(writeProbeEvent())
	h.tt.Frame()
	h.settle(func() bool { return scans() > base && !h.a.quick.listing })
	if got := scans() - base; got != 1 {
		t.Fatalf("first invalidation caused %d scans, want exactly 1", got)
	}
	if h.a.quick.err == nil {
		t.Fatal("the retried failure did not go sticky again")
	}

	// Twenty frames with no events: no scan. The hot loop must not
	// re-arm through ranking — after a failed retry the ranking is keyed
	// to the query again and nothing re-enters ensureFiles.
	for i := 0; i < 20; i++ {
		h.tt.Frame()
	}
	if got := scans() - base; got != 1 {
		t.Fatalf("20 frames after the failed retry caused %d scans total, want 1 — hot loop re-armed", got)
	}

	// Second invalidation: exactly one more scan — deduplication across
	// invalidations would swallow this retry, multiplication would add more.
	h.a.applyEvents(writeProbeEvent())
	h.tt.Frame()
	h.settle(func() bool { return scans() > base+1 && !h.a.quick.listing })
	if got := scans() - base; got != 2 {
		t.Fatalf("two invalidations caused %d scans, want exactly 2", got)
	}
	for i := 0; i < 10; i++ {
		h.tt.Frame()
	}
	if got := scans() - base; got != 2 {
		t.Fatalf("frames after the second failed retry added scans: %d total, want 2", got)
	}
}

// An invalidation landing while the scan it triggered is still running
// must not stack a second scan: q.listing is the dedup. What matters is
// that a burst of invalidations converges to one in-flight scan, not a
// queue of retries — the event-driven shape depends on it.
func TestReverifyInvalidationDuringScanDoesNotStack(t *testing.T) {
	scans := scriptedScanner(t, 50) // 50ms per scan: slow enough to hit mid-flight
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	h.a.trust()

	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	base := scans()

	// Trigger the retry scan, then fire a burst of invalidations while
	// it is (almost certainly) still running. The frame re-ranks and
	// starts the scan; the burst lands before the 50ms scan posts back.
	h.a.applyEvents(writeProbeEvent())
	h.tt.Frame() // re-rank starts the scan
	h.a.applyEvents(writeProbeEvent())
	h.a.applyEvents(writeProbeEvent())
	for i := 0; i < 5; i++ {
		h.tt.Frame() // frames while the scan runs: must not add scans
	}
	h.settle(func() bool { return !h.a.quick.listing })
	h.settle(func() bool { return scans() > base })
	// Drain anything queued, then measure.
	for i := 0; i < 10; i++ {
		h.tt.Frame()
	}
	h.settle(func() bool { return !h.a.quick.listing })
	got := scans() - base
	if got > 1 {
		t.Fatalf("a burst of invalidations mid-scan produced %d scans, want at most 1 (no stacking)", got)
	}
}

// Command-mode queries never consume the file list: their invalidation
// must neither lose the command results nor trigger a scan. The respin
// scopes the ranking invalidation to file-consuming queries; a regression
// here would re-scan workspaces for users who never look at files.
func TestReverifyCommandQueryKeepsResultsOnInvalidation(t *testing.T) {
	scans := scriptedScanner(t, 0)
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	h := newHarness(t, root)
	h.a.trust()

	h.a.openQuickPanel(">font")
	h.tt.Frame()
	h.tt.Frame()
	before := len(h.a.quick.results)
	if before == 0 {
		t.Fatal("no command results for '>font' — probe broken")
	}
	base := scans()
	h.a.applyEvents(writeProbeEvent())
	h.tt.Frame()
	h.tt.Frame()
	if len(h.a.quick.results) != before {
		t.Fatalf("invalidation changed command-query results: %d -> %d", before, len(h.a.quick.results))
	}
	for i := 0; i < 5; i++ {
		h.tt.Frame()
	}
	if scans() != base {
		t.Fatal("invalidation under a command query scanned the file list")
	}
}
