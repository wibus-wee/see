package lsp

import (
	"bufio"
	"context"
	"errors"
	"io"
	"testing"
	"time"
)

func TestCancelBlockedInitializedNotification(t *testing.T) {
	toClient, fromServer := io.Pipe()
	toServer, fromClient := io.Pipe()
	defer fromServer.Close()
	defer toServer.Close()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := Connect(ctx, toClient, fromClient, Options{}); result <- err }()
	if _, err := ReadMessage(bufio.NewReader(toServer)); err != nil {
		t.Fatal(err)
	}
	if err := WriteMessage(fromServer, []byte(`{"jsonrpc":"2.0","id":1,"result":{"capabilities":{}}}`)); err != nil {
		t.Fatal(err)
	}
	// Consume just one byte of the initialized notification, leaving its write blocked.
	var one [1]byte
	if _, err := io.ReadFull(toServer, one[:]); err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("initialized write outlived context")
	}
}
