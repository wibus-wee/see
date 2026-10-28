package lsp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

// maxMessage bounds the body of one message, so that a broken server
// cannot make the client allocate without limit.
const maxMessage = 256 << 20

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInternalError  = -32603
	CodeRequestCancel  = -32800
)

// ErrClosed is returned by calls on a connection whose stream has ended.
var ErrClosed = errors.New("lsp: connection closed")

// Error is a JSON-RPC error response.
type Error struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *Error) Error() string { return fmt.Sprintf("lsp: %s (%d)", e.Message, e.Code) }

// message is any JSON-RPC 2.0 message: a request (ID and Method), a
// notification (Method alone) or a response (ID alone).
type message struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Method  string           `json:"method,omitempty"`
	Params  json.RawMessage  `json:"params,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *Error           `json:"error,omitempty"`
}

// ReadMessage reads one message framed with a Content-Length header.
func ReadMessage(r *bufio.Reader) ([]byte, error) {
	length := -1
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			if err == io.EOF && line == "" && length < 0 {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("lsp: reading header: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if length < 0 {
				// A blank line before any header: tolerate it.
				continue
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, fmt.Errorf("lsp: malformed header %q", line)
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, fmt.Errorf("lsp: bad Content-Length %q", value)
			}
			if n > maxMessage {
				return nil, fmt.Errorf("lsp: message of %d bytes is too large", n)
			}
			length = n
		}
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, fmt.Errorf("lsp: reading body: %w", err)
	}
	return body, nil
}

// WriteMessage writes body framed with a Content-Length header.
func WriteMessage(w io.Writer, body []byte) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Content-Length: %d\r\n\r\n", len(body))
	b.Write(body)
	_, err := w.Write(b.Bytes())
	return err
}

// Handler answers a server's requests and takes its notifications. Handle
// returns the result of a request, or an *Error; it runs on its own
// goroutine, so it may block without stalling the read loop. Notify runs
// on the read loop and must return quickly.
type Handler interface {
	Handle(ctx context.Context, method string, params json.RawMessage) (any, error)
	Notify(method string, params json.RawMessage)
}

type response struct {
	result json.RawMessage
	err    *Error
}

// Conn is a JSON-RPC 2.0 connection over a byte stream, with LSP's
// framing: it matches responses to calls by ID, answers the peer's
// requests through a Handler, and keeps reading past messages it cannot
// parse.
type Conn struct {
	w       io.Writer
	wmu     sync.Mutex
	handler Handler
	logf    func(format string, args ...any)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan response
	closed  bool
	err     error
	done    chan struct{}
	serving sync.WaitGroup // the replies to the peer's requests on their way
}

// NewConn makes a connection that writes to w. Call Run with the stream
// to read from.
func NewConn(w io.Writer, h Handler, logf func(string, ...any)) *Conn {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &Conn{w: w, handler: h, logf: logf, pending: map[int64]chan response{}, done: make(chan struct{})}
}

// Done is closed once Run returns, after the replies on their way.
func (c *Conn) Done() <-chan struct{} { return c.done }

// Err is why Run returned, once Done is closed.
func (c *Conn) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}

// Run reads messages from r until it ends or fails, then fails the calls
// still waiting.
func (c *Conn) Run(r io.Reader) error {
	br := bufio.NewReaderSize(r, 64<<10)
	var err error
	for {
		var body []byte
		body, err = ReadMessage(br)
		if err != nil {
			break
		}
		c.dispatch(body)
	}
	if err == io.EOF {
		err = ErrClosed
	}
	c.mu.Lock()
	c.closed, c.err = true, err
	pending := c.pending
	c.pending = map[int64]chan response{}
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- response{err: &Error{Code: CodeInternalError, Message: "connection closed: " + err.Error()}}
	}
	c.serving.Wait()
	close(c.done)
	return err
}

func (c *Conn) dispatch(body []byte) {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		c.logf("lsp: dropping unparsable message: %v", err)
		return
	}
	switch {
	case m.ID != nil && m.Method != "":
		c.serving.Add(1)
		go func() {
			defer c.serving.Done()
			c.serve(*m.ID, m.Method, m.Params)
		}()
	case m.ID != nil:
		id, err := strconv.ParseInt(strings.Trim(string(*m.ID), `"`), 10, 64)
		if err != nil {
			c.logf("lsp: response with unknown id %s", *m.ID)
			return
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			return // canceled or answered already
		}
		ch <- response{result: m.Result, err: m.Error}
	case m.Method != "":
		if c.handler != nil {
			c.handler.Notify(m.Method, m.Params)
		}
	default:
		c.logf("lsp: dropping message without id or method")
	}
}

func (c *Conn) serve(id json.RawMessage, method string, params json.RawMessage) {
	var result any
	var err error
	if c.handler == nil {
		err = &Error{Code: CodeMethodNotFound, Message: "method not found: " + method}
	} else {
		result, err = c.handler.Handle(context.Background(), method, params)
	}
	reply := map[string]any{"jsonrpc": "2.0", "id": id}
	if err != nil {
		var rpcErr *Error
		if !errors.As(err, &rpcErr) {
			rpcErr = &Error{Code: CodeInternalError, Message: err.Error()}
		}
		reply["error"] = rpcErr
	} else {
		reply["result"] = result // nil marshals as null, as the protocol wants
	}
	if werr := c.write(reply); werr != nil {
		c.logf("lsp: replying to %s: %v", method, werr)
	}
}

func (c *Conn) write(v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	return WriteMessage(c.w, body)
}

// Notify sends a notification.
func (c *Conn) Notify(method string, params any) error {
	if c.isClosed() {
		return ErrClosed
	}
	return c.write(map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}

func (c *Conn) isClosed() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closed
}

// Call sends a request and waits for its response, decoding the result
// into result when it is not nil. Canceling ctx stops waiting and tells
// the server with $/cancelRequest.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	c.nextID++
	id := c.nextID
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	req := map[string]any{"jsonrpc": "2.0", "id": id, "method": method}
	if params != nil {
		req["params"] = params
	}
	if err := c.write(req); err != nil {
		c.forget(id)
		return err
	}
	select {
	case r := <-ch:
		if r.err != nil {
			return r.err
		}
		if result != nil && len(r.result) > 0 {
			if err := json.Unmarshal(r.result, result); err != nil {
				return fmt.Errorf("lsp: decoding %s result: %w", method, err)
			}
		}
		return nil
	case <-ctx.Done():
		c.forget(id)
		_ = c.Notify("$/cancelRequest", map[string]any{"id": id})
		return ctx.Err()
	}
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
