package lsp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"see/internal/operr"
	"strings"
	"testing"
	"time"
)

// The peer counts every feature request. A locally rejected operation must not reach it.
func TestCapabilityProcess(t *testing.T) {
	if os.Getenv("SEE_TEST_CAP_PEER") == "1" {
		r := bufio.NewReader(os.Stdin)
		calls := 0
		for {
			b, err := ReadMessage(r)
			if err != nil {
				os.Exit(1)
			}
			var req struct {
				ID     json.RawMessage
				Method string
			}
			if json.Unmarshal(b, &req) != nil {
				os.Exit(1)
			}
			result := "null"
			switch req.Method {
			case "initialize":
				result = `{"capabilities":{"hoverProvider":true,"executeCommandProvider":{"commands":["inspect"]}}}`
				if bad := os.Getenv("SEE_TEST_CAP_BAD"); bad != "" {
					result = bad
				}
				if os.Getenv("SEE_TEST_CAP_SECOND") == "1" {
					result = `{"capabilities":{"definitionProvider":true}}`
				}
			case "initialized":
				continue
			case "exit":
				os.Exit(0)
			case "shutdown":
			case "textDocument/hover":
				result = fmt.Sprintf(`{"contents":"calls=%d"}`, calls)
			default:
				calls++
			}
			if WriteMessage(os.Stdout, []byte(fmt.Sprintf(`{"jsonrpc":"2.0","id":%s,"result":%s}`, req.ID, result))) != nil {
				os.Exit(1)
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	c, err := Start(ctx, exe, []string{"-test.run=^TestCapabilityProcess$"}, Options{Env: []string{"SEE_TEST_CAP_PEER=1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Shutdown(time.Second)
	_, err = c.Definition(ctx, "file:///doc", Position{})
	for _, e := range []error{err, c.ExecuteCommand(ctx, Command{Command: "mutate"})} {
		if e == nil || operr.Classify(e).Kind != operr.Unsupported {
			t.Fatalf("expected unsupported: %v", e)
		}
	}
	if err = c.ExecuteCommand(ctx, Command{Command: "inspect"}); err != nil {
		t.Fatal(err)
	}
	text, _, err := c.Hover(ctx, "file:///doc", Position{})
	if err != nil || text != "calls=1" {
		t.Fatalf("peer observed %q: %v", text, err)
	}
	second, err := Start(ctx, exe, []string{"-test.run=^TestCapabilityProcess$"}, Options{Env: []string{"SEE_TEST_CAP_PEER=1", "SEE_TEST_CAP_SECOND=1"}})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Shutdown(time.Second)
	if !second.Supports("textDocument/definition") || second.Supports("textDocument/hover") || c.Supports("textDocument/definition") {
		t.Fatal("separate sessions shared a capability snapshot")
	}
	if _, err = second.Definition(ctx, "file:///doc", Position{}); err != nil {
		t.Fatal(err)
	}

}

func TestInvalidCapabilityHandshake(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, raw := range []string{`null`, `{}`, `{"capabilities":null}`, `{"capabilities":[]}`, `{"capabilities":{"positionEncoding":"secret-untrusted-encoding"}}`, `{"capabilities":{"positionEncoding":null}}`} {
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		c, err := Start(ctx, exe, []string{"-test.run=^TestCapabilityProcess$"}, Options{Env: []string{"SEE_TEST_CAP_PEER=1", "SEE_TEST_CAP_BAD=" + raw}})
		cancel()
		if c != nil {
			c.Kill()
		}
		if c != nil || err == nil || operr.Classify(err).Status != operr.Permanent || strings.Contains(err.Error(), "secret-untrusted") {
			t.Errorf("bad handshake returned client=%v err=%v", c != nil, err)
		}
	}
}
