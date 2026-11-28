package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"see/internal/operr"
)

// A peer can leave its pipe open without consuming another frame. A caller
// cancelling a large request must not wait for space in that pipe.
func TestCancelWithStalledPipe(t *testing.T) {
	r, w, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer r.Close()
	defer w.Close()
	incoming, peer, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer incoming.Close()
	defer peer.Close()
	c := NewConn(w, nil, nil)
	go c.Run(incoming)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.Call(ctx, "test/query", strings.Repeat("a", 2<<20), nil) }()
	var first [1]byte
	if _, err := r.Read(first[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancel waits for peer to drain stdin")
	}
}

func TestHeaderLimitBeforeNewline(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	defer w.Close()
	go func() { _, _ = w.Write([]byte(strings.Repeat("h", 128<<10))) }()
	done := make(chan error, 1)
	go func() { _, err := ReadMessage(bufio.NewReader(r)); done <- err }()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("accepted oversized header")
		}
	case <-time.After(time.Second):
		t.Fatal("read beyond header limit")
	}
}

// Unlike a local capability rejection, these codes came back from the peer
// after it read the request. Preserve the code, not its diagnostic payload.
func TestRemoteFailureOnPipe(t *testing.T) {
	requests, clientOutput, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer requests.Close()
	defer clientOutput.Close()
	clientInput, replies, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer clientInput.Close()
	defer replies.Close()
	c := NewConn(clientOutput, nil, nil)
	go c.Run(clientInput)
	r := bufio.NewReader(requests)
	for _, tc := range []struct {
		code   int
		kind   operr.Kind
		status operr.Status
	}{{CodeMethodNotFound, operr.Unsupported, operr.Permanent}, {CodeRequestCancel, operr.Cancelled, operr.Permanent}, {-32701, operr.Unexpected, operr.Temporary}} {
		done := make(chan error, 1)
		go func() { done <- c.Call(t.Context(), "test/query", nil, nil) }()
		b, err := ReadMessage(r)
		if err != nil {
			t.Fatal(err)
		}
		var req struct{ ID int }
		if err = json.Unmarshal(b, &req); err != nil {
			t.Fatal(err)
		}
		if err = WriteMessage(replies, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"error":{"code":%d,"message":"private-canary","data":"private-canary"}}`, req.ID, tc.code))); err != nil {
			t.Fatal(err)
		}
		err = <-done
		var e *operr.Error
		var rpc *Error
		if !errors.As(err, &e) || e.Operation != "test/query" || e.Kind != tc.kind || e.Status != tc.status || e.Finality != operr.Uncertain || !errors.As(err, &rpc) || rpc.Code != tc.code || len(rpc.Data) != 0 || strings.Contains(rpc.Message+err.Error(), "private-canary") {
			t.Fatalf("wrong remote error for %d: %#v", tc.code, e)
		}
	}
}
