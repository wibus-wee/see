package langserver

// Independent re-verification probe for a shape the fixer acknowledged:
// a document whose initial didOpen fails is registered locally anyway,
// and the next edit sends didChange for a document the server never saw
// opened — protocol-wise not strict. The matrix's REQ-EXT-010 rationale
// ("changes carry the document whole, so a landed change means the
// server's copy is current again") is exact for didChange failures but
// does not cover this case. This probe pins the actual behavior so the
// documentation gap can be judged against evidence.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
)

type openFailClient struct {
	fakeClient
	openErr error
}

func (c *openFailClient) DidOpen(uri, language string, version int, text string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, "open-attempt "+text)
	return c.openErr
}

func TestReverifyFailedOpenHealsByProtocolViolatingChange(t *testing.T) {
	c := &openFailClient{openErr: errors.New("pipe hiccup on open")}
	starts := 0
	s := New(Options{Name: "fake", Start: serving(c, &starts)})
	defer s.StopWait(time.Second)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	buf := editor.NewBuffer("v1 text")
	const uri = "file:///never-opened.go"
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err == nil {
		t.Fatal("control failed: open failure not reported")
	}
	waitFor(t, func() bool { st, _ := s.Status(); return st == Degraded })

	// A retry of the same content resends nothing: the document is
	// registered at version 1 locally even though the server never saw it.
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err == nil {
		t.Fatal("same-version re-sync reported success against a never-opened document")
	}
	if n := countCalls(c, "open-attempt"); n != 1 {
		t.Fatalf("same-version re-sync re-sent didOpen %d times; the failure is not retried", n)
	}

	// The next edit does not re-open the document — it sends didChange
	// for a document the server never received. Per the LSP protocol a
	// server may legitimately reject that change ("document not open"),
	// so the recovery the matrix describes is not guaranteed for this
	// case: it heals only against servers that tolerate the violation.
	c.openErr = nil
	buf.Set("v2 text")
	if err := s.Sync(ctx, client, uri, buf.Snapshot()); err != nil {
		t.Fatalf("change after failed open: %v", err)
	}
	got := c.got()
	// The exact shape: the one open attempt failed (the client returned
	// the injected error for it), and the next call is a bare change —
	// no second open in between.
	want := []string{"open-attempt v1 text", "change v2 text"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("server-visible calls differ from the pinned shape %v: %v", want, got)
	}
	if n := countCalls(c, "open-attempt"); n != 1 {
		t.Fatalf("the failed open was eventually retried (%d attempts) — behavior changed; re-evaluate this probe", n)
	}
	// And the state machine recovers on the strength of that violating
	// change, so Degraded-clearing is only as sound as the server's
	// tolerance.
	waitFor(t, func() bool { st, _ := s.Status(); return st == Ready })
	t.Logf("pinned shape: server saw %v — change without a successful open; recovery depends on server tolerance", got)
}

func countCalls(c *openFailClient, prefix string) int {
	n := 0
	for _, call := range c.got() {
		if strings.HasPrefix(call, prefix) {
			n++
		}
	}
	return n
}
