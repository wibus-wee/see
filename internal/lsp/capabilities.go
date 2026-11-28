package lsp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"see/internal/operr"
	"slices"
)

func providerEnabled(raw json.RawMessage) bool {
	raw = bytes.TrimSpace(raw)
	return bytes.Equal(raw, []byte("true")) || len(raw) > 0 && raw[0] == '{'
}

// Supports reports the negotiated operation for this connection. Reconnecting negotiates a new snapshot.
func (c *Client) Supports(method string) bool { return c.operations[method] }

// Operations lists the supported wire methods in stable order.
func (c *Client) Operations() []string {
	var ops []string
	for op, ok := range c.operations {
		if ok {
			ops = append(ops, op)
		}
	}
	slices.Sort(ops)
	return ops
}
func unsupported(method string) *operr.Error {
	e := operr.New(operr.Unsupported, operr.Permanent, nil)
	e.Operation = method
	e.Finality = operr.NoEffect
	return e
}
func (c *Client) call(ctx context.Context, method string, params, result any) error {
	if !c.Supports(method) {
		return unsupported(method)
	}
	err := c.conn.Call(ctx, method, params, result)
	var rpc *Error
	if errors.As(err, &rpc) && rpc.Code == CodeMethodNotFound {
		c.logf("LSP capability mismatch: %s", method)
	}
	return err
}

// SupportsCommand reports whether the server accepts this command identifier.
func (c *Client) SupportsCommand(command string) bool { return slices.Contains(c.commands, command) }
