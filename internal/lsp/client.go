package lsp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"see/internal/operr"
	"see/internal/proc"
)

// WorkspaceFolder is one root a server knows.
type WorkspaceFolder struct {
	URI, Name string
}

// Options configure a Client.
type Options struct {
	// RootPath is the workspace's directory, RootURI its URI.
	RootPath, RootURI string
	// WorkspaceFolders are every root in the workspace. Empty uses RootURI.
	WorkspaceFolders []WorkspaceFolder
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

func workspaceFolderParams(opts Options) []map[string]any {
	folders := opts.WorkspaceFolders
	if len(folders) == 0 {
		folders = []WorkspaceFolder{{URI: opts.RootURI, Name: opts.Name}}
	}
	out := make([]map[string]any, len(folders))
	for i, folder := range folders {
		out[i] = map[string]any{"uri": folder.URI, "name": folder.Name}
	}
	return out
}

// Client is a language client of one server: it initializes it, keeps
// its documents in sync, asks for definitions and shuts it down.
type Client struct {
	conn                *Conn
	opts                Options
	encoding            string
	sync                int
	openNote            bool
	defs                bool
	documentHighlights  bool
	implementations     bool
	typeDefinitions     bool
	inlayHints          bool
	documentLinks       bool
	documentLinkResolve bool
	callHierarchy       bool
	typeHierarchy       bool
	selectionRanges     bool
	pullDiagnostics     bool
	foldingRange        bool
	signatureHelp       bool
	completion          bool
	completionResolve   bool
	semanticFull        bool
	semanticRange       bool
	semanticLegend      semanticTokensLegend
	codeActions         codeActionSupport
	hasCodeActions      bool
	commands            []string
	operations          map[string]bool
	hasRename           bool
	hasPrepareRename    bool
	documentFormat      bool
	documentRangeFormat bool
	diagMu              sync.Mutex
	onDiagnostics       func(string, *int, []Diagnostic)
	earlyDiagnostics    map[string]publishedDiagnostics
	applyEdit           func(*WorkspaceEdit) error

	cmd    *exec.Cmd
	stdin  io.Closer
	abort  chan struct{}
	stdout io.Closer

	mu       sync.Mutex
	stopping bool
	exited   chan struct{}
}

// Start runs the server command with stdin and stdout for the protocol
// and stderr kept apart, then initializes it.
func Start(ctx context.Context, command string, args []string, opts Options) (*Client, error) {
	launchError := func(err error) error {
		e := operr.Classify(err)
		e.Operation = "start language server"
		e.Finality = operr.NoEffect
		e.Attempt = 1
		return e
	}
	if err := ctx.Err(); err != nil {
		return nil, launchError(err)
	}
	cmd := proc.Command(command, args...)
	if cmd.Err != nil {
		return nil, launchError(cmd.Err)
	}
	if err := proc.ValidateExecutable(opts.RootPath, cmd.Path); err != nil {
		return nil, launchError(err)
	}
	configureServer(cmd)
	cmd.Dir = opts.RootPath
	cmd.Env = mergeEnv(os.Environ(), opts.Env)
	// A server's stderr is untrusted content: it is redacted against the
	// environment it runs with and bounded, then attached to exit errors
	// instead of streamed to the log. Capturing it pipes through Wait, so
	// WaitDelay bounds how long a descendant holding the pipe can stall
	// reaping, as it does for the protocol pipes below.
	stderr := proc.NewTail(stderrDigestMax, cmd.Env)
	cmd.Stderr = stderr
	cmd.WaitDelay = 500 * time.Millisecond
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, launchError(err)
	}
	// Own the read end: cmd.Wait must not close it before the last reply is read.
	stdout, childStdout, err := os.Pipe()
	if err != nil {
		_ = stdin.Close()
		return nil, launchError(err)
	}
	cmd.Stdout = childStdout
	if err := cmd.Start(); err != nil {
		_ = stdout.Close()
		_ = childStdout.Close()
		return nil, launchError(err)
	}
	_ = childStdout.Close()
	c := newClient(stdin, stdout, opts)
	c.cmd, c.stdin, c.stdout, c.abort = cmd, stdin, stdout, make(chan struct{})
	waited := make(chan error, 1)
	go func() { waited <- cmd.Wait() }()
	go func() {
		var err error
		select {
		case err = <-waited:
			killServer(cmd) // descendants may still hold the protocol pipes
		case <-c.conn.Done():
			killServer(cmd)
			err = <-waited
		case <-c.abort:
			killServer(cmd)
			err = <-waited
		}
		// Drain bytes already written, but never wait indefinitely for EOF
		// from a descendant outside our process group.
		timer := time.NewTimer(500 * time.Millisecond)
		select {
		case <-c.conn.Done():
		case <-timer.C:
		}
		timer.Stop()
		_ = stdout.Close()
		if err == nil {
			err = c.conn.Err()
		}
		c.finish(withStderr(err, stderr))
	}()
	if err := c.initialize(ctx); err != nil {
		c.Kill()
		<-c.exited
		if digest := stderr.String(); digest != "" {
			err = fmt.Errorf("%w (server stderr: %s)", err, digest)
		}
		return nil, err
	}
	return c, nil
}

// stderrDigestMax bounds the server stderr kept for error reporting.
const stderrDigestMax = 4 << 10

// withStderr appends the redacted stderr tail to a non-nil exit error, so
// a crash report shows why the server died. The digest is bounded and
// secret-free, but it is still server-controlled text.
func withStderr(err error, stderr *proc.Tail) error {
	if err == nil {
		return nil
	}
	if digest := stderr.String(); digest != "" {
		return fmt.Errorf("%w (server stderr: %s)", err, digest)
	}
	return err
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
	c.stdout, _ = r.(io.Closer)
	go func() {
		<-c.conn.Done()
		c.finish(c.conn.Err())
	}()
	if err := c.initialize(ctx); err != nil {
		c.Kill()
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

// HasDocumentLinks reports whether the server provides document links.
func (c *Client) HasDocumentLinks() bool { return c.documentLinks }

// DocumentLinkResolveSupport reports whether the server fills a link's
// target on documentLink/resolve; its links arrive without one.
func (c *Client) DocumentLinkResolveSupport() bool { return c.documentLinkResolve }

// HasSignatureHelp reports whether the server answers signature help.
func (c *Client) HasSignatureHelp() bool         { return c.signatureHelp }
func (c *Client) HasCompletion() bool            { return c.completion }
func (c *Client) HasCompletionItemResolve() bool { return c.completionResolve }

// HasFoldingRange reports whether the server answers folding range
// requests.
func (c *Client) HasFoldingRange() bool { return c.foldingRange }

// HasCallHierarchy reports whether the server answers call hierarchy
// requests, either direction.
func (c *Client) HasCallHierarchy() bool { return c.callHierarchy }

// HasSelectionRanges reports whether the server provides selection
// ranges, as gopls does: the syntax nodes a position's selection grows
// through.
func (c *Client) HasSelectionRanges() bool { return c.selectionRanges }

// HasSemanticTokens reports whether the server answers semantic tokens
// requests, whole-document or for a range.
func (c *Client) HasSemanticTokens() bool { return c.semanticFull || c.semanticRange }

// SemanticTokensFull and SemanticTokensRange report which of the two
// semantic tokens requests the server answers.
func (c *Client) SemanticTokensFull() bool  { return c.semanticFull }
func (c *Client) SemanticTokensRange() bool { return c.semanticRange }

// SemanticTokenTypes and SemanticTokenModifiers are the server's
// legend: the names its token stream indexes, in its order.
func (c *Client) SemanticTokenTypes() []string { return slices.Clone(c.semanticLegend.TokenTypes) }
func (c *Client) SemanticTokenModifiers() []string {
	return slices.Clone(c.semanticLegend.TokenModifiers)
}

// HasCodeActions reports whether the server offers code actions.
func (c *Client) HasCodeActions() bool { return c.hasCodeActions }

// CodeActionKinds is the kinds the server named in its capabilities,
// empty when it made no such promise.
func (c *Client) CodeActionKinds() []string { return slices.Clone(c.codeActions.kinds) }

// HasExecuteCommand reports whether the server runs commands, as a
// code action's Command asks for.
func (c *Client) HasExecuteCommand() bool { return len(c.commands) > 0 }

// HasRename reports whether the server renames symbols. HasPrepareRename
// says whether it answers textDocument/prepareRename first.
func (c *Client) HasRename() bool        { return c.hasRename }
func (c *Client) HasPrepareRename() bool { return c.hasPrepareRename }

// ErrNoRename is a prepareRename answer that the position cannot be
// renamed: a result of null, or a range the request cannot read.
var ErrNoRename = errors.New("lsp: no rename at position")

// PrepareRename asks whether the symbol at pos can be renamed, and
// answers the range and placeholder the prompt should use. A null
// result becomes ErrNoRename; a literal that is not a range or
// defaultBehavior is one too.
func (c *Client) PrepareRename(ctx context.Context, uri string, pos Position) (*PrepareRenameResult, error) {
	var raw json.RawMessage
	if err := c.call(ctx, "textDocument/prepareRename", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}, &raw); err != nil {
		return nil, err
	}
	res, err := ParsePrepareRename(raw)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, ErrNoRename
	}
	return res, nil
}

// Rename renames the symbol at pos to newName, answering the edits the
// server wants made. A result of null or an empty edit is a no-op.
func (c *Client) Rename(ctx context.Context, uri string, pos Position, newName string) (*WorkspaceEdit, error) {
	var edit WorkspaceEdit
	if err := c.call(ctx, "textDocument/rename", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
		"newName":      newName,
	}, &edit); err != nil {
		return nil, err
	}
	return &edit, nil
}

// HasDocumentFormatting reports whether the server answers
// textDocument/formatting, for the whole document.
func (c *Client) HasDocumentFormatting() bool { return c.documentFormat }

// HasDocumentRangeFormatting reports whether the server answers
// textDocument/rangeFormatting, for a selection's range.
func (c *Client) HasDocumentRangeFormatting() bool { return c.documentRangeFormat }

// SetApplyEditHandler sets what answers a server's workspace/applyEdit
// request: it applies the edit and reports failure as the request's
// error. Until one is set the request is refused, as a client that
// advertises no applyEdit should.
func (c *Client) SetApplyEditHandler(fn func(*WorkspaceEdit) error) {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	c.applyEdit = fn
}

// WorkspaceSymbols searches declarations across the workspace.
type WorkspaceSymbol struct {
	Name     string   `json:"name"`
	Location Location `json:"location"`
}

func (c *Client) WorkspaceSymbols(ctx context.Context, query string) ([]WorkspaceSymbol, error) {
	var symbols []WorkspaceSymbol
	if err := c.call(ctx, "workspace/symbol", map[string]any{"query": query}, &symbols); err != nil {
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
		"processId":        os.Getpid(),
		"clientInfo":       map[string]any{"name": "See", "version": "0.1"},
		"rootUri":          c.opts.RootURI,
		"rootPath":         c.opts.RootPath,
		"workspaceFolders": workspaceFolderParams(c.opts),
		"capabilities": map[string]any{
			"general": map[string]any{
				"positionEncodings": []string{UTF8, UTF16},
			},
			"textDocument": map[string]any{
				"synchronization":   map[string]any{"didSave": false, "dynamicRegistration": false},
				"documentSymbol":    map[string]any{"hierarchicalDocumentSymbolSupport": true, "dynamicRegistration": false},
				"references":        map[string]any{"dynamicRegistration": false},
				"callHierarchy":     map[string]any{"dynamicRegistration": false},
				"typeHierarchy":     map[string]any{"dynamicRegistration": false},
				"definition":        map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"documentHighlight": map[string]any{"dynamicRegistration": false},
				"implementation":    map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"typeDefinition":    map[string]any{"linkSupport": true, "dynamicRegistration": false},
				"inlayHint":         map[string]any{},
				"foldingRange":      map[string]any{"lineFoldingOnly": true, "dynamicRegistration": false},
				"documentLink":      map[string]any{"tooltipSupport": true, "dynamicRegistration": false},
				"selectionRange":    map[string]any{"dynamicRegistration": false},
				"hover":             map[string]any{"contentFormat": []string{"markdown", "plaintext"}, "dynamicRegistration": false},
				"signatureHelp":     map[string]any{"signatureInformation": map[string]any{"documentationFormat": []string{"markdown", "plaintext"}, "parameterInformation": map[string]any{"labelOffsetSupport": true}}, "dynamicRegistration": false},
				"completion": map[string]any{"completionItem": map[string]any{
					"documentationFormat": []string{"markdown", "plaintext"},
					"labelDetailsSupport": true,
					"dataSupport":         true,
					"resolveSupport":      map[string]any{"properties": []string{"detail", "documentation"}},
				}, "dynamicRegistration": false},
				"semanticTokens": map[string]any{
					"dynamicRegistration":     false,
					"requests":                map[string]any{"range": true, "full": map[string]any{"delta": false}},
					"tokenTypes":              standardSemanticTokenTypes,
					"tokenModifiers":          standardSemanticTokenModifiers,
					"formats":                 []string{"relative"},
					"overlappingTokenSupport": false,
					"multilineTokenSupport":   false,
				},
				"codeAction":      map[string]any{"codeActionLiteralSupport": map[string]any{"codeActionKind": map[string]any{"valueSet": []string{"", "quickfix", "refactor", "refactor.extract", "refactor.inline", "refactor.rewrite", "source", "source.organizeImports"}}}, "dataSupport": true, "resolveSupport": map[string]any{"properties": []string{"edit", "command"}}, "dynamicRegistration": false},
				"rename":          map[string]any{"prepareSupport": true, "dynamicRegistration": false},
				"formatting":      map[string]any{"dynamicRegistration": false},
				"rangeFormatting": map[string]any{"dynamicRegistration": false},
			},
			"workspace": map[string]any{
				"workspaceFolders": true,
				"configuration":    true,
				"applyEdit":        true,
				"workspaceEdit": map[string]any{
					"documentChanges": true,
				},
				"executeCommand": map[string]any{"dynamicRegistration": false},
			},
			"window": map[string]any{"workDoneProgress": false},
		},
	}
	if c.opts.InitializationOptions != nil {
		params["initializationOptions"] = c.opts.InitializationOptions
	}
	var raw json.RawMessage
	if err := c.conn.Call(ctx, "initialize", params, &raw); err != nil {
		return fmt.Errorf("initialize: %w", err)
	}
	var res struct {
		Capabilities *serverCapabilities `json:"capabilities"`
	}
	if json.Unmarshal(raw, &res) != nil || res.Capabilities == nil {
		return fmt.Errorf("initialize: invalid capabilities: %w", operr.New(operr.InvalidInput, operr.Permanent, nil))
	}
	// UTF-16 is the protocol default only when positionEncoding was omitted.
	c.encoding = UTF16
	if encoding := res.Capabilities.PositionEncoding; encoding != nil {
		if json.Unmarshal(encoding, &c.encoding) != nil || (c.encoding != UTF8 && c.encoding != UTF16) || string(encoding) == "null" {
			return fmt.Errorf("initialize: unsupported position encoding: %w", unsupported("initialize"))
		}
	}
	c.sync, c.openNote = res.Capabilities.syncKind()
	c.defs = res.Capabilities.hasDefinition()
	c.documentHighlights = res.Capabilities.hasDocumentHighlights()
	c.implementations = (serverCapabilities{DefinitionProvider: res.Capabilities.ImplementationProvider}).hasDefinition()
	c.typeDefinitions = (serverCapabilities{DefinitionProvider: res.Capabilities.TypeDefinitionProvider}).hasDefinition()
	c.inlayHints = res.Capabilities.hasInlayHints()
	c.documentLinks = res.Capabilities.hasDocumentLinks()
	c.documentLinkResolve = res.Capabilities.documentLinkResolve()
	c.signatureHelp = res.Capabilities.hasSignatureHelp()
	c.completion = res.Capabilities.hasCompletion()
	c.completionResolve = res.Capabilities.completionItemResolve()
	c.callHierarchy = res.Capabilities.hasCallHierarchy()
	c.typeHierarchy = providerEnabled(res.Capabilities.TypeHierarchyProvider)
	c.selectionRanges = res.Capabilities.hasSelectionRanges()
	c.codeActions, c.hasCodeActions = res.Capabilities.codeActions()
	var cmds struct {
		Commands []string `json:"commands"`
	}
	if json.Unmarshal(res.Capabilities.ExecuteCommandProvider, &cmds) == nil {
		c.commands = cmds.Commands
	}
	c.hasRename, c.hasPrepareRename = res.Capabilities.hasRename()
	c.documentFormat = res.Capabilities.hasDocumentFormatting()
	c.documentRangeFormat = res.Capabilities.hasDocumentRangeFormatting()
	c.pullDiagnostics = providerEnabled(res.Capabilities.DiagnosticProvider)
	c.foldingRange = res.Capabilities.hasFoldingRange()
	c.semanticLegend, c.semanticFull, c.semanticRange = res.Capabilities.semanticTokens()
	c.operations = map[string]bool{
		"textDocument/definition":           c.defs,
		"textDocument/documentHighlight":    c.documentHighlights,
		"textDocument/implementation":       c.implementations,
		"textDocument/typeDefinition":       c.typeDefinitions,
		"textDocument/inlayHint":            c.inlayHints,
		"textDocument/documentLink":         c.documentLinks,
		"textDocument/prepareCallHierarchy": c.callHierarchy,
		"textDocument/prepareTypeHierarchy": c.typeHierarchy,
		"textDocument/selectionRange":       c.selectionRanges,
		"textDocument/diagnostic":           c.pullDiagnostics,
		"textDocument/foldingRange":         c.foldingRange,
		"textDocument/signatureHelp":        c.signatureHelp,
		"textDocument/completion":           c.completion,
		"textDocument/semanticTokens/full":  c.semanticFull,
		"textDocument/semanticTokens/range": c.semanticRange,
		"textDocument/codeAction":           c.hasCodeActions,
		"textDocument/rename":               c.hasRename,
		"textDocument/prepareRename":        c.hasPrepareRename,
		"textDocument/formatting":           c.documentFormat,
		"textDocument/rangeFormatting":      c.documentRangeFormat,
		"documentLink/resolve":              c.documentLinkResolve,
		"completionItem/resolve":            c.completionResolve,
		"callHierarchy/incomingCalls":       c.callHierarchy,
		"callHierarchy/outgoingCalls":       c.callHierarchy,
		"typeHierarchy/supertypes":          c.typeHierarchy,
		"typeHierarchy/subtypes":            c.typeHierarchy,
		"codeAction/resolve":                c.codeActions.resolve,
		"workspace/executeCommand":          c.HasExecuteCommand(),
		"textDocument/hover":                providerEnabled(res.Capabilities.HoverProvider),
		"textDocument/references":           providerEnabled(res.Capabilities.ReferencesProvider),
		"textDocument/documentSymbol":       providerEnabled(res.Capabilities.DocumentSymbolProvider),
		"workspace/symbol":                  providerEnabled(res.Capabilities.WorkspaceSymbolProvider),
	}
	c.logf("effective LSP operations: %v", c.Operations())
	return c.conn.writeContext(ctx, map[string]any{"jsonrpc": "2.0", "method": "initialized", "params": map[string]any{}})
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
	err := c.call(ctx, "textDocument/documentHighlight", map[string]any{
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
	err := c.call(ctx, method, map[string]any{
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
	err := c.call(ctx, "textDocument/hover", map[string]any{
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
	err := c.call(ctx, "textDocument/inlayHint", map[string]any{
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
	default:
		return nil, unsupported("textDocument/semanticTokens")
	}
	var result SemanticTokensData
	if err := c.call(ctx, method, params, &result); err != nil {
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

// DocumentLinks asks for the links of the whole document: its import
// paths, the URLs of its comments and the files its directives name.
// Where the server resolves lazily, a link's Target is empty until
// DocumentLinkResolve fills it.
func (c *Client) DocumentLinks(ctx context.Context, uri string) ([]DocumentLink, error) {
	var raw json.RawMessage
	err := c.call(ctx, "textDocument/documentLink", map[string]any{
		"textDocument": map[string]any{"uri": uri},
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseDocumentLinks(raw)
}

// DocumentLinkResolve asks the server to fill link's Target, for a link
// whose answer left it empty. link's Data goes back to the server, as
// the protocol wants.
func (c *Client) DocumentLinkResolve(ctx context.Context, link *DocumentLink) (*DocumentLink, error) {
	var resolved DocumentLink
	err := c.call(ctx, "documentLink/resolve", link, &resolved)
	if err != nil {
		return nil, err
	}
	return &resolved, nil
}

// SelectionRanges asks for the nested selections of each position of
// positions, one chain a position, innermost first.
func (c *Client) SelectionRanges(ctx context.Context, uri string, positions []Position) ([]*SelectionRange, error) {
	var raw json.RawMessage
	err := c.call(ctx, "textDocument/selectionRange", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"positions":    positions,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseSelectionRanges(raw)
}

// SignatureHelp asks for the signatures of the call at pos.
func (c *Client) SignatureHelp(ctx context.Context, uri string, pos Position) (*SignatureHelp, error) {
	var raw json.RawMessage
	err := c.call(ctx, "textDocument/signatureHelp", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseSignatureHelp(raw)
}

func (c *Client) Completion(ctx context.Context, uri string, pos Position) (*CompletionResult, error) {
	var raw json.RawMessage
	err := c.call(ctx, "textDocument/completion", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"position":     pos,
	}, &raw)
	if err != nil {
		return nil, err
	}
	return ParseCompletion(raw)
}

func (c *Client) CompletionItemResolve(ctx context.Context, item CompletionItem) (*CompletionItem, error) {
	if !c.completionResolve {
		return nil, unsupported("completionItem/resolve")
	}
	if len(item.Raw) == 0 {
		return nil, fmt.Errorf("lsp: completion item has no raw payload")
	}
	var raw json.RawMessage
	if err := c.call(ctx, "completionItem/resolve", item.Raw, &raw); err != nil {
		return nil, err
	}
	return ParseCompletionItem(raw)
}

// SetDiagnosticsHandler receives published diagnostics from the server.
func (c *Client) SetDiagnosticsHandler(fn func(string, *int, []Diagnostic)) {
	c.diagMu.Lock()
	defer c.diagMu.Unlock()
	c.onDiagnostics = fn
	if fn != nil {
		for _, p := range c.earlyDiagnostics {
			fn(p.URI, p.Version, p.Diagnostics)
		}
	}
	c.earlyDiagnostics = nil
}

type publishedDiagnostics struct {
	URI         string       `json:"uri"`
	Version     *int         `json:"version"`
	Diagnostics []Diagnostic `json:"diagnostics"`
}

func (c *Client) DidChangeWatchedFiles(changes []FileChange) error {
	return c.conn.Notify("workspace/didChangeWatchedFiles", map[string]any{"changes": changes})
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
	err := c.call(ctx, "textDocument/diagnostic", map[string]any{"textDocument": map[string]any{"uri": uri}}, &result)
	return result.Items, result.Kind == "full", err
}

// CodeActions asks which fixes and refactorings the server offers in
// the range of the document, given the diagnostics published there.
func (c *Client) CodeActions(ctx context.Context, uri string, r Range, diagnostics []Diagnostic) ([]CodeAction, error) {
	var actions []CodeAction
	err := c.call(ctx, "textDocument/codeAction", map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        r,
		"context":      map[string]any{"diagnostics": diagnostics},
	}, &actions)
	return actions, err
}

// ResolveCodeAction fills in an action that came with neither edit nor
// command. The action goes back as the server sent it; what it answers
// replaces the field it filled, the edit or the command.
func (c *Client) ResolveCodeAction(ctx context.Context, action CodeAction) (*CodeAction, error) {
	var resolved CodeAction
	err := c.call(ctx, "codeAction/resolve", action, &resolved)
	if err != nil {
		return nil, err
	}
	if resolved.Edit == nil {
		resolved.Edit = action.Edit
	}
	if resolved.Command == nil {
		resolved.Command = action.Command
	}
	return &resolved, nil
}

// ExecuteCommand runs the command of a code action on the server.
func (c *Client) ExecuteCommand(ctx context.Context, command Command) error {
	if !c.SupportsCommand(command.Command) {
		return unsupported("workspace/executeCommand")
	}
	params := map[string]any{"command": command.Command}
	if len(command.Arguments) > 0 {
		params["arguments"] = command.Arguments
	}
	return c.call(ctx, "workspace/executeCommand", params, nil)
}

// DocumentFormatting asks for the edits that format the whole
// document, as gopls's gofmt pass gives.
func (c *Client) DocumentFormatting(ctx context.Context, uri string, opts FormattingOptions) ([]TextEdit, error) {
	return c.formatting(ctx, "textDocument/formatting", uri, nil, opts)
}

// DocumentRangeFormatting asks for the edits that format r of the
// document, for the lines a selection covers.
func (c *Client) DocumentRangeFormatting(ctx context.Context, uri string, r Range, opts FormattingOptions) ([]TextEdit, error) {
	return c.formatting(ctx, "textDocument/rangeFormatting", uri, &r, opts)
}

func (c *Client) formatting(ctx context.Context, method, uri string, r *Range, opts FormattingOptions) ([]TextEdit, error) {
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"options":      opts,
	}
	if r != nil {
		params["range"] = *r
	}
	var raw json.RawMessage
	if err := c.call(ctx, method, params, &raw); err != nil {
		return nil, err
	}
	return ParseTextEdits(raw)
}

// References returns locations that refer to a symbol.
func (c *Client) References(ctx context.Context, uri string, pos Position) ([]Location, error) {
	var result []Location
	err := c.call(ctx, "textDocument/references", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos, "context": map[string]any{"includeDeclaration": false}}, &result)
	return result, err
}

// PrepareCallHierarchy asks which symbols the hierarchy at pos of the
// document can grow from. Servers that cannot place one at pos answer
// an error rather than an empty result, which ErrNoCallItem wraps, so
// the caller can tell "nothing here" apart from a failure.
func (c *Client) PrepareCallHierarchy(ctx context.Context, uri string, pos Position) ([]CallItem, error) {
	var items []callItemWire
	if err := c.call(ctx, "textDocument/prepareCallHierarchy", map[string]any{"textDocument": map[string]any{"uri": uri}, "position": pos}, &items); err != nil {
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
	if err := c.call(ctx, "callHierarchy/incomingCalls", map[string]any{"item": item.wire()}, &calls); err != nil {
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
	if err := c.call(ctx, "callHierarchy/outgoingCalls", map[string]any{"item": item.wire()}, &calls); err != nil {
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
	if c.cmd != nil {
		select {
		case <-c.abort:
		default:
			close(c.abort)
		}
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.stdout != nil {
		_ = c.stdout.Close()
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
		return workspaceFolderParams(h.opts), nil
	case "window/workDoneProgress/create", "window/showMessageRequest":
		return nil, nil
	case "workspace/applyEdit":
		// A server edits the workspace this way after workspace/executeCommand
		// (a rename done by a command, say). The app applies it; failure
		// answers the request with an error and false.
		var p struct {
			Edit WorkspaceEdit `json:"edit"`
		}
		if err := json.Unmarshal(params, &p); err != nil {
			return nil, &Error{Code: CodeInvalidParams, Message: err.Error()}
		}
		h.diagMu.Lock()
		fn := h.applyEdit
		h.diagMu.Unlock()
		if fn == nil {
			return nil, &Error{Code: CodeMethodNotFound, Message: "no applyEdit handler"}
		}
		applied := true
		if err := fn(&p.Edit); err != nil {
			applied = false
		}
		return map[string]any{"applied": applied}, nil
	}
	return nil, &Error{Code: CodeMethodNotFound, Message: "method not found"}
}

func (h *handler) Notify(method string, params json.RawMessage) {
	switch method {
	case "textDocument/publishDiagnostics":
		var p publishedDiagnostics
		if json.Unmarshal(params, &p) == nil {
			h.diagMu.Lock()
			fn := h.onDiagnostics
			if fn == nil {
				if h.earlyDiagnostics == nil {
					h.earlyDiagnostics = make(map[string]publishedDiagnostics)
				}
				h.earlyDiagnostics[p.URI] = p
			}
			h.diagMu.Unlock()
			if fn != nil {
				fn(p.URI, p.Version, p.Diagnostics)
			}
		}
	case "window/logMessage", "window/showMessage":
		var p struct {
			Type int `json:"type"`
		}
		if json.Unmarshal(params, &p) == nil && p.Type <= 2 { // errors and warnings
			(*Client)(h).logf("lsp: server message (severity %d)", p.Type)
		}
	}
}
