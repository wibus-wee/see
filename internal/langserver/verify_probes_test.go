// Regression pins adopted from the independent verifier's probes
// (verify-lane-a, commit cb4ac34): notification-queue overflow under
// concurrent pressure and the user-visible behavior once the restart
// budget is spent.

package langserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/operr"
)

// A blocked writer, several large documents and concurrent editors: the
// byte-bounded queue must fail the session loudly exactly once per
// session, every Sync caller must reach a terminal state, and Stop must
// not deadlock against the items still queued behind the blocked write.
func TestVerifyQueuePressureNoDeadlock(t *testing.T) {
	small := outboxMaxBytes
	outboxMaxBytes = 4 << 20
	defer func() { outboxMaxBytes = small }()

	blocked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c := &fakeClient{changeStarted: blocked, releaseChange: release}
	s := New(Options{Name: "fake", Start: serving(c, new(int))})
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("seed")
	const seedURI = "file:///seed.go"
	if err := s.Sync(ctx, client, seedURI, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	// Block the writer on the first change.
	buf.Set("blocking")
	s.Changed(seedURI, buf.Snapshot())
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}

	// Concurrent editors open large documents fire-and-forget, as the
	// app's Changed path does, until the byte cap trips. A Sync probe
	// runs beside them: it must terminate when the session fails.
	large := editor.NewBuffer(strings.Repeat("x", 256<<10))
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; ; i++ {
				select {
				case <-stop:
					return
				default:
				}
				uri := fmt.Sprintf("file:///w%d/doc%d.go", worker, i)
				if !s.Open(client, uri, large.Snapshot()) {
					return // the push was refused: the session is failing
				}
			}
		}(worker)
	}
	// A Sync issued against the session while the writer is blocked must
	// not hang past the session's failure.
	syncDone := make(chan error, 1)
	go func() {
		ictx, icancel := context.WithTimeout(ctx, 8*time.Second)
		defer icancel()
		syncDone <- s.Sync(ictx, client, "file:///probe.go", buf.Snapshot())
	}()

	deadline := time.After(10 * time.Second)
	for {
		st, _ := s.Status()
		if st == Failed {
			break
		}
		select {
		case <-deadline:
			t.Fatal("queue overflow never failed the session under pressure")
		case <-time.After(5 * time.Millisecond):
		}
	}
	close(stop)
	select {
	case err := <-syncDone:
		if err == nil {
			t.Fatal("Sync reported success against a session that never received the document")
		}
		t.Logf("blocked Sync terminated with: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("Sync hung after the session failed")
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("producers hung after the session failed")
	}
	// Stop against a session with a blocked writer must complete.
	stopped := make(chan struct{})
	go func() { s.StopWait(3 * time.Second); close(stopped) }()
	releaseOnce.Do(func() { close(release) })
	select {
	case <-stopped:
	case <-time.After(8 * time.Second):
		t.Fatal("Stop deadlocked behind queued notifications")
	}
}

// After the restart budget is spent, a user-triggered request must not
// silently hang or die: Get returns the persisted failure at once, and
// it is the classified persistent terminal state, not a raw string.
func TestVerifyBudgetExhaustionSurfacing(t *testing.T) {
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{5 * time.Millisecond, 5 * time.Millisecond}

	var mu sync.Mutex
	var exits []func(error)
	var starts int
	s := New(Options{Name: "fake", Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
		mu.Lock()
		defer mu.Unlock()
		exits = append(exits, onExit)
		starts++
		return &fakeClient{}, nil
	}})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.Get(ctx); err != nil {
		t.Fatal(err)
	}
	crash := func(i int) {
		mu.Lock()
		exit := exits[i]
		mu.Unlock()
		exit(errors.New("segfault"))
	}
	// Three sessions, three crashes: budget is two restarts.
	crash(0)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return starts >= 2 })
	crash(1)
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return starts >= 3 })
	crash(2)
	waitFor(t, func() bool { st, _ := s.Status(); return st == Failed })
	time.Sleep(30 * time.Millisecond) // past both restart delays

	// The user triggers an operation now: it must return immediately.
	started := time.Now()
	_, err := s.Get(ctx)
	elapsed := time.Since(started)
	if err == nil {
		t.Fatal("Get succeeded after the budget was spent")
	}
	if elapsed > time.Second {
		t.Fatalf("Get blocked %v after budget exhaustion", elapsed)
	}
	var failure *operr.Error
	if !errors.As(err, &failure) {
		t.Fatalf("user-facing failure is not classified: %v (%T)", err, err)
	}
	if failure.Status != operr.Persistent {
		t.Fatalf("user-facing failure is not persistent: %+v", failure)
	}
	// What the user actually reads: the Error() text. If this is only a
	// kind/status pair with no operation or cause, the UI's fallback path
	// shows exactly that.
	t.Logf("user-facing error text after budget exhaustion: %q", err.Error())
	if !strings.Contains(err.Error(), "persistent") {
		t.Fatalf("user-facing text hides the terminal state: %q", err.Error())
	}
}

// close-after-overflow: a document closed while its change is queued
// behind the blocked writer must not resurrect in the restart replay.
func TestVerifyCloseBeforeOverflowDoesNotReplay(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	defer releaseOnce.Do(func() { close(release) })
	c := &fakeClient{changeStarted: blocked, releaseChange: release}
	s := New(Options{Name: "fake", Start: serving(c, new(int))})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("seed")
	const uri = "file:///closing.go"
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	buf.Set("blocking")
	s.Changed(uri, buf.Snapshot())
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	// Close the document while its change is stuck. Close drops it from
	// docs, so the restart replay must not reopen it.
	s.Close(uri)
	for i := 0; i < 300 && s.Open(client, fmt.Sprintf("file:///filler%d.go", i), buf.Snapshot()); i++ {
	}
	st, _ := s.Status()
	if st != Failed {
		t.Fatalf("overflow did not fail the session: %v", st)
	}
	// Inspect the surviving snapshot set: a document closed before the
	// overflow must not be replayed into the next session.
	releaseOnce.Do(func() { close(release) })
	s.mu.Lock()
	_, resurrected := s.docs[uri]
	s.mu.Unlock()
	if resurrected {
		t.Fatal("a document closed before the overflow would be replayed after restart")
	}
}
