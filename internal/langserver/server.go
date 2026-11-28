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
	"see/internal/operr"
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
	Stopping
	Degraded
)

// Starter starts a client for a workspace; onExit is called if the
// server goes away by itself.
type Starter func(ctx context.Context, root string, folders []lsp.WorkspaceFolder, onExit func(error)) (Client, error)

// Options configure a Server.
type Options struct {
	Name     string // as errors and logs call it, such as "gopls"
	Root     string
	Folders  []lsp.WorkspaceFolder
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

	mu          sync.Mutex
	state       State
	err         error
	client      Client
	starting    chan struct{}
	cancelStart context.CancelFunc
	stopped     chan struct{}
	sessionDone chan struct{}
	docs        map[string]*document // by URI
	closed      bool
	attempt     int // counts starts, so a late exit of an old server is ignored
	readySince  time.Time
	retries     int // restarts since the last session that stayed up

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

// New returns a Server, stopped until the first Get.
func New(o Options) *Server {
	if o.Language == nil {
		o.Language = func(string) string { return "go" }
	}
	s := &Server{o: o, docs: map[string]*document{}, out: newOutbox(), stopped: make(chan struct{})}
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

// Name is what errors and logs call the server, as configured.
func (s *Server) Name() string { return s.o.Name }

// Current returns the initialized client without starting the server.
func (s *Server) Current() Client {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.client
}

// MatchesDocumentVersion reports whether the current session has the open
// document at both the requested LSP version and editor buffer revision.
func (s *Server) MatchesDocumentVersion(uri string, protocolVersion int64, bufferVersion uint64) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.docs[uri]
	return s.client != nil && d != nil && int64(d.version) == protocolVersion && d.bufVersion == bufferVersion
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
	case Ready, Degraded:
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
		s.readySince = time.Time{}
		s.setState(Starting, nil)
		startCtx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		s.cancelStart = cancel
		go s.run(startCtx, cancel, s.starting, s.attempt)
	}
	wait := s.starting
	s.mu.Unlock()
	select {
	case <-wait:
		return s.get(ctx, recover)
	case <-s.out.quit:
		return nil, context.Canceled
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

func (s *Server) run(ctx context.Context, cancel context.CancelFunc, done chan struct{}, attempt int) {
	defer close(done)
	defer cancel()
	c, err := s.o.Start(ctx, s.o.Root, s.o.Folders, func(err error) { s.exited(attempt, err) })
	var registerDiagnostics func()
	s.mu.Lock()
	defer func() {
		s.mu.Unlock()
		if registerDiagnostics != nil {
			registerDiagnostics()
		}
	}()
	if s.closed || attempt != s.attempt {
		if err == nil && c != nil {
			s.mu.Unlock()
			c.Shutdown(3 * time.Second)
			s.mu.Lock()
		}
		return
	}
	if err != nil {
		s.setState(Failed, err)
		return
	}
	if s.state != Starting {
		return // it exited as soon as it started
	}
	s.client = c
	s.sessionDone = make(chan struct{})
	s.readySince = time.Now()
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
			apply := s.o.ApplyEdit
			ac.SetApplyEditHandler(func(edit *lsp.WorkspaceEdit) error {
				if s.Current() != c {
					return errors.New("workspace edit from obsolete session")
				}
				return apply(s, edit)
			})
		}
	}
	previous := s.docs
	s.docs = map[string]*document{}
	s.setState(Ready, nil)
	for uri, d := range previous {
		s.queueSync(c, uri, d.snap)
	}
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
	if s.state != Starting && s.sessionDone == nil {
		return
	}
	s.failSession(fmt.Errorf("%s stopped: %w", s.o.Name, err))
}

func (s *Server) failSession(err error) {
	if s.sessionDone != nil {
		close(s.sessionDone)
		s.sessionDone = nil
	}
	s.client = nil
	if s.readySince.IsZero() {
		s.setState(Failed, err)
		return
	}
	if time.Since(s.readySince) >= stableUptime {
		s.retries = 0
	}
	s.readySince = time.Time{}
	if s.retries >= maxRestarts {
		failure := operr.Classify(err)
		failure.Status = operr.Persistent
		failure.Attempt = s.attempt
		failure.Operation = "language-server session"
		s.setState(Failed, failure)
		return
	}
	s.setState(Failed, err)
	s.retries++
	s.afterRestart(restartDelays[s.retries-1])
}

// afterRestart forgets the failure after delay and starts the server
// again in the background; a Retry or a Stop in between wins.
func (s *Server) afterRestart(delay time.Duration) {
	attempt := s.attempt
	time.AfterFunc(delay, func() {
		s.mu.Lock()
		if s.closed || s.state != Failed || s.attempt != attempt {
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
// It is the user's deliberate act — typically after reinstalling or
// reconfiguring the server — so the crash budget starts over with it:
// counting pre-fix crashes against the post-Retry session would durably
// fail a server after one fresh crash.
func (s *Server) Retry() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.state == Failed {
		s.retries = 0
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
	if c == nil || c != s.client {
		s.mu.Unlock()
		return fmt.Errorf("%s restarted", s.o.Name)
	}
	sessionDone := s.sessionDone
	result := s.queueSync(c, docURI, snap)
	s.mu.Unlock()
	select {
	case <-result.done:
		return result.err
	case <-sessionDone:
		return fmt.Errorf("%s session ended", s.o.Name)
	case <-ctx.Done():
		return ctx.Err()
	case <-s.out.quit:
		return context.Canceled
	}
}

// Open queues the sync without waiting for the server's stdin.
func (s *Server) Open(c Client, docURI string, snap editor.Snapshot) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c == nil || c != s.client {
		return false
	}
	s.queueSync(c, docURI, snap)
	return s.client == c
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
		text := snap.String()
		if !s.enqueue(func() {
			err := c.DidOpen(docURI, language, 1, text)
			s.completeSync(docURI, d, result, err)
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document open failed: %v", err)
			}
		}, len(text)) {
			result.complete(errQueueFull)
		}
		return result
	case d.bufVersion > snap.Version():
		return completedSyncResult(errors.New("obsolete document snapshot"))
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
		text := snap.String()
		if !s.enqueue(func() {
			err := c.DidChange(docURI, v, text)
			s.completeSync(docURI, d, result, err)
			if err == nil {
				s.pull(c, docURI, snap)
			} else {
				s.logf("document change failed: %v", err)
			}
		}, len(text)) {
			result.complete(errQueueFull)
		}
		return result
	}
}

func (s *Server) completeSync(docURI string, d *document, result *syncResult, err error) {
	result.complete(err)
	s.mu.Lock()
	if s.docs[docURI] == d && d.pending == result {
		if err == nil {
			d.pending = nil
		} else if s.client != nil && !s.closed {
			s.setState(Degraded, err)
		}
	}
	// A successful delivery after a failure clears Degraded: changes are
	// full-text, so whatever the failure cost, the server now holds this
	// document at the buffer's own version again.
	if err == nil && s.state == Degraded && s.client != nil && !s.closed {
		s.setState(Ready, nil)
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
		cost := 0
		for _, change := range changes {
			cost += len(change.URI)
		}
		s.enqueue(func() {
			if err := c.DidChangeWatchedFiles(changes); err != nil {
				s.logf("watched files: %v", err)
			}
		}, cost)
	}
}

// Changed sends a document's new text if the server has it open.
func (s *Server) Changed(docURI string, snap editor.Snapshot) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if d := s.docs[docURI]; d != nil {
		if s.client != nil {
			s.queueSync(s.client, docURI, snap)
		} else if snap.Version() >= d.bufVersion {
			d.snap, d.bufVersion = snap, snap.Version()
		}
	}
}

// Close tells the server a document is closed.
func (s *Server) Close(docURI string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.docs[docURI]; ok {
		delete(s.docs, docURI)
		if s.client == nil {
			return
		}
		c := s.client
		s.enqueue(func() { c.DidClose(docURI) }, len(docURI))
	}
}

// Stop shuts the server down, in the background; the Server is done.
func (s *Server) Stop() { s.beginStop(3 * time.Second) }

// StopWait initiates shutdown once and waits up to timeout for completion.
func (s *Server) StopWait(timeout time.Duration) {
	s.beginStop(timeout)
	select {
	case <-s.stopped:
	case <-time.After(timeout):
	}
}

func (s *Server) beginStop(timeout time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	s.out.stop()
	if s.cancelStart != nil {
		s.cancelStart()
	}
	c, starting := s.client, s.starting
	s.client = nil
	s.docs = nil
	s.setState(Stopping, nil)
	go func() {
		if starting != nil {
			<-starting
		}
		if c != nil {
			c.Shutdown(timeout)
		}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.setState(Off, nil)
		close(s.stopped)
	}()
}

// outbox runs functions one after the other on its goroutine, in the
// order they were pushed; pushing never blocks. Queued notifications are
// bounded by count and by the payload bytes they carry, since a blocked
// server otherwise accumulates one full document copy per edit.
type outbox struct {
	mu    sync.Mutex
	q     []outboxItem
	bytes int
	wake  chan struct{}
	quit  chan struct{}
	once  sync.Once
}

type outboxItem struct {
	fn   func()
	cost int
}

const outboxMaxCount = 256

// outboxMaxBytes caps the payload of queued notifications. Document text
// dominates it; a variable so tests can exercise the overflow with small
// documents.
var outboxMaxBytes = 64 << 20

func newOutbox() *outbox {
	return &outbox{wake: make(chan struct{}, 1), quit: make(chan struct{})}
}

var errQueueFull = errors.New("language server notification queue full")

// A missing notification invalidates the session: restart with full snapshots
// rather than continue against an unknown document version. Caller holds mu.
func (s *Server) enqueue(fn func(), cost int) bool {
	if s.client == nil {
		return false
	}
	if s.out.push(fn, cost) {
		return true
	}
	c := s.client
	s.failSession(errQueueFull)
	go c.Shutdown(time.Second)
	return false
}

func (o *outbox) push(fn func(), cost int) bool {
	o.mu.Lock()
	if len(o.q) >= outboxMaxCount || o.bytes+cost > outboxMaxBytes {
		o.mu.Unlock()
		return false
	}
	o.q = append(o.q, outboxItem{fn, cost})
	o.bytes += cost
	o.mu.Unlock()
	select {
	case o.wake <- struct{}{}:
	default:
	}
	return true
}

func (o *outbox) loop() {
	for {
		o.mu.Lock()
		q := o.q
		o.q = nil
		o.mu.Unlock()
		for _, item := range q {
			select {
			case <-o.quit:
				return
			default:
			}
			item.fn()
			o.mu.Lock()
			o.bytes -= item.cost
			o.mu.Unlock()
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
