package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"see/internal/proc"
)

// Options configure a Client.
type Options struct {
	// RootPath is the workspace's directory, RootURI its URI.
	RootPath, RootURI string
	// ServerID names the server for serverConfigurations, such as
	// "gopls"; "" answers workspace/configuration with nulls.
	ServerID string
	// InitializationOptions are passed to the server during initialization.
	InitializationOptions any
	// Env adds environment variables to the server's process, as
	// "NAME=value", merged over the inherited environment.
	Env []string
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
	conn               *Conn
	opts               Options
	encoding           string
	sync               int
	openNote           bool
	defs               bool
	documentHighlights bool
	implementations    bool
	typeDefinitions    bool
	inlayHints         bool
	callHierarchy      bool
	pullDiagnostics    bool
	signatureHelp      bool
	semanticFull       bool
	semanticRange      bool
	semanticLegend     semanticTokensLegend
	diagMu             sync.Mutex
	onDiagnostics      func(string, *int, []Diagnostic)

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
	cmd := proc.Command(command, args...)
	cmd.Dir = opts.RootPath
	cmd.Env = mergeEnv(os.Environ(), opts.Env)
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

// mergeEnv returns base with each "NAME=value" of extra applied: a NAME
// base already carries is replaced, the others are appended.
func mergeEnv(base, extra []string) []string {
	if len(extra) == 0 {
		return base
	}
	overrides := map[string]string{}
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		overrides[name] = kv
	}
	out := make([]string, 0, len(base)+len(extra))
	seen := map[string]bool{}
	for _, kv := range base {
		name, _, _ := strings.Cut(kv, "=")
		if v, ok := overrides[name]; ok {
			out = append(out, v)
			seen[name] = true
		} else {
			out = append(out, kv)
		}
	}
	for _, kv := range extra {
		name, _, _ := strings.Cut(kv, "=")
		if !seen[name] {
			out = append(out, kv)
			seen[name] = true
		}
	}
	return out
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

// HasDocumentHighlights reports whether the server provides semantic occurrences.
func (c *Client) HasDocumentHighlights() bool { return c.documentHighlights }

func (c *Client) HasImplementation() bool { return c.implementations }
func (c *Client) HasTypeDefinition() bool { return c.typeDefinitions }

// HasInlayHints reports whether the server provides inlay hints.
func (c *Client) HasInlayHints() bool { return c.inlayHints }

// HasSignatureHelp reports whether the server answers signature help.
func (c *Client) HasSignatureHelp() bool { return c.signatureHelp }

// HasCallHierarchy reports whether the server answers call hierarchy
// requests, either direction.
func (c *Client) HasCallHierarchy() bool { return c.callHierarchy }

// HasSemanticTokens reports whether the server answers semantic tokens
// requests, whole-document or for a range.
func (c *Client) HasSemanticTokens() bool { return c.semanticFull || c.semanticRange }

// SemanticTokensFull and SemanticTokensRange report which of the two
// semantic tokens requests the server answers.
func (c *Client) SemanticTokensFull() bool  { return c.semanticFull }
func (c *Client) SemanticTokensRange() bool { return c.semanticRange }

// SemanticTokenTypes and SemanticTokenModifiers are the server's
// legend: the names its token stream indexes, in its order.
func (c *Client) SemanticTokenTypes() []string     { return c.semanticLegend.TokenTypes }
func (c *Client) SemanticTokenModifiers() []string { return c.semanticLegend.TokenModifiers }

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
				"synchronization":   map[string]any{"didSave": false, "dynamicRegistration": false},
				"documentSymbol":    map[string]any{"hierarchicalDocumentSymbolSupport": true, "dynamicRegistration": false},
				"references":        map[string]any{"dynamicRegistration": false},
				"callHierarchy":     map[string]any{"dynamicRegistration": false},
				"definition":        map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"documentHighlight": map[string]any{"dynamicRegistration": false},
				"implementation":    map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"typeDefinition":    map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"inlayHint":         map[string]any{},
				"hover":             map[string]any{"contentFormat": []string{"markdown", "plaintext"}, "dynamicRegistration": false},
				"signatureHelp":     map[string]any{"signatureInformation": map[string]any{"documentationFormat": []string{"markdown", "plaintext"}, "parameterInformation": map[string]any{"labelOffsetSupport": true}}, "dynamicRegistration": false},
				"semanticTokens": map[string]any{
					"dynamicRegistration":     false,
					"requests":                map[string]any{"range": true, "full": map[string]any{"delta": false}},
					"tokenTypes":              standardSemanticTokenTypes,
					"tokenModifiers":          standardSemanticTokenModifiers,
					"formats":                 []string{"relative"},
					"overlappingTokenSupport": false,
					"multilineTokenSupport":   false,
				},
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
	c.documentHighlights = res.Capabilities.hasDocumentHighlights()
	c.implementations = (serverCapabilities{DefinitionProvider: res.Capabilities.ImplementationProvider}).hasDefinition()
	c.typeDefinitions = (serverCapabilities{DefinitionProvider: res.Capabilities.TypeDefinitionProvider}).hasDefinition()
	c.inlayHints = res.Capabilities.hasInlayHints()
	c.signatureHelp = res.Capabilities.hasSignatureHelp()
	c.callHierarchy = res.Capabilities.hasCallHierarchy()
	c.pullDiagnostics = len(res.Capabilities.DiagnosticProvider) > 0 && string(res.Capabilities.DiagnosticProvider) != "null" && string(res.Capabilities.DiagnosticProvider) != "false"
	c.semanticLegend, c.semanticFull, c.semanticRange = res.Capabilities.semanticTokens()
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
	return c.locations(ctx, "textDocument/definition", uri, pos)
}

// DocumentHighlights asks where the symbol at pos occurs in this document.
func (c *Client) DocumentHighlights(ctx context.Context, uri string, pos Position) ([]DocumentHighlight, error) {
	var highlights []DocumentHighlight
	err := c.conn.Call(ctx, "textDocument/documentHighlight", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &highlights)
	return highlights, err
}

func (c *Client) Implementation(ctx context.Context, uri string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/implementation", uri, pos)
}

func (c *Client) TypeDefinition(ctx context.Context, uri string, pos Position) ([]Location, error) {
	return c.locations(ctx, "textDocument/typeDefinition", uri, pos)
}

func (c *Client) locations(ctx context.Context, method, uri string, pos Position) ([]Location, error) {
	var raw json.RawMessage
	err := c.conn.Call(ctx, method, map[string]any{
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

// InlayHints asks for the inlay hints of the document in r, the lines
// on screen rather than the whole document.
func (c *Client) InlayHints(ctx context.Context, uri string, r Range) ([]InlayHint, error) {
	var raw json.RawMessage
	err := c.conn.Call(ctx, "textDocument/inlayHint", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        r,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseInlayHints(raw)
}

// SemanticTokens asks for the semantic tokens of the document, for its
// lines first to last when first is not negative, else for the whole
// document: whichever the server offers. A server answering neither
// request reports HasSemanticTokens false instead, and this returns an
// error.
func (c *Client) SemanticTokens(ctx context.Context, uri string, first, last int) (*SemanticTokensData, error) {
	params := map[string]any{"textDocument": map[string]any{"uri": uri}}
	method := ""
	switch {
	case first >= 0 && c.semanticRange:
		method = "textDocument/semanticTokens/range"
		params["range"] = Range{Start: Position{Line: first, Character: 0}, End: Position{Line: last + 1, Character: 0}}
	case c.semanticFull:
		method = "textDocument/semanticTokens/full"
	case c.semanticRange:
		method = "textDocument/semanticTokens/range"
		params["range"] = Range{Start: Position{Line: max(first, 0), Character: 0}, End: Position{Line: max(last, 0) + 1, Character: 0}}
	default:
		return nil, errors.New("lsp: server has no semantic tokens")
	}
	var result SemanticTokensData
	if err := c.conn.Call(ctx, method, params, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// standardSemanticTokenTypes and standardSemanticTokenModifiers are the
// names LSP 3.17 registers, which the client tells servers it reads; a
// server's own legend orders the stream it answers, which may name
// more.
var standardSemanticTokenTypes = []string{
	"namespace", "type", "class", "enum", "interface", "struct",
	"typeParameter", "parameter", "variable", "property", "enumMember",
	"event", "function", "method", "macro", "keyword", "modifier",
	"comment", "string", "number", "regexp", "operator", "decorator",
	"label",
}

var standardSemanticTokenModifiers = []string{
	"declaration", "definition", "readonly", "static", "deprecated",
	"abstract", "async", "modification", "documentation", "defaultLibrary",
}

// SignatureHelp asks for the signatures of the call at pos.
func (c *Client) SignatureHelp(ctx context.Context, uri string, pos Position) (*SignatureHelp, error) {
	var raw json.RawMessage
	err := c.conn.Call(ctx, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseSignatureHelp(raw)
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

// PrepareCallHierarchy asks which symbols the hierarchy at pos of the
// document can grow from. Servers that cannot place one at pos answer
// an error rather than an empty result, which ErrNoCallItem wraps, so
// the caller can tell "nothing here" apart from a failure.
func (c *Client) PrepareCallHierarchy(ctx context.Context, uri string, pos Position) ([]CallItem, error) {
	var items []callItemWire
	if err := c.conn.Call(ctx, "textDocument/prepareCallHierarchy", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}, &items); err != nil {
		var e *Error
		if errors.As(err, &e) && e.Code == 0 {
			return nil, ErrNoCallItem
		}
		return nil, err
	}
	if len(items) == 0 {
		return nil, ErrNoCallItem
	}
	out := make([]CallItem, len(items))
	for i, item := range items {
		out[i] = CallItem{
			ID: i, Name: item.Name, Kind: item.Kind, Detail: item.Detail,
			URI: item.URI, Range: item.Range, SelectionRange: item.SelectionRange, Data: item.Data,
		}
	}
	return out, nil
}

// ErrNoCallItem means the server found no call hierarchy item where
// asked: a position off a symbol, or a result of none.
var ErrNoCallItem = errors.New("lsp: no call hierarchy item")

// IncomingCalls asks who calls the item. An item without fromRanges is
// kept, of a call whose site the server does not name.
func (c *Client) IncomingCalls(ctx context.Context, item CallItem) ([]CallEdge, error) {
	var calls []struct {
		From       callItemWire `json:"from"`
		FromRanges []Range      `json:"fromRanges"`
	}
	if err := c.conn.Call(ctx, "callHierarchy/incomingCalls", map[string]any{"item": item.wire()}, &calls); err != nil {
		return nil, err
	}
	return callEdges(calls, func(c struct {
		From       callItemWire `json:"from"`
		FromRanges []Range      `json:"fromRanges"`
	}) (callItemWire, []Range) {
		return c.From, c.FromRanges
	}), nil
}

// OutgoingCalls asks what the item calls.
func (c *Client) OutgoingCalls(ctx context.Context, item CallItem) ([]CallEdge, error) {
	var calls []struct {
		To         callItemWire `json:"to"`
		FromRanges []Range      `json:"fromRanges"`
	}
	if err := c.conn.Call(ctx, "callHierarchy/outgoingCalls", map[string]any{"item": item.wire()}, &calls); err != nil {
		return nil, err
	}
	return callEdges(calls, func(c struct {
		To         callItemWire `json:"to"`
		FromRanges []Range      `json:"fromRanges"`
	}) (callItemWire, []Range) {
		return c.To, c.FromRanges
	}), nil
}

// wire is the item as the protocol sends it back, with the data the
// server wants returned.
func (item CallItem) wire() callItemWire {
	return callItemWire{
		Name: item.Name, Kind: item.Kind, Detail: item.Detail,
		URI: item.URI, Range: item.Range, SelectionRange: item.SelectionRange, Data: item.Data,
	}
}

// callEdges turns the wire's incoming or outgoing calls into edges,
// giving each item an ID of its own.
func callEdges[T any](calls []T, edgeOf func(T) (callItemWire, []Range)) []CallEdge {
	out := make([]CallEdge, 0, len(calls))
	for _, call := range calls {
		w, fromRanges := edgeOf(call)
		item := CallItem{
			ID: len(out), Name: w.Name, Kind: w.Kind, Detail: w.Detail,
			URI: w.URI, Range: w.Range, SelectionRange: w.SelectionRange, Data: w.Data,
		}
		out = append(out, CallEdge{Item: item, FromRanges: fromRanges})
	}
	return out
}

// Callers returns call sites from incoming call hierarchy items,
// flattened to locations as it always did: each fromRange is one call
// site of its caller, or the caller itself when the server names none.
func (c *Client) Callers(ctx context.Context, uri string, pos Position) ([]Location, error) {
	items, err := c.PrepareCallHierarchy(ctx, uri, pos)
	if err != nil {
		if errors.Is(err, ErrNoCallItem) {
			return nil, nil
		}
		return nil, err
	}
	var locations []Location
	for _, item := range items {
		calls, err := c.IncomingCalls(ctx, item)
		if err != nil {
			return nil, err
		}
		for _, call := range calls {
			if len(call.FromRanges) == 0 {
				locations = append(locations, Location{URI: call.Item.URI, Range: call.Item.SelectionRange, Name: call.Item.Name})
			}
			for _, r := range call.FromRanges {
				locations = append(locations, Location{URI: call.Item.URI, Range: r, Name: call.Item.Name})
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

// serverConfigurations is what workspace/configuration answers, keyed
// by server id: a server missing from it gets null per item, its own
// defaults — which for gopls means no inlay hints at all. Adding an
// entry moves only that server.
var serverConfigurations = map[string]any{
	"gopls": map[string]any{
		// The hint kinds a reader asks about; the rest stay gopls's
		// default, off.
		"hints": map[string]any{
			"assignVariableTypes":   true,
			"compositeLiteralTypes": true,
			"rangeVariableTypes":    true,
			"parameterNames":        true,
		},
	},
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
		out := make([]any, len(p.Items))
		if settings, ok := serverConfigurations[h.opts.ServerID]; ok {
			for i := range out {
				out[i] = settings
			}
		}
		return out, nil // null for each item of a server with nothing to say
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
