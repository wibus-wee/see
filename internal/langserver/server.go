// Package langserver runs a language server for a workspace: it starts the
// server on the first request, keeps the documents it has open in step
// with the editor's buffers, and reports its state and diagnostics.
package langserver

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"
	"see/internal/uri"
)

// Client is what a Server needs of a language client; *lsp.Client is one,
// and tests give their own.
type Client interface {
	Encoding() string
	HasDefinition() bool
	DidOpen(uri, languageID string, version int, text string) error
	DidChange(uri string, version int, text string) error
	DidClose(uri string) error
	Definition(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error)
	Hover(ctx context.Context, uri string, pos lsp.Position) (string, *lsp.Range, error)
	Shutdown(timeout time.Duration)
}

// State is where a server is in its life.
type State int

const (
	Off State = iota
	Starting
	Ready
	Failed
)

// Starter starts a client for a workspace; onExit is called if the
// server goes away by itself.
type Starter func(ctx context.Context, root string, onExit func(error)) (Client, error)

// Options configure a Server.
type Options struct {
	Name     string // as errors and logs call it, such as "gopls"
	Root     string
	Start    Starter
	Language func(path string) string // the language ID of a document; nil is "go"

	// OnState is called, from any goroutine, as the state changes.
	OnState func(State, error)
	// OnDiagnostics is called, from any goroutine, with the diagnostics
	// of a document at the snapshot they are of.
	OnDiagnostics func(s *Server, uri string, snap editor.Snapshot, ds []lsp.Diagnostic, encoding string)
	Logf          func(string, ...any)
	// Recover, when the server has failed with err, reports whether it
	// may start now, as when the missing server has been installed since.
	// Get then starts it again, once.
	Recover func(err error) bool
}

// Server manages one server for a workspace and keeps its documents
// synchronized with editor buffers. Its methods are safe from any goroutine.
type Server struct {
	o Options

	mu       sync.Mutex
	state    State
	err      error
	client   Client
	starting chan struct{}
	docs     map[string]*document // by URI
	closed   bool
	attempt  int // counts starts, so a late exit of an old server is ignored

	// out sends the documents' notifications, in order, on a goroutine
	// of its own: a write that blocks on a busy server never holds mu.
	out *outbox
}

type document struct {
	version    int    // the protocol's
	bufVersion uint64 // of the buffer last sent
	snap       editor.Snapshot
}

// New returns a Server, stopped until the first Get.
func New(o Options) *Server {
	if o.Language == nil {
		o.Language = func(string) string { return "go" }
	}
	s := &Server{o: o, docs: map[string]*document{}, out: newOutbox()}
	go s.out.loop()
	return s
}

// Status returns the state and, when failed, why.
func (s *Server) Status() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.err
}

func (s *Server) setState(st State, err error) {
	s.state, s.err = st, err
	if s.o.OnState != nil {
		s.o.OnState(st, err)
	}
}

// Get returns the running client, starting the server the first time.
// After a failure it fails until Retry, or until Recover allows a start.
func (s *Server) Get(ctx context.Context) (Client, error) {
	return s.get(ctx, true)
}

func (s *Server) get(ctx context.Context, recover bool) (Client, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, context.Canceled
	}
	switch s.state {
	case Ready:
		c := s.client
		s.mu.Unlock()
		return c, nil
	case Failed:
		err := s.err
		if recover && s.o.Recover != nil {
			s.mu.Unlock()
			if s.o.Recover(err) {
				s.retryIf(err)
				return s.get(ctx, false)
			}
			return nil, err
		}
		s.mu.Unlock()
		return nil, err
	case Off:
		s.starting = make(chan struct{})
		s.attempt++
		s.setState(Starting, nil)
		go s.run(s.starting, s.attempt)
	}
	wait := s.starting
	s.mu.Unlock()
	select {
	case <-wait:
		return s.get(ctx, recover)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// retryIf forgets the failure err, if it is still the server's.
func (s *Server) retryIf(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == Failed && s.err == err {
		s.setState(Off, nil)
	}
}

func (s *Server) run(done chan struct{}, attempt int) {
	defer close(done)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	c, err := s.o.Start(ctx, s.o.Root, func(err error) { s.exited(attempt, err) })
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		if c != nil {
			go c.Shutdown(3 * time.Second)
		}
		return
	}
	if err != nil {
		s.setState(Failed, err)
		return
	}
	if s.state == Failed {
		return // it exited as soon as it started
	}
	if !c.HasDefinition() {
		go c.Shutdown(3 * time.Second)
		s.setState(Failed, errors.New("the language server does not support Go to Definition"))
		return
	}
	s.client = c
	if dc, ok := c.(interface {
		SetDiagnosticsHandler(func(string, *int, []lsp.Diagnostic))
	}); ok {
		dc.SetDiagnosticsHandler(func(uri string, version *int, ds []lsp.Diagnostic) { s.published(c, uri, version, ds) })
	}
	s.docs = map[string]*document{}
	s.setState(Ready, nil)
}

func (s *Server) exited(attempt int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || attempt != s.attempt {
		return
	}
	s.client = nil
	s.docs = map[string]*document{}
	s.setState(Failed, fmt.Errorf("%s stopped: %w", s.o.Name, err))
}

// Retry forgets a failure, so the next request starts the server again.
func (s *Server) Retry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == Failed {
		s.setState(Off, nil)
	}
}

// Sync opens a document in the server, or sends its new text if the
// buffer changed since it was last sent, and waits until it is sent.
// The bookkeeping is under mu; the text goes out on the outbox.
func (s *Server) Sync(ctx context.Context, c Client, docURI string, snap editor.Snapshot) error {
	s.mu.Lock()
	if c != s.client {
		s.mu.Unlock()
		return fmt.Errorf("%s restarted", s.o.Name)
	}
	send := s.queueSync(c, docURI, snap)
	s.mu.Unlock()
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
func (s *Server) queueSync(c Client, docURI string, snap editor.Snapshot) <-chan error {
	done := make(chan error, 1)
	d := s.docs[docURI]
	switch {
	case d == nil:
		d = &document{version: 1, bufVersion: snap.Version(), snap: snap}
		s.docs[docURI] = d
		path, _ := uri.ToPath(docURI)
		language := s.o.Language(path)
		s.out.push(func() {
			err := c.DidOpen(docURI, language, 1, snap.String())
			done <- err
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document open failed: %v", err)
			}
		})
	case d.bufVersion == snap.Version():
		done <- nil
	default:
		d.version++
		d.bufVersion = snap.Version()
		d.snap = snap
		v := d.version
		s.out.push(func() {
			err := c.DidChange(docURI, v, snap.String())
			done <- err
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document change failed: %v", err)
			}
		})
	}
	return done
}

func (s *Server) logf(format string, args ...any) {
	if s.o.Logf != nil {
		s.o.Logf(format, args...)
	}
}

func (s *Server) published(c Client, docURI string, version *int, ds []lsp.Diagnostic) {
	s.mu.Lock()
	d := s.docs[docURI]
	if s.closed || c != s.client || d == nil || (version != nil && *version != d.version) {
		s.mu.Unlock()
		return
	}
	snap := d.snap
	s.mu.Unlock()
	if s.o.OnDiagnostics != nil {
		s.o.OnDiagnostics(s, docURI, snap, ds, c.Encoding())
	}
}

// pull asks a server that answers for diagnostics of a document, as one
// that pushes them would have sent them.
func (s *Server) pull(c Client, docURI string, snap editor.Snapshot) {
	dc, ok := c.(interface {
		Diagnostics(context.Context, string) ([]lsp.Diagnostic, bool, error)
	})
	if !ok {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		ds, full, err := dc.Diagnostics(ctx, docURI)
		if err != nil {
			s.logf("diagnostics failed: %v", err)
			return
		}
		if !full {
			return
		}
		s.mu.Lock()
		d := s.docs[docURI]
		valid := !s.closed && c == s.client && d != nil && d.bufVersion == snap.Version()
		s.mu.Unlock()
		if valid && s.o.OnDiagnostics != nil {
			s.o.OnDiagnostics(s, docURI, snap, ds, c.Encoding())
		}
	}()
}

// Changed sends a document's new text if the server has it open.
func (s *Server) Changed(docURI string, snap editor.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, open := s.docs[docURI]; open && s.client != nil {
		s.queueSync(s.client, docURI, snap)
	}
}

// Close tells the server a document is closed.
func (s *Server) Close(docURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.docs[docURI]; ok && s.client != nil {
		delete(s.docs, docURI)
		c := s.client
		s.out.push(func() { c.DidClose(docURI) })
	}
}

// Stop shuts the server down, in the background; the Server is done.
func (s *Server) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.out.stop()
	if c := s.client; c != nil {
		s.client = nil
		go c.Shutdown(3 * time.Second)
	}
}

// StopWait shuts the server down and waits, as the app quits.
func (s *Server) StopWait(timeout time.Duration) {
	s.mu.Lock()
	s.closed = true
	s.out.stop()
	c := s.client
	s.client = nil
	s.mu.Unlock()
	if c != nil {
		c.Shutdown(timeout)
	}
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
