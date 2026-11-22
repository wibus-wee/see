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
	OnState       func(State, error)
	OnDiagnostics DiagnosticsHandler
	// ApplyEdit, when set, answers a server's workspace/applyEdit
	// request by applying the edit it asks for, reporting failure to it.
	// The client registers it only when the started client exposes a
	// SetApplyEditHandler.
	ApplyEdit func(s *Server, edit *lsp.WorkspaceEdit) error
	Logf      func(string, ...any)
	// Recover, when the server has failed with err, reports whether it
	// may start now, as when the missing server has been installed since.
	// Get then starts it again, once.
	Recover func(err error) bool
}

type DiagnosticReport struct {
	Client Client
	URI    string
	Buffer *editor.Snapshot // nil for a report against the on-disk file
	Items  []lsp.Diagnostic
}

type DiagnosticsHandler func(*Server, DiagnosticReport)

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
	attempt  int  // counts starts, so a late exit of an old server is ignored
	ready    bool // the running client made it to Ready
	retries  int  // restarts since the last session that stayed up

	// out sends the documents' notifications, in order, on a goroutine
	// of its own: a write that blocks on a busy server never holds mu.
	out *outbox
}

type document struct {
	version    int    // the protocol's
	bufVersion uint64 // of the buffer last sent
	snap       editor.Snapshot
	pending    *syncResult
}

type syncResult struct {
	done chan struct{}
	err  error
}

func newSyncResult() *syncResult {
	return &syncResult{done: make(chan struct{})}
}

func completedSyncResult(err error) *syncResult {
	result := newSyncResult()
	result.complete(err)
	return result
}

func (r *syncResult) complete(err error) {
	r.err = err
	close(r.done)
}

func (r *syncResult) wait(ctx context.Context) error {
	select {
	case <-r.done:
		return r.err
	case <-ctx.Done():
		return ctx.Err()
	}
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

// SetApplyEdit is the ApplyEdit option named at run time, for a
// constructor that wraps New without exposing its Options. It is read
// when a client starts, so it answers servers it has not yet started.
func (s *Server) SetApplyEdit(fn func(s *Server, edit *lsp.WorkspaceEdit) error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.o.ApplyEdit = fn
}

// Status returns the state and, when failed, why.
func (s *Server) Status() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state, s.err
}

// Current returns the initialized client without starting the server.
func (s *Server) Current() Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
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
		s.ready = false
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
	var registerDiagnostics func()
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		if registerDiagnostics != nil {
			registerDiagnostics()
		}
	}()
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
	s.ready = true
	if dc, ok := c.(interface {
		SetDiagnosticsHandler(func(string, *int, []lsp.Diagnostic))
	}); ok {
		// Installing the handler can deliver diagnostics queued at startup.
		registerDiagnostics = func() {
			dc.SetDiagnosticsHandler(func(uri string, version *int, ds []lsp.Diagnostic) { s.published(c, uri, version, ds) })
		}
	}
	if s.o.ApplyEdit != nil {
		if ac, ok := c.(interface {
			SetApplyEditHandler(func(*lsp.WorkspaceEdit) error)
		}); ok {
			ac.SetApplyEditHandler(func(edit *lsp.WorkspaceEdit) error { return s.o.ApplyEdit(s, edit) })
		}
	}
	s.docs = map[string]*document{}
	s.setState(Ready, nil)
	go func(attempt int) {
		<-time.After(stableUptime)
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.closed && s.attempt == attempt && s.state == Ready {
			s.retries = 0
		}
	}(s.attempt)
}

// A server that exits on its own comes back on its own too, but not
// forever: only one that ran long enough to be Ready is restarted at
// all, at most maxRestarts times on backoff, and a session that stays
// up for stableUptime earns the retries back.
const (
	maxRestarts  = 2
	stableUptime = 5 * time.Minute
)

var restartDelays = [maxRestarts]time.Duration{30 * time.Second, 2 * time.Minute}

func (s *Server) exited(attempt int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed || attempt != s.attempt {
		return
	}
	s.client = nil
	s.docs = map[string]*document{}
	s.setState(Failed, fmt.Errorf("%s stopped: %w", s.o.Name, err))
	if s.ready && s.retries < maxRestarts {
		s.retries++
		s.afterRestart(restartDelays[s.retries-1])
	}
}

// afterRestart forgets the failure after delay and starts the server
// again in the background; a Retry or a Stop in between wins.
func (s *Server) afterRestart(delay time.Duration) {
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		if s.closed || s.state != Failed || s.retries == 0 {
			s.mu.Unlock()
			return
		}
		s.setState(Off, nil)
		s.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		s.get(ctx, false) // its failure only sets the state again
	})
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
	if err := ctx.Err(); err != nil {
		s.mu.Unlock()
		return err
	}
	if c != s.client {
		s.mu.Unlock()
		return fmt.Errorf("%s restarted", s.o.Name)
	}
	result := s.queueSync(c, docURI, snap)
	s.mu.Unlock()
	return result.wait(ctx)
}

// Open queues the sync without waiting for the server's stdin.
func (s *Server) Open(c Client, docURI string, snap editor.Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c != s.client {
		return false
	}
	s.queueSync(c, docURI, snap)
	return true
}

// queueSync queues what brings the server's copy of a document up to
// snap, with mu held so that versions go out in the order they are
// numbered. The returned result completes after it is sent.
func (s *Server) queueSync(c Client, docURI string, snap editor.Snapshot) *syncResult {
	d := s.docs[docURI]
	switch {
	case d == nil:
		result := newSyncResult()
		d = &document{version: 1, bufVersion: snap.Version(), snap: snap, pending: result}
		s.docs[docURI] = d
		path, _ := uri.ToPath(docURI)
		language := s.o.Language(path)
		s.out.push(func() {
			err := c.DidOpen(docURI, language, 1, snap.String())
			s.completeSync(docURI, d, result, err)
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document open failed: %v", err)
			}
		})
		return result
	case d.bufVersion == snap.Version():
		if d.pending != nil {
			return d.pending
		}
		return completedSyncResult(nil)
	default:
		result := newSyncResult()
		d.version++
		d.bufVersion = snap.Version()
		d.snap = snap
		d.pending = result
		v := d.version
		s.out.push(func() {
			err := c.DidChange(docURI, v, snap.String())
			s.completeSync(docURI, d, result, err)
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document change failed: %v", err)
			}
		})
		return result
	}
}

func (s *Server) completeSync(docURI string, d *document, result *syncResult, err error) {
	result.complete(err)
	s.mu.Lock()
	if s.docs[docURI] == d && d.pending == result {
		d.pending = nil
	}
	s.mu.Unlock()
}

func (s *Server) logf(format string, args ...any) {
	if s.o.Logf != nil {
		s.o.Logf(format, args...)
	}
}

func (s *Server) published(c Client, docURI string, version *int, ds []lsp.Diagnostic) {
	s.mu.Lock()
	d := s.docs[docURI]
	if s.closed || c != s.client || (version != nil && (d == nil || *version != d.version)) {
		s.mu.Unlock()
		return
	}
	report := DiagnosticReport{Client: c, URI: docURI, Items: ds}
	if d != nil {
		snap := d.snap
		report.Buffer = &snap
	}
	s.mu.Unlock()
	if s.o.OnDiagnostics != nil {
		s.o.OnDiagnostics(s, report)
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
			s.o.OnDiagnostics(s, DiagnosticReport{Client: c, URI: docURI, Buffer: &snap, Items: ds})
		}
	}()
}

func (s *Server) WatchedFiles(changes []lsp.FileChange) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.client.(interface{ DidChangeWatchedFiles([]lsp.FileChange) error }); ok {
		s.out.push(func() {
			if err := c.DidChangeWatchedFiles(changes); err != nil {
				s.logf("watched files: %v", err)
			}
		})
	}
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
