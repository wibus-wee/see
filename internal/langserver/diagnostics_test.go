package langserver

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/lsp"
)

type publishingClient struct {
	fakeClient
	publish func(string, *int, []lsp.Diagnostic)
}

func (c *publishingClient) SetDiagnosticsHandler(fn func(string, *int, []lsp.Diagnostic)) {
	c.publish = fn
}

func TestPublishedDiagnosticsRejectObsoleteDocumentAndServer(t *testing.T) {
	type report struct {
		text, message string
		version       uint64
	}
	var reports []report
	clients := []*publishingClient{{}, {}}
	var exits []func(error)
	s := New(Options{
		Name: "fake",
		Start: func(_ context.Context, _ string, onExit func(error)) (Client, error) {
			c := clients[len(exits)]
			exits = append(exits, onExit)
			return c, nil
		},
		OnDiagnostics: func(_ *Server, _ string, snap editor.Snapshot, ds []lsp.Diagnostic, enc string) {
			if enc != lsp.UTF16 {
				t.Errorf("encoding: %q", enc)
			}
			reports = append(reports, report{snap.String(), ds[0].Message, snap.Version()})
		},
	})
	defer s.StopWait(time.Second)
	ctx := context.Background()
	client, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	const docURI = "file:///w/a.go"
	buf := editor.NewBuffer("original")
	if err := s.Sync(ctx, client, docURI, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	originalVersion := 1
	publish := func(c *publishingClient, v *int, message string) {
		c.publish(docURI, v, []lsp.Diagnostic{{Message: message}})
	}
	publish(clients[0], &originalVersion, "original problem")
	if len(reports) != 1 || reports[0].text != "original" {
		t.Fatalf("initial publication: %+v", reports)
	}
	buf.Set("edited")
	if err := s.Sync(ctx, client, docURI, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	publish(clients[0], &originalVersion, "stale edit")
	currentVersion := 2
	publish(clients[0], &currentVersion, "current problem")
	if len(reports) != 2 || reports[1].text != "edited" || reports[1].message != "current problem" || reports[1].version != buf.Version() {
		t.Fatalf("edit publications: %+v", reports)
	}
	s.Close(docURI)
	publish(clients[0], &currentVersion, "closed document")
	if len(reports) != 2 {
		t.Fatalf("closed document published: %+v", reports)
	}
	exits[0](errors.New("crash"))
	s.Retry()
	replacement, err := s.Get(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Sync(ctx, replacement, docURI, buf.Snapshot()); err != nil {
		t.Fatal(err)
	}
	publish(clients[0], nil, "old server without version")
	publish(clients[1], nil, "recovered problem")
	if len(reports) != 3 || reports[2].message != "recovered problem" || reports[2].text != "edited" {
		t.Fatalf("restart publications: %+v", reports)
	}
	s.Stop()
	publish(clients[1], nil, "after shutdown")
	if len(reports) != 3 {
		t.Fatalf("stopped server published: %+v", reports)
	}
}
