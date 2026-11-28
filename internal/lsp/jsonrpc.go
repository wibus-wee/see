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
	"time"

	"see/internal/operr"
)

// maxMessage bounds the body of one message, so that a broken server
// cannot make the client allocate without limit.
const (
	maxMessage = 256 << 20
	maxHeader  = 16 << 10
	maxPending = 128
	maxServing = 32
)

var ErrBusy = errors.New("lsp: request limit exceeded")

// JSON-RPC error codes.
const (
	CodeParseError     = -32700
	CodeInvalidRequest = -32600
	CodeMethodNotFound = -32601
	CodeInvalidParams  = -32602
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
	remaining := maxHeader
	for {
		var header []byte
		for {
			part, err := r.ReadSlice('\n')
			remaining -= len(part)
			if remaining < 0 {
				return nil, errors.New("lsp: oversized message header")
			}
			header = append(header, part...)
			if err == bufio.ErrBufferFull {
				continue
			}
			if err == io.EOF && remaining == maxHeader {
				return nil, io.EOF
			}
			if err != nil {
				if err == io.EOF {
					err = io.ErrUnexpectedEOF
				}
				return nil, fmt.Errorf("lsp: incomplete header: %w", err)
			}
			break
		}
		line := strings.TrimRight(string(header), "\r\n")
		if line == "" {
			if length < 0 {
				// A blank line before any header: tolerate it.
				continue
			}
			break
		}
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			return nil, errors.New("lsp: malformed header")
		}
		if strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 0 {
				return nil, errors.New("lsp: invalid Content-Length")
			}
			if length >= 0 {
				return nil, errors.New("lsp: duplicate Content-Length")
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
	writes  chan outgoing
	handler Handler
	logf    func(format string, args ...any)

	mu      sync.Mutex
	nextID  int64
	pending map[int64]chan response
	closed  bool
	err     error
	done    chan struct{}
	serving chan struct{}
	serveWG sync.WaitGroup
}

// NewConn makes a connection that writes to w. Call Run with the stream
// to read from.
func NewConn(w io.Writer, h Handler, logf func(string, ...any)) *Conn {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	c := &Conn{
		w: w, handler: h, logf: logf,
		writes: make(chan outgoing, maxPending), serving: make(chan struct{}, maxServing),
		pending: make(map[int64]chan response), done: make(chan struct{}),
	}
	go c.writeLoop()
	return c
}

// Done is closed when the input stream ends or fails.
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
		if err = c.dispatch(body); err != nil {
			break
		}
	}
	if err == io.EOF {
		err = ErrClosed
	}
	c.mu.Lock()
	c.closed, c.err = true, err
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.mu.Unlock()

	// Done must mean the connection is over entirely: a request being
	// answered can still be writing its reply when the input stream
	// ends, and whoever waits on Done — a test's teardown, the process
	// supervisor — must not unmake the connection's world under it.
	c.serveWG.Wait()
	close(c.done)
	return err
}

func (c *Conn) dispatch(body []byte) error {
	var m message
	if err := json.Unmarshal(body, &m); err != nil {
		c.logf("lsp: dropping unparsable message")
		return nil
	}
	switch {
	case m.ID != nil && m.Method != "":
		select {
		case c.serving <- struct{}{}:
		default:
			return ErrBusy
		}
		c.serveWG.Add(1)
		go func() {
			defer c.serveWG.Done()
			defer func() { <-c.serving }()
			c.serve(*m.ID, m.Method, m.Params)
		}()
	case m.ID != nil:
		id, err := strconv.ParseInt(strings.Trim(string(*m.ID), `"`), 10, 64)
		if err != nil {
			c.logf("lsp: response with invalid id")
			return nil
		}
		c.mu.Lock()
		ch := c.pending[id]
		delete(c.pending, id)
		c.mu.Unlock()
		if ch == nil {
			return nil // canceled or answered already
		}
		ch <- response{result: m.Result, err: m.Error}
	case m.Method != "":
		if c.handler != nil {
			c.handler.Notify(m.Method, m.Params)
		}
	default:
		c.logf("lsp: dropping message without id or method")
	}
	return nil
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
		c.logf("lsp: could not send reply")
	}
}

type outgoing struct {
	ctx    context.Context
	body   []byte
	result chan error
}

func (c *Conn) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case m := <-c.writes:
			err := m.ctx.Err()
			if err == nil {
				err = WriteMessage(c.w, m.body)
			}
			if m.result != nil {
				m.result <- err
			}
		}
	}
}

func (c *Conn) writeContext(ctx context.Context, v any) error {
	body, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(body) > maxMessage {
		return errors.New("lsp: outbound message too large")
	}
	result := make(chan error, 1)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	case c.writes <- outgoing{ctx, body, result}:
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return ErrClosed
	case err := <-result:
		return err
	}
}
func (c *Conn) write(v any) error { return c.writeContext(context.Background(), v) }

// Never let a peer that stopped reading hold the cancelling caller. If the
// queue is full, forgetting the request still discards any late response.
func (c *Conn) cancel(id int64) {
	c.forget(id)
	body, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "$/cancelRequest", "params": map[string]any{"id": id}})
	select {
	case c.writes <- outgoing{context.Background(), body, nil}:
	default:
	}
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

func classifyRPCFailure(failure error) *operr.Error {
	var e *operr.Error
	var rpc *Error
	switch {
	case errors.As(failure, &rpc):
		// Server messages and data may include document or environment contents.
		safe := &Error{Code: rpc.Code, Message: "request failed"}
		switch rpc.Code {
		case CodeMethodNotFound:
			e = operr.New(operr.Unsupported, operr.Permanent, safe)
		case CodeInvalidRequest, CodeInvalidParams:
			e = operr.New(operr.InvalidInput, operr.Permanent, safe)
		case CodeRequestCancel:
			e = operr.New(operr.Cancelled, operr.Permanent, errors.Join(context.Canceled, safe))
		case -32801:
			e = operr.New(operr.Conflict, operr.Permanent, safe) // ContentModified
		default:
			e = operr.New(operr.Unexpected, operr.Temporary, safe)
		}
	case errors.Is(failure, ErrBusy):
		e = operr.New(operr.RateLimited, operr.Temporary, ErrBusy)
	default:
		e = operr.Classify(failure)
	}
	return e
}

// Call sends a request and waits for its response, decoding the result
// into result when it is not nil. Canceling ctx stops waiting and tells
// the server with $/cancelRequest.
func (c *Conn) Call(ctx context.Context, method string, params any, result any) (failure error) {
	started := time.Now()
	finality := operr.NoEffect
	defer func() {
		if failure == nil {
			return
		}
		e := classifyRPCFailure(failure)
		e.Operation = method
		e.Duration = time.Since(started)
		e.Attempt = 1
		e.Finality = finality
		failure = e
	}()
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return ErrClosed
	}
	if len(c.pending) >= maxPending {
		c.mu.Unlock()
		return ErrBusy
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
	finality = operr.Uncertain
	var r response
	if err := c.writeContext(ctx, req); err != nil {
		if ctx.Err() != nil {
			c.cancel(id)
			return ctx.Err()
		}
		c.forget(id)
		// A final reply may have arrived immediately before EOF.
		select {
		case reply, ok := <-ch:
			if !ok {
				return err
			}
			r = reply
		default:
			return err
		}
	} else {
		select {
		case reply, ok := <-ch:
			if !ok {
				return ErrClosed
			}
			r = reply
		case <-ctx.Done():
			c.cancel(id)
			return ctx.Err()
		}
	}
	if r.err != nil {
		return r.err
	}
	if result != nil && len(r.result) > 0 {
		if err := json.Unmarshal(r.result, result); err != nil {
			return fmt.Errorf("lsp: decoding %s result: %w", method, err)
		}
	}
	return nil
}

func (c *Conn) forget(id int64) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}
