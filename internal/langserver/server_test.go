package langserver

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
)

type fakeClient struct {
	noDefinition  bool
	changeStarted chan struct{}
	releaseChange <-chan struct{}
	changeErr     error

	mu    sync.Mutex
	calls []string
}

func (c *fakeClient) record(s string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, s)
	return nil
}

func (c *fakeClient) got() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.calls...)
}

func (c *fakeClient) Encoding() string    { return lsp.UTF16 }
func (c *fakeClient) HasDefinition() bool { return !c.noDefinition }
func (c *fakeClient) DidOpen(uri, language string, version int, text string) error {
	return c.record("open " + language + " " + text)
}
func (c *fakeClient) DidChange(uri string, version int, text string) error {
	if c.changeStarted != nil {
		close(c.changeStarted)
		<-c.releaseChange
	}
	if err := c.record("change " + text); err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.changeErr
}
func (c *fakeClient) DidClose(uri string) error { return c.record("close") }
func (c *fakeClient) Definition(context.Context, string, lsp.Position) ([]lsp.Location, error) {
	return nil, nil
}
func (c *fakeClient) Hover(context.Context, string, lsp.Position) (string, *lsp.Range, error) {
	return "", nil, nil
}
func (c *fakeClient) Shutdown(time.Duration) {}

func serving(c Client, starts *int) Starter {
	return func(context.Context, string, []lsp.WorkspaceFolder, func(error)) (Client, error) {
		*starts++
		return c, nil
	}
}

func TestStartsOnceAndReportsStates(t *testing.T) {
	var mu sync.Mutex
	var states []State
	starts := 0
	s := New(Options{Name: "fake", Start: serving(&fakeClient{}, &starts), OnState: func(st State, _ error) {
		mu.Lock()
		states = append(states, st)
		mu.Unlock()
	}})
	defer s.StopWait(time.Second)
	for range 3 {
		if _, err := s.Get(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if starts != 1 || len(states) != 2 || states[0] != Starting || states[1] != Ready {
		t.Fatalf("starts %d, states %v", starts, states)
	}
}

func TestFailureHoldsUntilRetry(t *testing.T) {
	starts := 0
	boom := errors.New("boom")
	s := New(Options{Start: func(context.Context, string, []lsp.WorkspaceFolder, func(error)) (Client, error) {
		starts++
		return nil, boom
	}})
	defer s.StopWait(time.Second)
	s.Get(context.Background())
	if _, err := s.Get(context.Background()); err != boom || starts != 1 {
		t.Fatalf("err %v, starts %d", err, starts)
	}
	s.Retry()
	s.Get(context.Background())
	if starts != 2 {
		t.Fatalf("starts after Retry %d", starts)
	}
}

func TestServerWithoutDefinitionIsReady(t *testing.T) {
	starts := 0
	s := New(Options{Start: serving(&fakeClient{noDefinition: true}, &starts)})
	defer s.StopWait(time.Second)
	if _, err := s.Get(context.Background()); err != nil || serverState(s) != Ready {
		t.Fatalf("optional definition support prevented readiness: %v", err)
	}
}

func TestRecoverRestartsOnce(t *testing.T) {
	starts := 0
	missing := errors.New("missing")
	s := New(Options{
		Start: func(context.Context, string, []lsp.WorkspaceFolder, func(error)) (Client, error) {
			starts++
			return nil, missing
		},
		Recover: func(err error) bool { return err == missing },
	})
	defer s.StopWait(time.Second)
	if _, err := s.Get(context.Background()); err != missing {
		t.Fatalf("err %v", err)
	}
	if starts != 2 {
		t.Fatalf("starts %d, want the first and one recovery", starts)
	}
}

func TestExitOfAnOldServerIsIgnored(t *testing.T) {
	var exits []func(error)
	s := New(Options{Name: "fake", Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
		exits = append(exits, onExit)
		return &fakeClient{}, nil
	}})
	defer s.StopWait(time.Second)
	s.Get(context.Background())
	exits[0](errors.New("crashed"))
	if st, _ := s.Status(); st != Failed {
		t.Fatalf("state %v after the exit", st)
	}
	s.Retry()
	s.Get(context.Background())
	exits[0](errors.New("late"))
	if st, _ := s.Status(); st != Ready {
		t.Fatalf("state %v after an old server's exit", st)
	}
}

func TestSyncOpensThenChangesInOrder(t *testing.T) {
	fc := &fakeClient{}
	starts := 0
	s := New(Options{Start: serving(fc, &starts), Language: func(string) string { return "txt" }})
	defer s.StopWait(time.Second)
	ctx := context.Background()
	c, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("a")
	const docURI = "file:///w/a.txt"
	if err := s.Sync(ctx, c, docURI, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	s.Sync(ctx, c, docURI, buf.Snapshot()) // unchanged: nothing sent
	buf.Set("ab")
	s.Sync(ctx, c, docURI, buf.Snapshot())
	s.Close(docURI)
	s.Changed(docURI, buf.Snapshot()) // closed: nothing sent
	s.Sync(ctx, c, docURI, buf.Snapshot())
	want := []string{"open txt a", "change ab", "close", "open txt ab"}
	if got := fc.got(); len(got) != len(want) || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] || got[3] != want[3] {
		t.Fatalf("calls %q, want %q", got, want)
	}
}

func TestQueueSyncWaitsForInFlightChange(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{name: "success"},
		{name: "error", err: errors.New("change failed")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			changeStarted := make(chan struct{})
			releaseChange := make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(releaseChange) }) }

			fc := &fakeClient{
				changeStarted: changeStarted,
				releaseChange: releaseChange,
				changeErr:     tc.err,
			}
			s := New(Options{Start: serving(fc, new(int))})
			defer s.StopWait(time.Second)
			defer release()
			ctx := context.Background()
			client, err := s.Get(ctx)
			if err != nil {
				t.Fatal(err)
			}
			buf := editor.NewBuffer("a")
			const docURI = "file:///w/a.txt"
			if err := s.Sync(ctx, client, docURI, buf.Snapshot()); err != nil {
				t.Fatal(err)
			}
			buf.Set("ab")
			s.Changed(docURI, buf.Snapshot())
			select {
			case <-changeStarted:
			case <-time.After(time.Second):
				t.Fatal("DidChange did not start")
			}

			s.mu.Lock()
			wait := s.queueSync(client, docURI, buf.Snapshot())
			s.mu.Unlock()
			select {
			case <-wait.done:
				t.Fatalf("same-version Sync returned before the queued DidChange completed: %v", wait.err)
			default:
			}

			release()
			select {
			case <-wait.done:
				err := wait.err
				if !errors.Is(err, tc.err) {
					t.Fatalf("Sync error = %v, want %v", err, tc.err)
				}
			case <-time.After(time.Second):
				t.Fatal("Sync did not finish after DidChange was released")
			}
		})
	}
}

func TestStoppedServerRefuses(t *testing.T) {
	starts := 0
	s := New(Options{Start: serving(&fakeClient{}, &starts)})
	s.Stop()
	if _, err := s.Get(context.Background()); !errors.Is(err, context.Canceled) || starts != 0 {
		t.Fatalf("err %v, starts %d", err, starts)
	}
}

func TestExitedServerRestartsOnBackoff(t *testing.T) {
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{10 * time.Millisecond, 10 * time.Millisecond}

	var mu sync.Mutex
	var exits []func(error)
	var starts atomic.Int32
	s := New(Options{Name: "fake", Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
		mu.Lock()
		defer mu.Unlock()
		exits = append(exits, onExit)
		starts.Add(1)
		return &fakeClient{}, nil
	}})
	defer s.StopWait(time.Second)
	s.Get(context.Background())

	exitAt := func(i int) func(error) {
		mu.Lock()
		defer mu.Unlock()
		return exits[i]
	}

	exitAt(0)(errors.New("crashed"))
	if st, _ := s.Status(); st != Failed {
		t.Fatalf("state %v after the exit", st)
	}
	waitFor(t, func() bool { return starts.Load() == 2 && serverState(s) == Ready })

	// A crash before Ready is not restarted again: exits[1] belongs to
	// a live server, so it reports again, and the retries run out.
	exitAt(1)(errors.New("crashed"))
	waitFor(t, func() bool { return starts.Load() == 3 && serverState(s) == Ready })
	exitAt(2)(errors.New("crashed"))
	time.Sleep(50 * time.Millisecond)
	if st, _ := s.Status(); st != Failed || starts.Load() != 3 {
		t.Fatalf("state %v, starts %d after the retries ran out", st, starts.Load())
	}
}

func TestNeverReadyExitIsNotRestarted(t *testing.T) {
	old := restartDelays
	defer func() { restartDelays = old }()
	restartDelays = [maxRestarts]time.Duration{10 * time.Millisecond, 10 * time.Millisecond}

	var mu sync.Mutex
	var exits []func(error)
	var starts atomic.Int32
	s := New(Options{Name: "fake", Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
		mu.Lock()
		defer mu.Unlock()
		exits = append(exits, onExit)
		if starts.Add(1) == 1 {
			return &fakeClient{}, nil
		}
		return nil, errors.New("won't start")
	}})
	defer s.StopWait(time.Second)
	s.Get(context.Background())
	mu.Lock()
	exit := exits[0]
	mu.Unlock()
	exit(errors.New("crashed"))
	waitFor(t, func() bool { return starts.Load() == 2 && serverState(s) == Failed })
	time.Sleep(50 * time.Millisecond)
	if st, _ := s.Status(); st != Failed || starts.Load() != 2 {
		t.Fatalf("state %v, starts %d; the failed restart must not loop", st, starts.Load())
	}
}

func serverState(s *Server) State {
	st, _ := s.Status()
	return st
}

func waitFor(t *testing.T, ok func() bool) {
	t.Helper()
	for i := 0; i < 500; i++ {
		if ok() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("timed out waiting")
}

func TestBlockedWriterFailsSessionOnQueueOverflow(t *testing.T) {
	blocked, release := make(chan struct{}), make(chan struct{})
	c := &fakeClient{changeStarted: blocked, releaseChange: release}
	s := New(Options{Start: serving(c, new(int))})
	defer s.StopWait(time.Second)
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("open")
	const uri = "file:///a.go"
	if err = s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	buf.Set("changed")
	s.Changed(uri, buf.Snapshot())
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	for i := 0; i < 1024; i++ {
		if !s.Open(c, string(rune(0x400+i)), buf.Snapshot()) {
			if state, err := s.Status(); state != Failed || !errors.Is(err, errQueueFull) {
				t.Fatalf("lost notification without failure: %v %v", state, err)
			}
			return
		}
	}
	t.Fatal("blocked writer accepted unbounded notifications")
}

func TestBlockedWriterFailsSessionOnByteOverflow(t *testing.T) {
	// Payload bytes, not notification count, must trip the queue bound:
	// a few large documents behind a blocked writer cannot accumulate.
	small := outboxMaxBytes
	outboxMaxBytes = 1 << 20
	defer func() { outboxMaxBytes = small }()

	blocked, release := make(chan struct{}), make(chan struct{})
	c := &fakeClient{changeStarted: blocked, releaseChange: release}
	s := New(Options{Start: serving(c, new(int))})
	defer s.StopWait(time.Second)
	defer close(release)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("open")
	const uri = "file:///a.go"
	if err = s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	buf.Set("changed")
	s.Changed(uri, buf.Snapshot())
	select {
	case <-blocked:
	case <-ctx.Done():
		t.Fatal(ctx.Err())
	}
	large := editor.NewBuffer(strings.Repeat("x", 256<<10))
	for i := 0; i < 64; i++ {
		if !s.Open(client, fmt.Sprintf("file:///big%d.go", i), large.Snapshot()) {
			if state, err := s.Status(); state != Failed || !errors.Is(err, errQueueFull) {
				t.Fatalf("byte overflow did not fail the session: %v %v", state, err)
			}
			return
		}
	}
	t.Fatal("blocked writer accepted unbounded payload bytes")
}
