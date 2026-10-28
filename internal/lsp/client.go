package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// Options configure a Client.
type Options struct {
	// RootPath is the workspace's directory, RootURI its URI.
	RootPath, RootURI string
	// InitializationOptions are passed to the server during initialization.
	InitializationOptions any
	// Name names the workspace folder.
	Name string
	// Logf receives the client's diagnostics; never the documents' text.
	Logf func(format string, args ...any)
	// OnExit is called once when the server's process ends or the
	// connection breaks, with why, unless Shutdown asked for it.
	OnExit func(err error)
}

// Client is a language client of one server: it initializes it, keeps
// its documents in sync, asks for definitions and shuts it down.
type Client struct {
	conn            *Conn
	opts            Options
	encoding        string
	sync            int
	openNote        bool
	defs            bool
	pullDiagnostics bool
	diagMu          sync.Mutex
	onDiagnostics   func(string, *int, []Diagnostic)

	cmd    *exec.Cmd
	stdin  io.Closer
	stderr *tail

	mu       sync.Mutex
	stopping bool
	exited   chan struct{}
}

// Start runs the server command with stdin and stdout for the protocol
// and stderr kept apart, then initializes it.
func Start(ctx context.Context, command string, args []string, opts Options) (*Client, error) {
	cmd := exec.Command(command, args...)
	cmd.Dir = opts.RootPath
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	errTail := &tail{max: 16 << 10, logf: opts.Logf}
	cmd.Stderr = errTail
	// Wait gives up on the output pipes this long after the process ends,
	// should a child of the server keep them open.
	cmd.WaitDelay = 2 * time.Second
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("starting %s: %w", command, err)
	}
	c := newClient(stdin, stdout, opts)
	c.cmd, c.stdin, c.stderr = cmd, stdin, errTail
	go func() {
		<-c.conn.Done()
		// The stream ended or broke: a server that still runs after a
		// protocol error is of no use any more.
		_ = cmd.Process.Kill()
		err := cmd.Wait()
		if err == nil {
			err = c.conn.Err()
		}

		c.finish(err)
	}()
	if err := c.initialize(ctx); err != nil {
		c.Kill()
		return nil, err
	}
	return c, nil
}

// Connect makes a client of a server already reached through r and w, as
// a test's, and initializes it.
func Connect(ctx context.Context, r io.Reader, w io.WriteCloser, opts Options) (*Client, error) {
	c := newClient(w, r, opts)
	c.stdin = w
	go func() {
		<-c.conn.Done()
		c.finish(c.conn.Err())
	}()
	if err := c.initialize(ctx); err != nil {
		w.Close()
		return nil, err
	}
	return c, nil
}

func newClient(w io.Writer, r io.Reader, opts Options) *Client {
	c := &Client{opts: opts, exited: make(chan struct{})}
	c.conn = NewConn(w, (*handler)(c), opts.Logf)
	go c.conn.Run(r)
	return c
}

func (c *Client) logf(format string, args ...any) {
	if c.opts.Logf != nil {
		c.opts.Logf(format, args...)
	}
}

func (c *Client) finish(err error) {
	c.mu.Lock()
	select {
	case <-c.exited:
		c.mu.Unlock()
		return
	default:
	}
	stopping := c.stopping
	close(c.exited)
	c.mu.Unlock()
	if !stopping && c.opts.OnExit != nil {
		if err == nil {
			err = errors.New("language server exited")
		}
		c.opts.OnExit(err)
	}
}

// Exited is closed once the server has gone.
func (c *Client) Exited() <-chan struct{} { return c.exited }

// Encoding is the negotiated position encoding.
func (c *Client) Encoding() string { return c.encoding }

// HasDefinition reports whether the server answers definition requests.
func (c *Client) HasDefinition() bool { return c.defs }

// WorkspaceSymbols searches declarations across the workspace.
type WorkspaceSymbol struct {
	Name     string   `json:"name"`
	Location Location `json:"location"`
}

func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	var symbols []WorkspaceSymbol
	if err := c.conn.Call(ctx, "workspace/symbol", map[string]any{"query": query}, &symbols); err != nil {
		return nil, err
	}
	out := make([]WorkspaceSymbol, 0, len(symbols))
	for _, s := range symbols {
		if s.Location.URI != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

func (c *Client) initialize(ctx context.Context) error {
	params := map[string]any{
		"processId":  os.Getpid(),
		"clientInfo": map[string]any{"name": "See", "version": "0.1"},
		"rootUri":    c.opts.RootURI,
		"rootPath":   c.opts.RootPath,
		"workspaceFolders": []map[string]any{
			{"uri": c.opts.RootURI, "name": c.opts.Name},
		},
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{UTF8, UTF16},
			},
			"textDocument": map[string]any{
				"synchronization": map[string]any{"didSave": false, "dynamicRegistration": false},
				"references":      map[string]any{"dynamicRegistration": false},
				"callHierarchy":   map[string]any{"dynamicRegistration": false},
				"definition":      map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"hover":           map[string]any{"contentFormat": []string{"markdown", "plaintext"}, "dynamicRegistration": false},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    true,
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
	if c.opts.InitializationOptions != nil {
		params["initializationOptions"] = c.opts.InitializationOptions
	}
	var res struct {
		Capabilities serverCapabilities `json:"capabilities"`
	}
	if err := c.conn.Call(ctx, "initialize", params, &res); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	c.encoding = NegotiatedEncoding(res.Capabilities.PositionEncoding)
	c.sync, c.openNote = res.Capabilities.syncKind()
	c.defs = res.Capabilities.hasDefinition()
	c.pullDiagnostics = len(res.Capabilities.DiagnosticProvider) > 0 && string(res.Capabilities.DiagnosticProvider) != "null" && string(res.Capabilities.DiagnosticProvider) != "false"
	return c.conn.Notify("initialized", map[string]any{})
}

// DidOpen tells the server a document is open with text at version.
func (c *Client) DidOpen(uri, languageID string, version int, text string) error {
	if !c.openNote {
		return nil
	}
	return c.conn.Notify("textDocument/didOpen", map[string]any{
		"textDocument": map[string]any{"uri": uri, "languageId": languageID, "version": version, "text": text},
	})
}

// DidChange gives the server a document's whole new text at version,
// which full and incremental sync both accept.
func (c *Client) DidChange(uri string, version int, text string) error {
	if c.sync == SyncNone {
		return nil
	}
	return c.conn.Notify("textDocument/didChange", map[string]any{
		"textDocument":   map[string]any{"uri": uri, "version": version},
		"contentChanges": []map[string]any{{"text": text}},
	})
}

// DidClose tells the server a document is closed.
func (c *Client) DidClose(uri string) error {
	if !c.openNote {
		return nil
	}
	return c.conn.Notify("textDocument/didClose", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	})
}

// Definition asks where the symbol at pos of the document is defined.
func (c *Client) Definition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var raw json.RawMessage
	err := c.conn.Call(ctx, "textDocument/definition", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseLocations(raw)
}

// Hover asks what the symbol at pos is: Markdown, and the range it
// covers when the server says.
func (c *Client) Hover(ctx context.Context, uri string, pos Position) (string, *Range, error) {
	var raw json.RawMessage
	err := c.conn.Call(ctx, "textDocument/hover", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw)
	if err != nil {
		return "", nil, err
	}
	return ParseHover(raw)
}

// SetDiagnosticsHandler receives published diagnostics from the server.
func (c *Client) SetDiagnosticsHandler(fn func(string, *int, []Diagnostic)) {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	c.onDiagnostics = fn
}

// Diagnostics requests a full diagnostic report when the server supports it.
func (c *Client) Diagnostics(ctx context.Context, uri string) ([]Diagnostic, bool, error) {
	if !c.pullDiagnostics {
		return nil, false, nil
	}
	var result struct {
		Kind  string       `json:"kind"`
		Items []Diagnostic `json:"items"`
	}
	err := c.conn.Call(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}}, &result)
	return result.Items, result.Kind == "full", err
}

// References returns locations that refer to a symbol.
func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var result []Location
	err := c.conn.Call(ctx, "textDocument/references", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos, "context": map[string]any{"includeDeclaration": false}}, &result)
	return result, err
}

// Callers returns call sites from incoming call hierarchy items.
func (c *Client) Callers(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var items []json.RawMessage
	if err := c.conn.Call(ctx, "textDocument/prepareCallHierarchy", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}, &items); err != nil {
		return nil, err
	}
	var locations []Location
	for _, item := range items {
		var calls []struct {
			From struct {
				URI            string `json:"uri"`
				Name           string `json:"name"`
				SelectionRange Range  `json:"selectionRange"`
			} `json:"from"`
			Ranges []Range `json:"fromRanges"`
		}
		if err := c.conn.Call(ctx, "callHierarchy/incomingCalls", map[string]any{"item": item}, &calls); err != nil {
			return nil, err
		}
		for _, call := range calls {
			if len(call.Ranges) == 0 {
				locations = append(locations, Location{URI: call.From.URI, Range: call.From.SelectionRange, Name: call.From.Name})
			}
			for _, r := range call.Ranges {
				locations = append(locations, Location{URI: call.From.URI, Range: r, Name: call.From.Name})
			}
		}
	}
	return locations, nil
}

// Shutdown asks the server to shut down and exit, and kills it when it
// has not within timeout, even if a write to it is stuck.
func (c *Client) Shutdown(timeout time.Duration) {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	go func() {
		if err := c.conn.Call(ctx, "shutdown", nil, nil); err == nil {
			_ = c.conn.Notify("exit", nil)
		}
		if c.stdin != nil {
			c.stdin.Close()
		}
	}()
	select {
	case <-c.exited:
	case <-ctx.Done():
		c.Kill()
		if c.cmd != nil {
			<-c.exited // the process is killed, so its stdout closes
		}
	}
}

// Kill ends the server's process at once.
func (c *Client) Kill() {
	c.mu.Lock()
	c.stopping = true
	c.mu.Unlock()
	if c.stdin != nil {
		c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
	}
}

// handler answers the server's requests with the least a client must.
type handler Client

func (h *handler) Handle(ctx context.Context, method string, params json.RawMessage) (any, error) {
	switch method {
	case "workspace/configuration":
		var p struct {
			Items []json.RawMessage `json:"items"`
		}
		_ = json.Unmarshal(params, &p)
		return make([]any, len(p.Items)), nil // null for each: the server's defaults
	case "workspace/workspaceFolders":
		return []map[string]any{{"uri": h.opts.RootURI, "name": h.opts.Name}}, nil
	case "client/registerCapability", "client/unregisterCapability",
		"window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	}
	return nil, &Error{Code: CodeMethodNotFound, Message: "method not found: " + method}
}

func (h *handler) Notify(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		var p struct {
			URI         string       `json:"uri"`
			Version     *int         `json:"version"`
			Diagnostics []Diagnostic `json:"diagnostics"`
		}
		if json.Unmarshal(params, &p) == nil {
			h.diagMu.Lock()
			fn := h.onDiagnostics
			h.diagMu.Unlock()
			if fn != nil {
				fn(p.URI, p.Version, p.Diagnostics)
			}
		}
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type    int    `json:"type"`
			Message string `json:"message"`
		}
		if json.Unmarshal(params, &p) == nil && p.Type <= 2 { // errors and warnings
			(*Client)(h).logf("lsp: server: %s", p.Message)
		}
	}
}

// tail keeps the last max bytes written to it, as a server's stderr.
type tail struct {
	mu   sync.Mutex
	max  int
	b    []byte
	logf func(string, ...any)
}

func (t *tail) Write(p []byte) (int, error) {
	if t.logf != nil {
		t.logf("server stderr: %s", string(p))
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.b = append(t.b, p...)
	if len(t.b) > t.max {
		t.b = append(t.b[:0], t.b[len(t.b)-t.max:]...)
	}
	return len(p), nil
}

func (t *tail) String() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.b)
}
