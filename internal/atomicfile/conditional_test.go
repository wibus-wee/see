package atomicfile

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCommitIfPublishesWhenConditionPasses(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitIf(func() error { return nil }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "new" {
		t.Fatalf("conditional commit did not publish: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("conditional commit left staging behind")
	}
}

func TestCommitIfRefusalKeepsOldBytesAndNoStaging(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	conflict := errors.New("the file changed on disk")
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	err = s.CommitIf(func() error { return conflict })
	if !errors.Is(err, conflict) {
		t.Fatalf("err = %v, want the condition's own error", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("a refused commit changed the target: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("a refused commit left staging behind")
	}
	if err := s.Commit(); err == nil {
		t.Fatal("the refused session still accepts commits")
	}
}

// TestCommitIfEvaluatesAtThePublishBoundary is the point of the method:
// the condition observes the target as it is after the whole staging
// write, not as it was when the session began. An external write landing
// mid-stream is caught; only the rename boundary itself remains open.
func TestCommitIfEvaluatesAtThePublishBoundary(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	const original = "the version the caller read"
	if err := os.WriteFile(path, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	writeStarted := make(chan struct{})
	externalDone := make(chan struct{})
	go func() {
		<-writeStarted
		if err := os.WriteFile(path, []byte("external rewrite"), 0o644); err != nil {
			t.Error(err)
		}
		close(externalDone)
	}()
	close(writeStarted)
	big := strings.Repeat("z", 200_000)
	for i := 0; i < len(big); i += 1000 {
		if _, err := s.Write([]byte(big[i : i+1000])); err != nil {
			t.Fatal(err)
		}
		if i == 100_000 {
			<-externalDone // mid-stream, the external write has landed
		}
	}
	conflict := errors.New("the file changed on disk")
	err = s.CommitIf(func() error {
		current, rerr := os.ReadFile(path)
		if rerr != nil {
			return rerr
		}
		if string(current) != original {
			return conflict
		}
		return nil
	})
	if !errors.Is(err, conflict) {
		t.Fatalf("err = %v, want the conflict: the condition did not see the mid-stream external write", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "external rewrite" {
		t.Fatalf("the refused commit overwrote the external write: %q err=%v", data[:min(len(data), 40)], err)
	}
}

func TestCommitIfAbsentCreatesWhenAbsent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "created.txt")
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("first and only")); err != nil {
		t.Fatal(err)
	}
	if err := s.CommitIfAbsent(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "first and only" {
		t.Fatalf("create-if-absent did not publish: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("create-if-absent left staging behind")
	}
}

func TestCommitIfAbsentRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("already here"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("would overwrite")); err != nil {
		t.Fatal(err)
	}
	err = s.CommitIfAbsent()
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("err = %v, want fs.ErrExist", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "already here" {
		t.Fatalf("the existing target changed: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("the refused publish left staging behind")
	}
}

// TestCommitIfAbsentIsAtomicUnderConcurrency is the difference from
// check-then-write: two racing create-if-absent commits over an absent
// target cannot both succeed, because the kernel performs the existence
// test inside the link. No window exists to exploit.
func TestCommitIfAbsentIsAtomicUnderConcurrency(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "race.txt")
	const racers = 8
	var wg sync.WaitGroup
	results := make(chan error, racers)
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := Begin(path)
			if err != nil {
				results <- err
				return
			}
			if _, err := s.Write([]byte("one of the racers")); err != nil {
				results <- err
				return
			}
			results <- s.CommitIfAbsent()
		}()
	}
	wg.Wait()
	close(results)
	wins, losses := 0, 0
	for err := range results {
		if err == nil {
			wins++
		} else if errors.Is(err, fs.ErrExist) {
			losses++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || losses != racers-1 {
		t.Fatalf("wins=%d losses=%d: create-if-absent was not atomic — exactly one racer must win", wins, losses)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "one of the racers" {
		t.Fatalf("the winner's bytes: %q err=%v", data, err)
	}
	if stagingLeftovers(t, dir) {
		t.Fatal("racing commits left staging behind")
	}
}

// TestSessionSharedAcrossGoroutines exercises the documented contract:
// Write, Commit and Abort may race across goroutines on one session, and
// the outcome is one consistent terminal state. Run with -race; before
// sessions serialized internally, the race detector reported the state
// bits racing here.
func TestSessionSharedAcrossGoroutines(t *testing.T) {
	dir := t.TempDir()
	for range 20 {
		path := filepath.Join(dir, "file.txt")
		s, err := Begin(path)
		if err != nil {
			t.Fatal(err)
		}
		var wg sync.WaitGroup
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				_, _ = s.Write([]byte("concurrent"))
			}()
		}
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.Commit()
		}()
		go func() {
			defer wg.Done()
			_ = s.Abort()
		}()
		wg.Wait()
		// Whatever won, the target is either absent, the old bytes, or a
		// whole number of complete pieces — never a half-write of one.
		data, err := os.ReadFile(path)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		if len(data)%len("concurrent") != 0 {
			t.Fatalf("torn content %q: a racing write published partially", data)
		}
		_ = os.Remove(path)
	}
}

// TestPanicInWriteCallbackCleansUp is the panic contract: the staging
// file and its handle do not outlive a panicking write callback, and the
// target is never touched. The panic itself propagates to the caller.
func TestPanicInWriteCallbackCleansUp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	func() {
		defer func() {
			if recover() == nil {
				t.Error("the panic did not propagate")
			}
		}()
		_ = WriteFile(path, func(w io.Writer) error {
			_, _ = io.WriteString(w, "before the panic")
			panic("caller bug")
		})
	}()
	if stagingLeftovers(t, dir) {
		t.Fatal("a panicking write left staging behind")
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "old" {
		t.Fatalf("a panicking write touched the target: %q err=%v", data, err)
	}
}

// A condition that calls back into its own session observes the finished
// session instead of deadlocking: finished is set before cond runs, and
// the lock no longer covers cond, because nothing a racing operation
// could still change needs it. A re-entrant cond is a caller bug — its
// Write fails — but the publish must survive it: the condition itself
// decides, and the session never wedges.
func TestCommitIfReentrantCondDoesNotDeadlock(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		done <- s.CommitIf(func() error {
			// The re-entrant call must fail fast on the finished
			// session, not wait on a lock its own commit holds.
			if _, werr := s.Write([]byte("x")); werr == nil {
				return errors.New("write on a finished session succeeded")
			}
			return nil
		})
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("commit with a re-entrant cond failed: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("commit with a re-entrant cond deadlocked")
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Fatalf("target %q, want the published bytes", data)
	}
}

// A racing Abort cannot have ended the session once commit won the
// finished bit, and it must not wait behind cond either: it observes the
// finished session and returns.
func TestCommitIfAbortRacingCondReturns(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "f.txt")
	if err := os.WriteFile(target, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Begin(target)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	release := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- s.CommitIf(func() error {
			<-release
			return nil
		})
	}()
	// Give the commit a moment to enter cond, then abort: it must not
	// block behind the condition.
	time.Sleep(50 * time.Millisecond)
	abortDone := make(chan error, 1)
	go func() { abortDone <- s.Abort() }()
	select {
	case <-abortDone:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("abort blocked behind the condition")
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("commit failed: %v", err)
	}
}
