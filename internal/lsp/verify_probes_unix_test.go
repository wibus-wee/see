//go:build unix

// Regression pins adopted from the independent verifier's probes
// (verify-lane-a, commit cb4ac34): process-group escape and
// shutdown-protocol timing, exercised through the production Start path
// with real processes.

package lsp

import (
	"context"
	"fmt"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"
)

// probeAlive is the kill(pid, 0) liveness check: nil error means the
// process exists, EPERM also means it exists.
func probeAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	return err == nil || err == syscall.EPERM
}

// TestVerifyEscapeHelper is the setsid escapee fixture: re-executed as a
// grandchild, it leaves the server's process group, writes its pid to
// $SEE_VERIFY_PIDFILE and sleeps holding every inherited fd — including
// the protocol pipes, which is what makes it dangerous.
func TestVerifyEscapeHelper(t *testing.T) {
	if os.Getenv("SEE_VERIFY_ESCAPE") != "1" {
		return
	}
	_, _ = syscall.Setsid()
	pid := os.Getpid()
	if f := os.Getenv("SEE_VERIFY_PIDFILE"); f != "" {
		_ = os.WriteFile(f, []byte(fmt.Sprint(pid)), 0600)
	}
	time.Sleep(60 * time.Second)
	os.Exit(0)
}

// spawnEscapee starts the escapee as a direct descendant of the shell
// that runs the server script: it inherits the protocol pipes and then
// leaves the process group.
func escapeeSpawn(exe, pidfile string) string {
	// No redirections: the escapee must inherit and hold the protocol
	// pipes, or the test would not exercise the drain path at all.
	return fmt.Sprintf(`SEE_VERIFY_ESCAPE=1 SEE_VERIFY_PIDFILE=%s %q -test.run=^TestVerifyEscapeHelper$ & disown; `,
		pidfile, exe)
}

func waitForPidfile(t *testing.T, pidfile string) int {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(pidfile); err == nil {
			var pid int
			if _, err := fmt.Sscanf(strings.TrimSpace(string(data)), "%d", &pid); err == nil && pid > 0 {
				return pid
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("escapee never wrote its pid")
	return 0
}

// A descendant that calls setsid leaves the process group that SIGKILL
// targets. The matrix admits escaped descendants are outside the kill
// guarantee; this test pins what the admission costs: the escapee
// survives shutdown holding the protocol pipes, and the client still
// terminates because the owned read end is closed after a bounded drain.
func TestVerifyEscapedGrandchildSurvivesShutdown(t *testing.T) {
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	dir := t.TempDir()
	pidfile := dir + "/grandchild.pid"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The shell answers initialize, spawns the escapee, then serves the
	// protocol until killed: the escapee holds copies of both pipes.
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) +
		escapeeSpawn(exe, pidfile) + `exec sleep 60`
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	pid := waitForPidfile(t, pidfile)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }() // never leak the fixture

	done := make(chan struct{})
	go func() { c.Shutdown(150 * time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("shutdown blocked on pipes the escapee inherited")
	}
	select {
	case <-c.Exited():
	case <-time.After(2 * time.Second):
		t.Fatal("exit detection blocked by escapee holding stdout")
	}
	alive := probeAlive(pid)
	t.Logf("escaped grandchild alive after group kill: %v", alive)
	if !alive {
		t.Fatal("unexpected: escapee was killed — the process-group guarantee is stronger than documented")
	}
}

// On server crash, a pending request must terminate even when an escaped
// descendant holds stdout open (no EOF ever arrives). The 500 ms drain in
// the watcher is the only thing standing between the caller and a hang.
func TestVerifyPendingRequestTerminatesWithEscapeeHoldingPipes(t *testing.T) {
	// The peer advertises hover so the request is genuinely dispatched,
	// answers initialize, spawns the escapee, then dies mid-request.
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{"hoverProvider":true}}}`
	dir := t.TempDir()
	pidfile := dir + "/grandchild.pid"
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// readInitialize consumes the initialize frame; the second
	// readInitialize consumes the hover frame; then the shell dies
	// without answering, leaving the escapee holding both pipes.
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) +
		escapeeSpawn(exe, pidfile) + readInitialize + `exit 1`
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	pid := waitForPidfile(t, pidfile)
	defer func() { _ = syscall.Kill(pid, syscall.SIGKILL) }()

	// The hover request must be in flight when the server dies: the
	// shell's second readInitialize consumes its frame before exiting.
	// Poll the pidfile write ordering is not enough — the frame read is
	// what matters — so issue the request and rely on the shell exiting
	// only after consuming it.
	done := make(chan error, 1)
	go func() { _, _, err := c.Hover(ctx, "file:///a", Position{}); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("pending request succeeded against a dead server")
		}
		if strings.Contains(err.Error(), "unsupported") {
			t.Fatal("request was never dispatched; fixture is broken")
		}
		t.Logf("pending request terminated with: %v", err)
	case <-time.After(6 * time.Second):
		t.Fatal("pending request hung: escapee-held pipe hid the leader's death")
	}
	if !probeAlive(pid) {
		t.Fatal("escapee died unexpectedly; fixture invalid")
	}
}

// Shutdown escalation timing: the server answers shutdown but never
// exits. The kill must land near the requested timeout, not at the pipe
// drain's or the protocol's convenience.
func TestVerifyShutdownEscalationTiming(t *testing.T) {
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	// Answer initialize; for shutdown reply id 2; then ignore exit and
	// sleep forever.
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) +
		readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(`{"jsonrpc":"2.0","id":2,"result":null}`), `{"jsonrpc":"2.0","id":2,"result":null}`) +
		`exec sleep 60`
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	c.Shutdown(300 * time.Millisecond)
	elapsed := time.Since(started)
	t.Logf("shutdown of an exit-ignoring server took %v", elapsed)
	if elapsed > 3*time.Second {
		t.Fatalf("shutdown escalation too slow: %v", elapsed)
	}
	select {
	case <-c.Exited():
	default:
		t.Fatal("server not exited after shutdown returned")
	}
}

// The server never answers shutdown at all: the shutdown Call fails at
// the context timeout, exit is never sent, stdin closes. Escalation must
// still complete.
func TestVerifyShutdownUnresponsiveServer(t *testing.T) {
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) + `exec sleep 60`
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	c.Shutdown(300 * time.Millisecond)
	elapsed := time.Since(started)
	t.Logf("shutdown of a shutdown-ignoring server took %v", elapsed)
	if elapsed > 3*time.Second {
		t.Fatalf("shutdown escalation too slow: %v", elapsed)
	}
	select {
	case <-c.Exited():
	default:
		t.Fatal("server not exited after shutdown returned")
	}
}

// REQ-EXT-090 in the small: a server that emits an endless stderr flood
// must not stall process reaping beyond WaitDelay, and the digest stays
// bounded. (The implementer's tests cover redaction, not flood volume.)
func TestVerifyStderrFloodDoesNotStallReaping(t *testing.T) {
	script := `yes SECRETFLOODLINE 1>&2 & flood=$!; sleep 0.3; kill $flood 2>/dev/null; exit 1`
	ctx, cancel := context.WithTimeout(t.Context(), 8*time.Second)
	defer cancel()
	started := time.Now()
	_, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err == nil {
		t.Fatal("crashed server started successfully")
	}
	elapsed := time.Since(started)
	t.Logf("crash under stderr flood reaped in %v: %v", elapsed, err)
	if elapsed > 4*time.Second {
		t.Fatalf("reaping stalled by stderr flood: %v", elapsed)
	}
	var e = err.Error()
	if len(e) > 8192 {
		t.Fatalf("error carries an unbounded digest: %d bytes", len(e))
	}
	if strings.Count(e, "SECRETFLOODLINE") > 300 {
		t.Fatalf("digest is not tail-bounded: %d flood lines", strings.Count(e, "SECRETFLOODLINE"))
	}
}
