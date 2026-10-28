package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	uriutil "see/internal/uri"
)

// langClient is what the app needs of a language client; *lsp.Client is
// one, and tests give their own.
type langClient interface {
	Encoding() string
	HasDefinition() bool
	DidOpen(uri, languageID string, version int, text string) error
	DidChange(uri string, version int, text string) error
	DidClose(uri string) error
	Definition(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error)
	Hover(ctx context.Context, uri string, pos lsp.Position) (string, *lsp.Range, error)
	Shutdown(timeout time.Duration)
}

type lspState int

const (
	lspOff lspState = iota
	lspStarting
	lspReady
	lspFailed
)

// errNoGopls is returned when no gopls is installed.
var errNoGopls = errors.New("gopls is not installed")

// starter starts a client for a workspace; onExit is called if the
// server goes away by itself.
type starter func(ctx context.Context, root string, onExit func(error)) (langClient, error)

// languageServer manages one server for a workspace and keeps its documents
// synchronized with editor buffers. Its methods are safe from any goroutine.
type languageServer struct {
	root     string
	name     string
	language func(string) string
	start    starter
	onState  func(lspState, error) // called from any goroutine

	mu       sync.Mutex
	state    lspState
	err      error
	client   langClient
	starting chan struct{}
	docs     map[string]*lspDoc // by URI
	closed   bool
	attempt  int // counts starts, so a late exit of an old server is ignored

	// out sends the documents' notifications, in order, on a goroutine
	// of its own: a write that blocks on a busy server never holds mu.
	out *outbox
}

type lspDoc struct {
	version    int    // the protocol's
	bufVersion uint64 // of the buffer last sent
}

func newGoLSP(root string, start starter, onState func(lspState, error)) *languageServer {
	m := &languageServer{root: root, name: "gopls", language: func(string) string { return "go" }, start: start, onState: onState, docs: map[string]*lspDoc{}, out: newOutbox()}
	go m.out.loop()
	return m
}

// outbox runs functions one after the other on its goroutine, in the
// order they were pushed; pushing never blocks.
type outbox struct {
	mu   sync.Mutex
	q    []func()
	wake chan struct{}
	quit chan struct{}
	once sync.Once
}

func newOutbox() *outbox {
	return &outbox{wake: make(chan struct{}, 1), quit: make(chan struct{})}
}

func (o *outbox) push(fn func()) {
	o.mu.Lock()
	o.q = append(o.q, fn)
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

func (o *outbox) loop() {
	for {
		o.mu.Lock()
		q := o.q
		o.q = nil
		o.mu.Unlock()
		for _, fn := range q {
			select {
			case <-o.quit:
				return
			default:
			}
			fn()
		}
		select {
		case <-o.wake:
		case <-o.quit:
			return
		}
	}
}

// stop ends the loop; what is still queued is dropped.
func (o *outbox) stop() { o.once.Do(func() { close(o.quit) }) }

func (m *languageServer) status() (lspState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.err
}

func (m *languageServer) setState(s lspState, err error) {
	m.state, m.err = s, err
	if m.onState != nil {
		m.onState(s, err)
	}
}

// get returns the running client, starting the server the first time.
// After a failure it fails until retry.
func (m *languageServer) get(ctx context.Context) (langClient, error) {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil, context.Canceled
	}
	switch m.state {
	case lspReady:
		c := m.client
		m.mu.Unlock()
		return c, nil
	case lspFailed:
		err := m.err
		m.mu.Unlock()
		return nil, err
	case lspOff:
		m.starting = make(chan struct{})
		m.attempt++
		m.setState(lspStarting, nil)
		go m.run(m.starting, m.attempt)
	}
	wait := m.starting
	m.mu.Unlock()
	select {
	case <-wait:
		return m.get(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (m *languageServer) run(done chan struct{}, attempt int) {
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := m.start(ctx, m.root, func(err error) { m.exited(attempt, err) })
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		if c != nil {
			go c.Shutdown(3 * time.Second)
		}
		return
	}
	if err != nil {
		m.setState(lspFailed, err)
		return
	}
	if m.state == lspFailed {
		return // it exited as soon as it started
	}
	if !c.HasDefinition() {
		go c.Shutdown(3 * time.Second)
		m.setState(lspFailed, errors.New("the language server does not support Go to Definition"))
		return
	}
	m.client = c
	m.docs = map[string]*lspDoc{}
	m.setState(lspReady, nil)
}

func (m *languageServer) exited(attempt int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || attempt != m.attempt {
		return
	}
	m.client = nil
	m.docs = map[string]*lspDoc{}
	m.setState(lspFailed, fmt.Errorf("%s stopped: %w", m.name, err))
}

// retry forgets a failure, so the next request starts the server again.
func (m *languageServer) retry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == lspFailed {
		m.setState(lspOff, nil)
	}
}

// sync opens a document in the server, or sends its new text if the
// buffer changed since it was last sent, and waits until it is sent.
// The bookkeeping is under mu; the text goes out on the outbox.
func (m *languageServer) sync(ctx context.Context, c langClient, uri string, snap editor.Snapshot) error {
	m.mu.Lock()
	if c != m.client {
		m.mu.Unlock()
		return fmt.Errorf("%s restarted", m.name)
	}
	send := m.queueSync(c, uri, snap)
	m.mu.Unlock()
	select {
	case err := <-send:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// queueSync queues what brings the server's copy of a document up to
// snap, with mu held so that versions go out in the order they are
// numbered. The channel gets the result of sending it.
func (m *languageServer) queueSync(c langClient, uri string, snap editor.Snapshot) <-chan error {
	done := make(chan error, 1)
	d := m.docs[uri]
	switch {
	case d == nil:
		d = &lspDoc{version: 1, bufVersion: snap.Version()}
		m.docs[uri] = d
		path, _ := uriutil.ToPath(uri)
		language := m.language(path)
		m.out.push(func() { done <- c.DidOpen(uri, language, 1, snap.String()) })
	case d.bufVersion == snap.Version():
		done <- nil
	default:
		d.version++
		d.bufVersion = snap.Version()
		v := d.version
		m.out.push(func() { done <- c.DidChange(uri, v, snap.String()) })
	}
	return done
}

// changed sends a document's new text if the server has it open.
func (m *languageServer) changed(uri string, snap editor.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, open := m.docs[uri]; open && m.client != nil {
		m.queueSync(m.client, uri, snap)
	}
}

// close tells the server a document is closed.
func (m *languageServer) close(uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[uri]; ok && m.client != nil {
		delete(m.docs, uri)
		c := m.client
		m.out.push(func() { c.DidClose(uri) })
	}
}

// stop shuts the server down, in the background; the manager is done.
func (m *languageServer) stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	m.out.stop()
	if c := m.client; c != nil {
		m.client = nil
		go c.Shutdown(3 * time.Second)
	}
}

// stopWait shuts the server down and waits, as the app quits.
func (m *languageServer) stopWait(timeout time.Duration) {
	m.mu.Lock()
	m.closed = true
	m.out.stop()
	c := m.client
	m.client = nil
	m.mu.Unlock()
	if c != nil {
		c.Shutdown(timeout)
	}
}

// findGopls finds gopls: $SEE_GOPLS (or the legacy $NSR_GOPLS), then PATH, then Go's bin
// directories. A gopls inside the workspace is never used.
func findGopls(root string) (string, error) {
	var candidates []string
	p, ok := os.LookupEnv("SEE_GOPLS")
	if !ok {
		p = os.Getenv("NSR_GOPLS")
	}
	if p != "" {
		candidates = append(candidates, p)
	}
	if p, err := exec.LookPath("gopls"); err == nil {
		candidates = append(candidates, p)
	}
	exe := "gopls"
	if runtime.GOOS == "windows" {
		exe = "gopls.exe"
	}
	if p := os.Getenv("GOBIN"); p != "" {
		candidates = append(candidates, filepath.Join(p, exe))
	}
	if p := os.Getenv("GOPATH"); p != "" {
		for _, dir := range filepath.SplitList(p) {
			candidates = append(candidates, filepath.Join(dir, "bin", exe))
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		candidates = append(candidates, filepath.Join(home, "go", "bin", exe))
	}
	for _, p := range candidates {
		if !filepath.IsAbs(p) {
			continue
		}
		if root != "" && within(root, p) {
			continue
		}
		if fi, err := os.Stat(p); err == nil && fi.Mode().IsRegular() {
			return p, nil
		}
	}
	return "", errNoGopls
}

// startGopls is the starter of the real gopls.
func startGopls(logf func(string, ...any)) starter {
	return func(ctx context.Context, root string, onExit func(error)) (langClient, error) {
		path, err := findGopls(root)
		if err != nil {
			return nil, err
		}
		c, err := lsp.Start(ctx, path, nil, lsp.Options{
			RootPath: root,
			RootURI:  uriOf(root),
			Name:     filepath.Base(root),
			Logf:     logf,
			OnExit:   onExit,
		})
		if err != nil {
			return nil, err
		}
		return c, nil
	}
}

var errNoTypeScriptServer = errors.New("typescript-language-server is not installed")

func typeScriptLanguage(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".ts":
		return "typescript"
	case ".tsx":
		return "typescriptreact"
	case ".js", ".mjs", ".cjs":
		return "javascript"
	case ".jsx":
		return "javascriptreact"
	}
	return ""
}

func findTypeScriptServer(root string) (string, error) {
	p := os.Getenv("SEE_TYPESCRIPT_LANGUAGE_SERVER")
	if p == "" {
		p, _ = exec.LookPath("typescript-language-server")
	}
	if !filepath.IsAbs(p) {
		return "", errNoTypeScriptServer
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", errNoTypeScriptServer
	}
	realRoot := root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		realRoot = r
	}
	if root != "" && (within(root, p) || within(realRoot, resolved)) {
		return "", errNoTypeScriptServer
	}
	if fi, err := os.Stat(resolved); err != nil || !fi.Mode().IsRegular() {
		return "", errNoTypeScriptServer
	}
	return p, nil
}

func startTypeScriptServer(logf func(string, ...any)) starter {
	return func(ctx context.Context, root string, onExit func(error)) (langClient, error) {
		path, err := findTypeScriptServer(root)
		if err != nil {
			return nil, err
		}
		return lsp.Start(ctx, path, []string{"--stdio"}, lsp.Options{RootPath: root, RootURI: uriOf(root), Name: filepath.Base(root), Logf: logf, OnExit: onExit})
	}
}
