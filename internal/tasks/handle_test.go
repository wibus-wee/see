package tasks

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"see/internal/operr"
	"strings"
	"testing"
	"time"
)

func TestPolicyProcess(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "ok")
	exe, _ := os.Executable()
	root := t.TempDir()
	base := (Handle{}).WithCommand(func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, exe, args...)
	})
	n := 0
	p := Policy{Attempts: 3, BeforeStart: func(context.Context, int) error {
		n++
		if n == 1 {
			return errors.New("private-token")
		}
		return nil
	}}
	derived, err := base.WithPolicy(p)
	if err != nil {
		t.Fatal(err)
	}
	out := new(Output)
	if err := derived.Run(context.Background(), "ignored", root, Test, out); err != nil {
		t.Fatal(err)
	}
	text, _ := out.Snapshot()
	if n != 2 || !strings.Contains(text, "args=test ./...") {
		t.Fatal(n, text)
	}
	t.Setenv("SEE_TASK_HELPER", "fail")
	e := operr.Classify(derived.Run(context.Background(), "ignored", root, Test, out))
	if e == nil || e.Attempt != 1 || e.Finality != operr.Uncertain || n != 3 {
		t.Fatal(e, n)
	}
	p.BeforeStart = func(context.Context, int) error { return errors.New("private-token") }
	h, _ := base.WithPolicy(p)
	e = operr.Classify(h.Run(context.Background(), "ignored", root, Build, out))
	if e == nil || e.Status != operr.Persistent || e.Attempt != 3 || strings.Contains(e.Error(), "private-token") {
		t.Fatal(e)
	}
}
func TestPolicyAdmissionAndCancellation(t *testing.T) {
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h, _ := (Handle{}).WithPolicy(Policy{Concurrent: 1, Attempts: 5, Backoff: time.Hour, BeforeStart: func(context.Context, int) error { close(entered); return errors.New("offline") }})
	done := make(chan error, 1)
	go func() { done <- h.Run(ctx, "go", ".", Test, new(Output)) }()
	<-entered
	e := operr.Classify(h.Run(context.Background(), "go", ".", Test, new(Output)))
	if e == nil || e.Kind != operr.RateLimited {
		t.Fatal(e)
	}
	cancel()
	select {
	case err := <-done:
		e = operr.Classify(err)
		if e == nil || e.Kind != operr.Cancelled || e.Finality != operr.NoEffect || e.Attempt != 1 {
			t.Fatal(e)
		}
	case <-time.After(time.Second):
		t.Fatal("retry wait ignored cancellation")
	}
}

func TestAttemptAndTotalDeadline(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "ok")
	exe, _ := os.Executable()
	root := t.TempDir()
	base := (Handle{}).WithClock(func() time.Time { return time.Unix(1, 0) })
	h, err := base.WithCommand(func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, exe, args...)
	}).WithPolicy(Policy{Timeout: 10 * time.Millisecond, Attempts: 2, BeforeStart: func(ctx context.Context, n int) error {
		if n == 1 {
			<-ctx.Done()
			return ctx.Err()
		}
		return errors.New("private")
	}})
	if err != nil {
		t.Fatal(err)
	}
	e := operr.Classify(h.Run(context.Background(), "ignored", root, Build, new(Output)))
	if e.Status != operr.Persistent || e.Attempt != 2 || e.Duration != 0 {
		t.Fatal(e)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer cancel()
	e = operr.Classify(h.Run(ctx, "ignored", root, Build, new(Output)))
	if e.Kind != operr.Timeout || e.Attempt != 1 || e.Status == operr.Persistent {
		t.Fatal(e)
	}
}

func TestMissingProcessIsSafeToDisplay(t *testing.T) {
	secret := "token-must-not-appear"
	err := (Handle{}).Run(context.Background(), filepath.Join(t.TempDir(), secret), t.TempDir(), Build, new(Output))
	e := operr.Classify(fmt.Errorf("task boundary: %w", err))
	// What reports the miss is the platform's: POSIX fails the fork with
	// ENOENT, while Windows fails the PATHEXT resolution ahead of any
	// start and exec.ErrNotFound carries no Is relation to os.ErrNotExist
	// there. Both classify as the one not-found kind.
	missing := errors.Is(err, os.ErrNotExist) || errors.Is(err, exec.ErrNotFound)
	if e == nil || e.Kind != operr.NotFound || e.Status != operr.Permanent || e.Operation != "go-build" || !missing || strings.Contains(err.Error(), secret) || len(e.Trace) != 0 {
		t.Fatal(e)
	}
}

func TestDeriveWhileProcessDispatchInFlight(t *testing.T) {
	t.Setenv("SEE_TASK_HELPER", "ok")
	exe, _ := os.Executable()
	entered, release := make(chan struct{}), make(chan struct{})
	old, _ := (Handle{}).WithPolicy(Policy{BeforeStart: func(context.Context, int) error { close(entered); <-release; return nil }})
	root := t.TempDir()
	out := new(Output)
	done := make(chan error, 1)
	go func() { done <- old.Run(context.Background(), exe, root, Build, out) }()
	<-entered
	replacement, _ := old.WithPolicy(Policy{})
	replacement = replacement.WithCommand(func(ctx context.Context, _ string, args ...string) *exec.Cmd {
		return exec.CommandContext(ctx, root+"/absent", args...)
	})
	err := replacement.Run(context.Background(), exe, root, Build, new(Output))
	close(release)
	original := <-done
	if e := operr.Classify(err); e == nil || e.Kind != operr.NotFound || original != nil {
		t.Fatal(e, original)
	}
	text, _ := out.Snapshot()
	if !strings.Contains(text, "args=build ./...") {
		t.Fatal(text)
	}
}
func TestRejectInvalidPolicy(t *testing.T) {
	for _, p := range []Policy{{Attempts: -1}, {Timeout: -1}, {Backoff: -1}, {Concurrent: -1}} {
		_, err := (Handle{}).WithPolicy(p)
		if e := operr.Classify(err); e == nil || e.Kind != operr.InvalidInput {
			t.Fatal(p, e)
		}
	}
}
