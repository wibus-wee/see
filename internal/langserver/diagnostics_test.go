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
		onDisk        bool
	}
	var reports []report
	clients := []*publishingClient{{}, {}}
	var exits []func(error)
	s := New(Options{
		Name: "fake",
		Start: func(_ context.Context, _ string, _ []lsp.WorkspaceFolder, onExit func(error)) (Client, error) {
			c := clients[len(exits)]
			exits = append(exits, onExit)
			return c, nil
		},
		OnDiagnostics: func(_ *Server, r DiagnosticReport) {
			if r.Client.Encoding() != lsp.UTF16 {
				t.Errorf("encoding: %q", r.Client.Encoding())
			}
			got := report{message: r.Items[0].Message, onDisk: r.Buffer == nil}
			if r.Buffer != nil {
				got.text, got.version = r.Buffer.String(), r.Buffer.Version()
			}
			reports = append(reports, got)
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
	publish(clients[0], nil, "on-disk problem")
	if len(reports) != 3 || !reports[2].onDisk || reports[2].message != "on-disk problem" {
		t.Fatalf("workspace publication: %+v", reports)
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
	if len(reports) != 4 || reports[3].message != "recovered problem" || reports[3].text != "edited" {
		t.Fatalf("restart publications: %+v", reports)
	}
	s.Stop()
	publish(clients[1], nil, "after shutdown")
	if len(reports) != 4 {
		t.Fatalf("stopped server published: %+v", reports)
	}
}
