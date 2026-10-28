package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
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

// goLSP runs at most one gopls for a workspace, started when a Go
// definition is first asked for, and keeps the documents it was given
// in step with their buffers. Its methods are safe from any goroutine.
type goLSP struct {
	root    string
	start   starter
	onState func(lspState, error) // called from any goroutine

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

func newGoLSP(root string, start starter, onState func(lspState, error)) *goLSP {
	m := &goLSP{root: root, start: start, onState: onState, docs: map[string]*lspDoc{}, out: newOutbox()}
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

func (m *goLSP) status() (lspState, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.err
}

func (m *goLSP) setState(s lspState, err error) {
	m.state, m.err = s, err
	if m.onState != nil {
		m.onState(s, err)
	}
}

// get returns the running client, starting the server the first time.
// After a failure it fails until retry.
func (m *goLSP) get(ctx context.Context) (langClient, error) {
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

func (m *goLSP) run(done chan struct{}, attempt int) {
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

func (m *goLSP) exited(attempt int, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed || attempt != m.attempt {
		return
	}
	m.client = nil
	m.docs = map[string]*lspDoc{}
	m.setState(lspFailed, fmt.Errorf("gopls stopped: %w", err))
}

// retry forgets a failure, so the next request starts the server again.
func (m *goLSP) retry() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state == lspFailed {
		m.setState(lspOff, nil)
	}
}

// sync opens a document in the server, or sends its new text if the
// buffer changed since it was last sent, and waits until it is sent.
// The bookkeeping is under mu; the text goes out on the outbox.
func (m *goLSP) sync(ctx context.Context, c langClient, uri string, snap editor.Snapshot) error {
	m.mu.Lock()
	if c != m.client {
		m.mu.Unlock()
		return errors.New("gopls restarted")
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
func (m *goLSP) queueSync(c langClient, uri string, snap editor.Snapshot) <-chan error {
	done := make(chan error, 1)
	d := m.docs[uri]
	switch {
	case d == nil:
		d = &lspDoc{version: 1, bufVersion: snap.Version()}
		m.docs[uri] = d
		m.out.push(func() { done <- c.DidOpen(uri, "go", 1, snap.String()) })
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
func (m *goLSP) changed(uri string, snap editor.Snapshot) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, open := m.docs[uri]; open && m.client != nil {
		m.queueSync(m.client, uri, snap)
	}
}

// close tells the server a document is closed.
func (m *goLSP) close(uri string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.docs[uri]; ok && m.client != nil {
		delete(m.docs, uri)
		c := m.client
		m.out.push(func() { c.DidClose(uri) })
	}
}

// stop shuts the server down, in the background; the manager is done.
func (m *goLSP) stop() {
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
func (m *goLSP) stopWait(timeout time.Duration) {
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
