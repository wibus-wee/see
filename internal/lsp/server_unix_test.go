//go:build unix

package lsp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"see/internal/operr"
)

func TestStartStalledPipe(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 200*time.Millisecond)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := Start(ctx, "/bin/sh", []string{"-c", "sleep 20 & wait"}, Options{InitializationOptions: strings.Repeat("a", 1<<20)})
		result <- err
	}()
	select {
	case err := <-result:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("initialize blocked on child's stdin")
	}
}

func TestShutdownStalledProcessGroup(t *testing.T) {
	response := `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	// The child keeps all pipes open even after its shell dies. It never reads
	// the shutdown request, so only timeout escalation can complete shutdown.
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; sleep 20 & wait`, len(response), response)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill()
	done := make(chan struct{})
	go func() { c.Shutdown(100 * time.Millisecond); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
		t.Fatal("shutdown blocked on inherited pipes")
	}
}

const readInitialize = `IFS=' ' read -r header length; read -r blank; length=$(printf '%s' "$length" | tr -d '\r'); dd bs=1 count="$length" of=/dev/null 2>/dev/null; `

func TestProcessExitWithInheritedStdout(t *testing.T) {
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	const warning = `{"jsonrpc":"2.0","method":"window/logMessage","params":{"type":1,"message":"SECRET"}}`
	script := readInitialize + fmt.Sprintf(`echo SECRET >&2; printf 'Content-Length: %d\r\n\r\n%%sContent-Length: %d\r\n\r\n%%s' '%s' '%s'; read initialized; sleep 20 & exit 7`, len(warning), len(response), warning, response)
	logs := make(chan string, 16)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{Logf: func(f string, a ...any) { logs <- fmt.Sprintf(f, a...) }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill()
	select {
	case <-c.Exited():
	case <-ctx.Done():
		t.Fatal("leader exit hidden by descendant")
	}
	for len(logs) > 0 {
		if strings.Contains(<-logs, "SECRET") {
			t.Fatal("remote text leaked into logs")
		}
	}
}

func TestFinalReplyBeforeExit(t *testing.T) {
	frame := func(body string) string {
		return fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(body), body)
	}
	// Slow the protocol reader after the reply is written, so waiting on the
	// child can finish while the reply is still buffered in the stdout pipe.
	script := readInitialize + frame(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`) + readInitialize + readInitialize + frame(`{"jsonrpc":"2.0","method":"window/logMessage","params":{"type":1}}`) + frame(`{"jsonrpc":"2.0","id":2,"result":` + strings.Repeat(" ", 6000) + `42}`) + `exit 0`
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{Logf: func(f string, a ...any) {
		if strings.Contains(f, "server message") {
			time.Sleep(20 * time.Millisecond)
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill()
	var answer int
	if err = c.conn.Call(ctx, "final", nil, &answer); err != nil || answer != 42 {
		t.Fatalf("last reply = %d, %v", answer, err)
	}
	select {
	case <-c.Exited():
	case <-ctx.Done():
		t.Fatal("process not reaped")
	}
}

func TestStartWithoutExecutePermission(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores permission bits")
	}
	dir := t.TempDir()
	exe := filepath.Join(dir, "server")
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Start(t.Context(), exe, nil, Options{})
	var e *operr.Error
	if !errors.As(err, &e) || e.Kind != operr.PermissionDenied || e.Status != operr.Permanent || e.Finality != operr.NoEffect {
		t.Fatalf("start error: %#v", err)
	}
	if strings.Contains(err.Error(), exe) {
		t.Fatalf("error leaked the executable path: %v", err)
	}
}

// A secret longer than the 4 KiB stderr digest must not leak either:
// redaction runs as bytes arrive, so the window's size does not bound
// what can be redacted. The child prints the secret one byte at a time,
// which also covers secrets split across pipe reads.
func TestStartCrashRedactsOversizeStderrSecret(t *testing.T) {
	secret := strings.Repeat("k", 5000) // longer than the 4 KiB digest
	t.Setenv("SEE_TEST_KEY", secret)
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	// The second readInitialize consumes the client's initialized
	// notification: exiting only then is what makes the crash a crash of
	// a live session rather than a race against the handshake.
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) +
		readInitialize + `echo "fatal: key rejected" >&2; i=0; while [ $i -lt 5000 ]; do printf k >&2; i=$((i+1)); done; printf '\n' >&2; echo "check the config" >&2; exit 1`
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	exitErr := make(chan error, 1)
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{OnExit: func(err error) { exitErr <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill()
	select {
	case err := <-exitErr:
		text := err.Error()
		for i := 0; i+64 <= len(secret); i += 64 {
			if strings.Contains(text, secret[i:i+64]) {
				t.Fatalf("oversize secret fragment leaked into exit error: %.200q", text)
			}
		}
		if !strings.Contains(text, "check the config") {
			t.Fatalf("stderr digest missing from exit error: %v", err)
		}
		if !strings.Contains(text, "fatal: key rejected") {
			t.Fatalf("harmless stderr line lost: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("server exit not reported")
	}
}

func TestStartCrashReportsRedactedStderr(t *testing.T) {
	t.Setenv("SEE_TEST_SECRET", "testsecretvalue123")
	const response = `{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`
	script := readInitialize + fmt.Sprintf(`printf 'Content-Length: %d\r\n\r\n%%s' '%s'; `, len(response), response) +
		readInitialize + `echo "fatal: token testsecretvalue123 rejected" >&2; echo "check the config" >&2; exit 1`
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	exitErr := make(chan error, 1)
	c, err := Start(ctx, "/bin/sh", []string{"-c", script}, Options{OnExit: func(err error) { exitErr <- err }})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Kill()
	select {
	case err := <-exitErr:
		text := err.Error()
		if strings.Contains(text, "testsecretvalue123") {
			t.Fatalf("secret leaked into exit error: %v", err)
		}
		if !strings.Contains(text, "check the config") {
			t.Fatalf("stderr digest missing from exit error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("server exit not reported")
	}
}
