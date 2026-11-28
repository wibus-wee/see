package app

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo"

	"see/internal/config"
	"see/internal/langserver"
	"see/internal/lsp"
)

// The threat model (SECURITY-THREAT-MODEL.md) promises three things these
// tests verify through the app's own machinery: a workspace that was not
// trusted runs nothing it brought, the secrets a user entrusts to a
// language server's configuration do not end up in the log everyone reads,
// and a scan that fails is retried instead of freezing into an empty list.

// logText is the log as the output panel shows it.
func (h *harness) logText() string {
	var b strings.Builder
	for _, e := range h.a.logs.Snapshot() {
		b.WriteString(e.Source)
		b.WriteString(": ")
		b.WriteString(e.Message)
		b.WriteString("\n")
	}
	return b.String()
}

// A configured server can carry credentials in its environment and even
// in its arguments — tokens the server needs and the user typed into the
// configuration. Whatever happens to that server, See itself must never
// pass them into the log: the log is a place errors are read from, pasted
// into issues, and screenshotted (REQ-SEC-030). The server starting, the
// server failing to start, and the server's whole lifecycle are the paths
// that log.
func TestConfiguredServerSecretsStayOutOfTheLog(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n"})
	const token = "see-test-token-9f8e7d6c5b4a3210"
	h := newHarness(t, "")
	h.a.cfg.LSP = []config.LSPConfig{{
		Name:       "secret-lsp",
		Extensions: []string{".rs"},
		Command:    []string{"see-no-such-secret-server-xyz", "--auth-token", token},
		Env:        map[string]string{"LSP_AUTH_TOKEN": token},
	}}
	h.a.openWorkspace(root)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })

	// First lifecycle: the binary is missing, so startup fails — the
	// path most prone to echoing what it tried to run.
	m := h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("no server for a configured extension")
	}
	h.a.trust()
	h.open("main.rs")
	h.settle(func() bool {
		s, _ := m.Status()
		return s == langserver.Failed
	})

	// Second lifecycle: the server starts, runs, and is asked to stop —
	// state transitions and shutdown are the other paths that log.
	m.Stop()
	h.a.customLSPs[h.a.customIndex(m)] = langserver.New(langserver.Options{
		Name: "secret-lsp", Root: root,
		Start: func(ctx context.Context, _ string, _ []lsp.WorkspaceFolder, _ func(error)) (langserver.Client, error) {
			return h.fc, nil
		},
		Logf: h.a.serverLog("secret-lsp"),
	})
	m = h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("the replacement server did not register")
	}
	h.open("main.rs")
	h.settle(func() bool {
		s, _ := m.Status()
		return s == langserver.Ready
	})
	m.Stop()

	h.settle(func() bool { return len(h.a.logs.Snapshot()) > 0 })
	if log := h.logText(); strings.Contains(log, token) {
		t.Fatalf("the server's secret reached the log:\n%s", log)
	}
}

// The command of a configured server is the user's own configuration, not
// the workspace's: even configured, a binary that lives inside the
// workspace must not run, because checking out a repository must never
// hand the repository code execution on the user's machine — the threat
// model's first in-scope item.
func TestConfiguredServerInsideWorkspaceNeverStarts(t *testing.T) {
	root := writeTree(t, map[string]string{"main.rs": "fn main() {}\n"})
	inside := filepath.Join(root, "tools", "evil-lsp")
	h := newHarness(t, "")
	h.a.cfg.LSP = []config.LSPConfig{{
		Name:       "evil-lsp",
		Extensions: []string{".rs"},
		Command:    []string{inside},
	}}
	h.a.openWorkspace(root)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	m := h.a.serverFor(filepath.Join(root, "main.rs"))
	if m == nil {
		t.Fatal("no server for a configured extension")
	}
	h.a.trust()
	h.open("main.rs")
	h.settle(func() bool {
		s, _ := m.Status()
		return s == langserver.Failed || s == langserver.Ready
	})
	if s, _ := m.Status(); s == langserver.Ready {
		t.Fatal("a server inside the workspace started")
	}
}

// A scan that fails leaves files nil — no list exists, and an empty
// list would pass for an empty workspace — while the error stays
// visible and sticky: frames of an open panel must not re-scan
// forever, and opening the panel is the user's retry (REQ-LIST-030).
func TestQuickOpenRetriesAfterAScanError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the failing scanner is a shell script")
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	// A scanner whose health is a state file: it dies the way ripgrep
	// dies when the directory it walks breaks under it, until the file
	// says it healed. It counts its invocations, so the test can tell
	// a scan retried from a scan that never stopped.
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
	t.Setenv("SEE_SCANNER_STATE", state) // empty: broken
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

	// First scan: fails, reports, and leaves nothing behind to retry from.
	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("a broken scanner reported no error")
	}
	if h.a.quick.files != nil {
		t.Fatalf("a failed scan left a file list of %d entries", len(h.a.quick.files))
	}

	// While the error stands, frames of the open panel must not scan
	// again: every scan does the same work for the same failure, and
	// the loop never ends.
	before := scans()
	if before == 0 {
		t.Fatal("the scanner never ran")
	}
	for i := 0; i < 15; i++ {
		h.tt.Frame()
		time.Sleep(20 * time.Millisecond)
	}
	if after := scans(); after != before {
		t.Fatalf("a failed scan re-scanned itself: %d scans became %d", before, after)
	}
	if h.a.quick.err == nil {
		t.Fatal("the scan error did not survive the frames")
	}

	// Opening the panel again is the retry: the same broken scanner
	// runs once more and reports the same error.
	h.a.quick.open = false
	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if scans() == before {
		t.Fatal("reopening the panel did not retry the scan")
	}
	if h.a.quick.err == nil {
		t.Fatal("the retry reported no error for the same broken scanner")
	}

	// A scanner that heals yields its files through the same retry: the
	// error was a moment, not a verdict.
	if err := os.WriteFile(state, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	h.a.quick.open = false
	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err != nil {
		t.Fatalf("the healed scanner still errored: %v", h.a.quick.err)
	}
	if len(h.a.quick.files) != 1 || h.a.quick.files[0] != "healed.go" {
		t.Fatalf("files = %v after the retry", h.a.quick.files)
	}
}

// The second deliberate retry point: a workspace invalidation. Events
// mean the file list no longer matches the disk, so the sticky error
// clears with the list — and the open panel must re-scan by itself,
// because the user fixing permissions in another terminal is precisely
// the moment a retry has a chance. A panel that instead waited for a
// retype or reopen would sit on a dead error while the filesystem
// healed underneath it, and the invalidation would be a retry point in
// name only (the verifier's D3).
func TestQuickOpenInvalidationRetriesScan(t *testing.T) {
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
	t.Setenv("SEE_SCANNER_STATE", state) // empty: broken
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

	// Scan fails; the error is sticky across frames, as before.
	h.a.openQuickly()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("a broken scanner reported no error")
	}
	before := scans()
	if before == 0 {
		t.Fatal("the scanner never ran")
	}

	// An empty event batch is not an invalidation: nothing changes,
	// nothing retries.
	h.a.applyEvents(nil)
	for i := 0; i < 5; i++ {
		h.tt.Frame()
	}
	if h.a.quick.err == nil {
		t.Fatal("an empty event batch cleared the sticky error")
	}
	if scans() != before {
		t.Fatal("an empty event batch retried the scan")
	}

	// A real event invalidates: the error clears, and the open panel
	// re-scans without any further user action. One invalidation means
	// one retry, not a resumption of the hot loop — the retried scan
	// fails the same way and goes sticky again.
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	h.tt.Frame()
	h.settle(func() bool { return scans() > before })
	if scans() != before+1 {
		t.Fatalf("one invalidation caused %d scans, want exactly one retry", scans()-before)
	}
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.err == nil {
		t.Fatal("the retried scan's error did not go sticky again")
	}

	// The user heals the scanner in another terminal; the next filesystem
	// event is the retry that succeeds — files arrive with the panel
	// never closed and the query never retyped.
	if err := os.WriteFile(state, []byte("ok"), 0o644); err != nil {
		t.Fatal(err)
	}
	before = scans()
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	h.tt.Frame()
	h.settle(func() bool { return scans() > before && !h.a.quick.listing })
	if h.a.quick.err != nil {
		t.Fatalf("the healed scanner still errored: %v", h.a.quick.err)
	}
	if len(h.a.quick.files) != 1 || h.a.quick.files[0] != "healed.go" {
		t.Fatalf("files = %v after the invalidation retry", h.a.quick.files)
	}
}

// Cancellation of a scan — closing the workspace while it lists — never
// leaves the panel believing in a partial file list either.
func TestQuickOpenScanCancellationLeavesNoPartialList(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the slow scanner is a shell script")
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	scanner := filepath.Join(t.TempDir(), "slow-rg")
	err := os.WriteFile(scanner, []byte("#!/bin/sh\nwhile true; do echo f.go; sleep 0.05; done\n"), 0o755)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", scanner)
	h := newHarness(t, root)
	h.a.trust()
	h.a.openQuickly()
	h.settle(func() bool { return h.a.quick.listing })
	// Closing the workspace is what cancels its scan context.
	h.a.wsCancel()
	h.settle(func() bool { return !h.a.quick.listing })
	if h.a.quick.files != nil {
		t.Fatalf("a canceled scan left %d files behind", len(h.a.quick.files))
	}
	// Let the death of the scanner process settle, so a leak shows up.
	time.Sleep(100 * time.Millisecond)
}

// An invalidation that lands while a scan is in flight must not let the
// scan commit its stale result: the scan started before the event, so
// its list is behind the disk by at least that event, and committing it
// would serve the stale list indefinitely — before the fix, nothing
// re-entered the scan because the invalidation had already cleared the
// list. The scan discards instead, and the panel re-scans what the disk
// says now. Mutation evidence: removing the completion guard in
// ensureFiles fails this test with scan1.go committed.
func TestQuickOpenInvalidationDuringScanRescans(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the scanner is a shell script")
	}
	root := writeTree(t, map[string]string{"a.go": "package a\n"})
	dir := t.TempDir()
	count := filepath.Join(dir, "count")
	gate := filepath.Join(dir, "gate")
	scanner := filepath.Join(dir, "slow-rg")
	script := "#!/bin/sh\n" +
		"echo start >> \"$SEE_SCANNER_COUNT\"\n" +
		"while [ ! -f \"$SEE_SCANNER_GATE\" ]; do sleep 0.02; done\n" +
		"n=$(grep -c start \"$SEE_SCANNER_COUNT\")\n" +
		"echo \"scan${n}.go\"\n"
	if err := os.WriteFile(scanner, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SEE_RG", scanner)
	t.Setenv("SEE_SCANNER_COUNT", count)
	t.Setenv("SEE_SCANNER_GATE", gate)
	h := newHarness(t, root)
	h.a.trust()
	scans := func() int {
		data, err := os.ReadFile(count)
		if err != nil {
			return 0
		}
		return strings.Count(string(data), "start")
	}

	// The first scan is in flight, blocked on the gate.
	h.a.openQuickly()
	if !waitFor(10*time.Second, func() bool { return scans() == 1 }) {
		data, _ := os.ReadFile(count)
		rg, rgerr := findRipgrep(h.a.root)
		t.Fatalf("scan never started: qerr=%v listing=%v files=%v rg=%q rgerr=%v scanlog=%q",
			h.a.quick.err, h.a.quick.listing, h.a.quick.files, rg, rgerr, data)
	}
	if !h.a.quick.listing {
		t.Fatal("the first scan is not in flight")
	}

	// The invalidation lands mid-scan, then the scan completes.
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	if err := os.WriteFile(gate, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Frames drive the re-rank that re-enters the scan: the discarded
	// completion leaves files nil, and the open panel's next scan is what
	// a user watching the panel would see settle.
	deadline := time.Now().Add(15 * time.Second)
	for len(h.a.quick.files) != 1 {
		if time.Now().After(deadline) {
			t.Fatalf("no list committed: scans=%d listing=%v err=%v", scans(), h.a.quick.listing, h.a.quick.err)
		}
		for {
			select {
			case fn := <-h.q:
				fn()
			default:
				goto drained
			}
		}
	drained:
		h.tt.Frame()
		time.Sleep(30 * time.Millisecond)
	}

	// The committed list must come from a scan that started after the
	// invalidation — scan2.go — never the in-flight scan's scan1.go.
	if scans() != 2 {
		t.Fatalf("%d scans ran, want the invalidated one discarded and exactly one rescan", scans())
	}
	if h.a.quick.files[0] != "scan2.go" {
		t.Fatalf("the committed list is %v — the stale in-flight scan won", h.a.quick.files)
	}
}

// waitFor polls cond until it holds or the timeout expires, returning
// whether it ever held. settle cannot be used here: nothing is posted
// while the scan has not started.
func waitFor(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}
