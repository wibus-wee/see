package langserver

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/operr"
)

// Retry is the user's deliberate restart — typically after reinstalling
// or reconfiguring the server — so the crash budget starts over with it.
// A session that crashes after a manual Retry must earn automatic
// restarts again rather than fail durably on its first crash.
func TestRetryResetsTheRestartBudget(t *testing.T) {
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{5 * time.Millisecond, 5 * time.Millisecond}

	var mu sync.Mutex
	var exits []func(error)
	s := New(Options{Name: "fake", Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
		mu.Lock()
		defer mu.Unlock()
		exits = append(exits, onExit)
		return &fakeClient{}, nil
	}})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := s.Get(ctx); err != nil {
		t.Fatal(err)
	}
	starts := func() int { mu.Lock(); defer mu.Unlock(); return len(exits) }
	crash := func(i int) {
		mu.Lock()
		exit := exits[i]
		mu.Unlock()
		exit(errors.New("segfault"))
	}
	// Spend the whole budget: two crashes, two automatic restarts, a
	// third crash, durable failure.
	crash(0)
	waitFor(t, func() bool { return starts() >= 2 })
	crash(1)
	waitFor(t, func() bool { return starts() >= 3 })
	crash(2)
	waitFor(t, func() bool { st, _ := s.Status(); return st == Failed })
	time.Sleep(4 * restartDelays[0])
	if got := starts(); got != 3 {
		t.Fatalf("control failed: restarts continued past the budget: %d starts", got)
	}

	// The user retries — after reinstalling the binary, say — and the new
	// session crashes once. The crash must earn automatic restarts again:
	// the budget the pre-fix crashes consumed cannot be held against the
	// session the user just asked for.
	s.Retry()
	if _, err := s.Get(ctx); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { return starts() >= 4 })
	crash(3)
	waitFor(t, func() bool { return starts() >= 5 })
	// Whatever the state machine shows mid-restart, the failure must not
	// be the persistent terminal kind: that is what an unreset budget
	// would have produced.
	if _, err := s.Status(); err != nil {
		var failure *operr.Error
		if errors.As(err, &failure) && failure.Status == operr.Persistent {
			t.Fatalf("budget not reset by Retry: durable failure after one fresh crash: %+v", failure)
		}
	}
	// A second crash within the new budget still restarts rather than
	// terminating durably.
	crash(4)
	waitFor(t, func() bool { return starts() >= 6 })
	crash(5)
	time.Sleep(4 * restartDelays[0])
	if got := starts(); got != 6 {
		t.Fatalf("the new budget was not spent exactly once: %d starts", got)
	}
}

// A delivery failure marks the server Degraded, and the next successful
// sync clears it: changes carry the document whole, so once one lands the
// server's copy is no longer suspect. The state must be visible in
// between rather than silently sticky.
func TestDegradedRecoversOnTheNextSuccessfulSync(t *testing.T) {
	c := &fakeClient{changeErr: errors.New("pipe hiccup")}
	s := New(Options{Name: "fake", Start: serving(c, new(int))})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("seed")
	const uri = "file:///degraded.go"
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	buf.Set("first edit")
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err == nil {
		t.Fatal("injected change failure not reported")
	}
	waitFor(t, func() bool { st, _ := s.Status(); return st == Degraded })
	// The failure is sticky only until delivery works again, not until a
	// restart: Get serves the same client, and a successful change clears
	// the state.
	c.mu.Lock()
	c.changeErr = nil
	c.mu.Unlock()
	buf.Set("second edit")
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool { st, _ := s.Status(); return st == Ready })
}
