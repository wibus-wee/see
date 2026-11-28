package app

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/avatar"
	"see/internal/config"
	"see/internal/lsp"
	"see/internal/platform"

	"see/internal/langserver"
)

func init() {
	watchFiles = nil // tests feed file events themselves
	avatarFetch = func(context.Context, *http.Client, string, []string) ([]byte, error) { return nil, avatar.ErrNone }
	// Notifications need the running app; tests record what would fire
	// through notifySent, notifyBadges and notifyBounces instead, and
	// play the window's focus with notifyFocusedOn.
	notifySend = func(title, body string, silent bool) {
		notifySent = append(notifySent, sentNotification{title, body, silent})
	}
	notifyDockBadge = func(text string) { notifyBadges = append(notifyBadges, text) }
	notifyDockBounce = func() { notifyBounces++ }
	notifyFocused = func() bool { return notifyFocusedOn }
}

// sentNotification is what notifySend would have shown.
type sentNotification struct {
	title, body string
	silent      bool
}

// The notifications tests' fakes recorded. Tests set notifyFocusedOn to
// give the window focus, and the prefs to switch notifications on.
var (
	notifySent      []sentNotification
	notifyBadges    []string
	notifyBounces   int
	notifyFocusedOn bool
)

// resetNotify clears the fakes and focuses the window; newHarness calls
// it for every test.
func resetNotify() {
	notifySent, notifyBadges, notifyBounces, notifyFocusedOn = nil, nil, 0, true
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "see-test-home-")
	if err != nil {
		panic(err)
	}
	realCache, _ := os.UserCacheDir()
	for _, key := range []string{"HOME", "USERPROFILE", "APPDATA", "LOCALAPPDATA", "XDG_CONFIG_HOME"} {
		if err := os.Setenv(key, dir); err != nil {
			panic(err)
		}
	}
	shareNativesCache(realCache)
	code := runTestsWithAppKitMainThread(m)
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// shareNativesCache links mygo's cache of native libraries in the test
// home to the user's, so that libghostty downloads once rather than on
// every run. The test home keeps the rest of the cache to itself.
func shareNativesCache(realCache string) {
	testCache, err := os.UserCacheDir()
	if realCache == "" || err != nil || testCache == realCache {
		return
	}
	shared := filepath.Join(realCache, "mygo")
	if os.MkdirAll(shared, 0o755) != nil || os.MkdirAll(testCache, 0o755) != nil {
		return
	}
	_ = os.Symlink(shared, filepath.Join(testCache, "mygo"))
}

// harnessWidth and harnessHeight are the size of the harness's window.
const harnessWidth, harnessHeight = 1100, 700

// harness runs the app's view in a tester, with background results
// queued until the test lets them through.
type harness struct {
	t        *testing.T
	a        *App
	tt       *ui.Tester
	q        chan func()
	fc       *fakeClient
	host     *fakeHost
	ran      int // queued results run
	feedback []feedbackEntry
}

func newHarness(t *testing.T, root string) *harness {
	return newHarnessWithConfig(t, root, &config.Config{ConfirmClose: true})
}

func newMultiRootHarness(t *testing.T, root string) *harness {
	t.Helper()
	return newMultiRootHarnessWithConfig(t, root, &config.Config{ConfirmClose: true})
}

func newMultiRootHarnessWithConfig(t *testing.T, root string, cfg *config.Config) *harness {
	t.Helper()
	cfg.SetBetaEnabled(config.BetaMultiRootWorkspaces, true)
	return newHarnessWithConfig(t, root, cfg)
}

func newHarnessWithConfig(t *testing.T, root string, cfg *config.Config) *harness {
	t.Helper()
	h := &harness{t: t, q: make(chan func(), 1024), fc: &fakeClient{enc: lsp.UTF16}, host: &fakeHost{}}
	h.a = newApp(cfg)
	t.Cleanup(func() { releaseStandalonePaths(h.a) })
	h.a.hasWelcome = true
	h.a.host = h.host
	h.a.post.set(platform.SchedulerFunc(func(fn func()) { h.q <- fn }))
	h.a.logf = testLogf(t)
	resetNotify()
	t.Cleanup(resetNotify)
	h.a.startLSP = func(func(string, ...any)) langserver.Starter {
		return func(ctx context.Context, root string, _ []lsp.WorkspaceFolder, onExit func(error)) (langserver.Client, error) {
			if h.fc.startErr != nil {
				return nil, h.fc.startErr
			}
			return h.fc, nil
		}
	}
	h.tt = ui.NewTester(h.a.view, harnessWidth, harnessHeight)
	h.installFeedbackLog()
	if root != "" {
		h.a.openWorkspace(root)
		h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	}
	return h
}

// testLogf logs to t until the test ends, and drops what background
// work logs after it, which t.Logf would panic on.
func testLogf(t *testing.T) func(string, ...any) {
	var mu sync.Mutex
	done := false
	t.Cleanup(func() {
		mu.Lock()
		done = true
		mu.Unlock()
	})
	return func(format string, args ...any) {
		mu.Lock()
		defer mu.Unlock()
		if !done {
			t.Logf(format, args...)
		}
	}
}

// newHarness records the feedback the app reports in h.feedback: flash
// events drain into toasts on the first frame after they land, after
// which nothing observes them, so tests read the recording rather than
// the state. Production leaves feedbackLog nil.
func (h *harness) installFeedbackLog() {
	feedbackLog = &h.feedback
	h.t.Cleanup(func() { feedbackLog = nil })
}

// feedbackTexts is the feedback the test's app reported, errors as the
// successes, in order.
func (h *harness) feedbackTexts() []string {
	var out []string
	for _, e := range h.feedback {
		out = append(out, e.msg)
	}
	return out
}

// hasFeedback reports whether the app reported exactly the message.
func (h *harness) hasFeedback(msg string) bool {
	for _, e := range h.feedback {
		if e.msg == msg {
			return true
		}
	}
	return false
}

// feedbackContains reports whether some reported feedback carries part.
func (h *harness) feedbackContains(part string) bool {
	for _, e := range h.feedback {
		if strings.Contains(e.msg, part) {
			return true
		}
	}
	return false
}

// feedbackError is the newest error the app reported, if any.
func (h *harness) feedbackError() (string, bool) {
	for i := len(h.feedback) - 1; i >= 0; i-- {
		if h.feedback[i].err {
			return h.feedback[i].msg, true
		}
	}
	return "", false
}

// clearFeedback forgets what the app reported, as a test step ends.
func (h *harness) clearFeedback() { h.feedback = nil }

// treeHas reports whether the file tree shows a row of p.
func (h *harness) treeHas(p string) bool {
	for i := range h.a.tree.Rows() {
		if h.a.tree.Item(i) == p {
			return true
		}
	}
	return false
}

// settle runs queued results until cond holds, then draws a frame.
func (h *harness) settle(cond func() bool) {
	h.t.Helper()
	guard := time.After(time.Minute) // turns a hang into a failure; never tuned
	for !cond() {
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		case <-guard:
			h.t.Fatal("condition never held")
		}
	}
	h.tt.Frame()
}

// open opens a file as from the tree and waits for it to load.
func (h *harness) open(rel string) *doc {
	h.t.Helper()
	p := filepath.Join(h.a.root, rel)
	h.a.userOpen(p)
	d := h.a.docs[p]
	h.settle(func() bool { return !d.loading })
	return d
}

type fakeClient struct {
	mu                sync.Mutex
	enc               string
	startErr          error
	def               func(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error)
	implementation    func(context.Context, string, lsp.Position) ([]lsp.Location, error)
	typeDefinition    func(context.Context, string, lsp.Position) ([]lsp.Location, error)
	references        func(context.Context, string, lsp.Position) ([]lsp.Location, error)
	noNavigation      bool
	hover             func(ctx context.Context, uri string, pos lsp.Position) (string, *lsp.Range, error)
	inlayHints        func(ctx context.Context, uri string, r lsp.Range) ([]lsp.InlayHint, error)
	documentLinks     func(ctx context.Context, uri string) ([]lsp.DocumentLink, error)
	foldingRanges     func(ctx context.Context, uri string) ([]lsp.FoldingRange, error)
	signature         func(ctx context.Context, uri string, pos lsp.Position) (*lsp.SignatureHelp, error) // nil: no signature help
	completion        func(ctx context.Context, uri string, pos lsp.Position) (*lsp.CompletionResult, error)
	completionResolve func(context.Context, lsp.CompletionItem) (*lsp.CompletionItem, error)
	semanticTokens    func(ctx context.Context, uri string, first, last int) (*lsp.SemanticTokensData, error)
	semanticTypes     []string // the legend fake answers, when semanticTokens is set
	callHier          callFake
	noCalls           bool
	codeActions       []lsp.CodeAction
	codeActionsErr    error
	noCommands        bool
	lastCommand       lsp.Command
	rename            func(ctx context.Context, uri string, pos lsp.Position, newName string) (*lsp.WorkspaceEdit, error)
	prepareRename     func(ctx context.Context, uri string, pos lsp.Position) (*lsp.PrepareRenameResult, error)
	noRename          bool // server without renameProvider
	format            formatFake
	watched           []lsp.FileChange
	log               []string
	shutdown          bool
}

// formatFake answers the formatting requests tests give it: a nil
// document or range field is a server without that capability; a set
// one records what the request carried and answers its edits.
type formatFake struct {
	document *struct {
		opts  lsp.FormattingOptions
		edits []lsp.TextEdit
		err   error
	}
	rng *struct {
		r     lsp.Range
		opts  lsp.FormattingOptions
		edits []lsp.TextEdit
		err   error
	}
}

func (f *fakeClient) HasDocumentFormatting() bool      { return f.format.document != nil }
func (f *fakeClient) HasDocumentRangeFormatting() bool { return f.format.rng != nil }
func (f *fakeClient) DocumentFormatting(ctx context.Context, uri string, opts lsp.FormattingOptions) ([]lsp.TextEdit, error) {
	f.record("formatting")
	f.mu.Lock()
	f.format.document.opts = opts
	defer f.mu.Unlock()
	return f.format.document.edits, f.format.document.err
}
func (f *fakeClient) DocumentRangeFormatting(ctx context.Context, uri string, r lsp.Range, opts lsp.FormattingOptions) ([]lsp.TextEdit, error) {
	f.record("rangeFormatting")
	f.mu.Lock()
	f.format.rng.r, f.format.rng.opts = r, opts
	defer f.mu.Unlock()
	return f.format.rng.edits, f.format.rng.err
}

// callFake answers the hierarchy requests tests give it.
type callFake struct {
	items    []lsp.CallItem
	prepare  error
	incoming func(lsp.CallItem) ([]lsp.CallEdge, error)
	outgoing func(lsp.CallItem) ([]lsp.CallEdge, error)
}

func (f *fakeClient) DidChangeWatchedFiles(changes []lsp.FileChange) error {
	f.mu.Lock()
	f.watched = append(f.watched, changes...)
	f.mu.Unlock()
	return nil
}

func (f *fakeClient) watchedFiles() []lsp.FileChange {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]lsp.FileChange(nil), f.watched...)
}

func (f *fakeClient) record(s string) {
	f.mu.Lock()
	f.log = append(f.log, s)
	f.mu.Unlock()
}

func (f *fakeClient) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.log)
}

func (f *fakeClient) Encoding() string        { return f.enc }
func (f *fakeClient) HasDefinition() bool     { return true }
func (f *fakeClient) HasImplementation() bool { return !f.noNavigation }
func (f *fakeClient) HasTypeDefinition() bool { return !f.noNavigation }
func (f *fakeClient) Implementation(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error) {
	f.record("implementation")
	if f.implementation == nil {
		return nil, nil
	}
	return f.implementation(ctx, uri, pos)
}
func (f *fakeClient) TypeDefinition(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error) {
	f.record("typeDefinition")
	if f.typeDefinition == nil {
		return nil, nil
	}
	return f.typeDefinition(ctx, uri, pos)
}
func (f *fakeClient) References(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error) {
	f.record("references")
	if f.references == nil {
		return nil, nil
	}
	return f.references(ctx, uri, pos)
}
func (f *fakeClient) DidOpen(uri, lang string, v int, text string) error {
	f.record("open " + filepath.Base(uri))
	return nil
}
func (f *fakeClient) DidChange(uri string, v int, text string) error {
	f.record("change " + filepath.Base(uri))
	return nil
}
func (f *fakeClient) DidClose(uri string) error { f.record("close " + filepath.Base(uri)); return nil }
func (f *fakeClient) Definition(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error) {
	f.record("definition")
	if f.def == nil {
		return nil, nil
	}
	return f.def(ctx, uri, pos)
}
func (f *fakeClient) Hover(ctx context.Context, uri string, pos lsp.Position) (string, *lsp.Range, error) {
	f.record("hover")
	if f.hover == nil {
		return "", nil, nil
	}
	return f.hover(ctx, uri, pos)
}
func (f *fakeClient) HasInlayHints() bool { return f.inlayHints != nil }
func (f *fakeClient) InlayHints(ctx context.Context, uri string, r lsp.Range) ([]lsp.InlayHint, error) {
	f.record("inlayHint")
	if f.inlayHints == nil {
		return nil, nil
	}
	return f.inlayHints(ctx, uri, r)
}
func (f *fakeClient) SemanticTokensFull() bool { return f.semanticTokens != nil }
func (f *fakeClient) HasSemanticTokens() bool  { return f.semanticTokens != nil }
func (f *fakeClient) SemanticTokens(ctx context.Context, uri string, first, last int) (*lsp.SemanticTokensData, error) {
	f.record("semanticTokens")
	if f.semanticTokens == nil {
		return nil, nil
	}
	return f.semanticTokens(ctx, uri, first, last)
}
func (f *fakeClient) SemanticTokenTypes() []string { return f.semanticTypes }

func (f *fakeClient) HasDocumentLinks() bool           { return f.documentLinks != nil }
func (f *fakeClient) DocumentLinkResolveSupport() bool { return false }
func (f *fakeClient) DocumentLinks(ctx context.Context, uri string) ([]lsp.DocumentLink, error) {
	f.record("documentLink")
	if f.documentLinks == nil {
		return nil, nil
	}
	return f.documentLinks(ctx, uri)
}
func (f *fakeClient) DocumentLinkResolve(ctx context.Context, link *lsp.DocumentLink) (*lsp.DocumentLink, error) {
	f.record("documentLink/resolve")
	return link, nil
}
func (f *fakeClient) HasFoldingRange() bool { return f.foldingRanges != nil }
func (f *fakeClient) FoldingRanges(ctx context.Context, uri string) ([]lsp.FoldingRange, error) {
	f.record("foldingRange")
	if f.foldingRanges == nil {
		return nil, nil
	}
	return f.foldingRanges(ctx, uri)
}
func (f *fakeClient) HasSignatureHelp() bool { return f.signature != nil }
func (f *fakeClient) SignatureHelp(ctx context.Context, uri string, pos lsp.Position) (*lsp.SignatureHelp, error) {
	f.record("signatureHelp")
	return f.signature(ctx, uri, pos)
}
func (f *fakeClient) HasCompletion() bool { return f.completion != nil }
func (f *fakeClient) Completion(ctx context.Context, uri string, pos lsp.Position) (*lsp.CompletionResult, error) {
	f.record("completion")
	return f.completion(ctx, uri, pos)
}
func (f *fakeClient) HasCompletionItemResolve() bool { return f.completionResolve != nil }
func (f *fakeClient) CompletionItemResolve(ctx context.Context, item lsp.CompletionItem) (*lsp.CompletionItem, error) {
	f.record("completionItem/resolve")
	if f.completionResolve == nil {
		return nil, nil
	}
	return f.completionResolve(ctx, item)
}
func (f *fakeClient) Shutdown(time.Duration) {
	f.mu.Lock()
	f.shutdown = true
	f.mu.Unlock()
}

func (f *fakeClient) HasCodeActions() bool { return f.codeActions != nil || f.codeActionsErr != nil }
func (f *fakeClient) CodeActionKinds() []string {
	return []string{"quickfix"}
}
func (f *fakeClient) SupportsCommand(string) bool { return !f.noCommands }
func (f *fakeClient) CodeActions(ctx context.Context, uri string, r lsp.Range, ds []lsp.Diagnostic) ([]lsp.CodeAction, error) {
	f.record("codeActions")
	return f.codeActions, f.codeActionsErr
}
func (f *fakeClient) ResolveCodeAction(ctx context.Context, action lsp.CodeAction) (*lsp.CodeAction, error) {
	f.record("resolveCodeAction " + action.Title)
	return &action, nil
}
func (f *fakeClient) ExecuteCommand(ctx context.Context, command lsp.Command) error {
	f.record("executeCommand " + command.Command)
	f.mu.Lock()
	f.lastCommand = command
	f.mu.Unlock()
	return nil
}

func (f *fakeClient) HasCallHierarchy() bool { return !f.noCalls }

func (f *fakeClient) HasRename() bool        { return !f.noRename && f.rename != nil }
func (f *fakeClient) HasPrepareRename() bool { return f.prepareRename != nil }

func (f *fakeClient) PrepareRename(ctx context.Context, uri string, pos lsp.Position) (*lsp.PrepareRenameResult, error) {
	f.record("prepareRename")
	if f.prepareRename == nil {
		return nil, lsp.ErrNoRename
	}
	return f.prepareRename(ctx, uri, pos)
}

func (f *fakeClient) Rename(ctx context.Context, uri string, pos lsp.Position, newName string) (*lsp.WorkspaceEdit, error) {
	f.record("rename " + newName)
	return f.rename(ctx, uri, pos, newName)
}

func (f *fakeClient) PrepareCallHierarchy(ctx context.Context, uri string, pos lsp.Position) ([]lsp.CallItem, error) {
	f.record("prepareCallHierarchy")
	if f.callHier.prepare != nil {
		return nil, f.callHier.prepare
	}
	return f.callHier.items, nil
}

func (f *fakeClient) IncomingCalls(ctx context.Context, item lsp.CallItem) ([]lsp.CallEdge, error) {
	f.record("incomingCalls " + item.Name)
	if f.callHier.incoming == nil {
		return nil, nil
	}
	return f.callHier.incoming(item)
}

func (f *fakeClient) OutgoingCalls(ctx context.Context, item lsp.CallItem) ([]lsp.CallEdge, error) {
	f.record("outgoingCalls " + item.Name)
	if f.callHier.outgoing == nil {
		return nil, nil
	}
	return f.callHier.outgoing(item)
}

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for rel, text := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func serverOf(root string, c langserver.Client) *langserver.Server {
	return langserver.New(langserver.Options{Name: "gopls", Root: root, Start: func(context.Context, string, []lsp.WorkspaceFolder, func(error)) (langserver.Client, error) {
		return c, nil
	}})
}

// fakeHost is a desktop without a window, as platform.NoDesktop, but for what a test
// gives it to see what the app asks.
type fakeHost struct {
	platform.NoDesktop
	newFile     func(root, name string) (string, error)
	pickFile    func() (string, error)
	openFile    func() (string, error)
	confirm     func(message string) (int, error)
	trash       func(path string) error
	copy        func(paths ...string) error
	copyPNG     func(png []byte) error
	revealFile  func(path string) bool
	openProject func(path string) bool
	resize      func(width, height int)
	appearance  func(string)
	fontPopover func(anchor ui.Rect, terminal bool)
	closed      int // CloseWindow calls
}

func (f *fakeHost) CloseWindow() { f.closed++ }

func (f *fakeHost) PickNewFile(root, name string) (string, error) {
	if f.newFile == nil {
		return f.NoDesktop.PickNewFile(root, name)
	}
	return f.newFile(root, name)
}

func (f *fakeHost) ConfirmDirty(message string) (int, error) {
	if f.confirm == nil {
		return f.NoDesktop.ConfirmDirty(message)
	}
	return f.confirm(message)
}

func (f *fakeHost) TrashItem(path string) error {
	if f.trash == nil {
		return f.NoDesktop.TrashItem(path)
	}
	return f.trash(path)
}

func (f *fakeHost) CopyFiles(paths ...string) error {
	if f.copy == nil {
		return f.NoDesktop.CopyFiles(paths...)
	}
	return f.copy(paths...)
}

func (f *fakeHost) CopyImage(png []byte) error {
	if f.copyPNG == nil {
		return f.NoDesktop.CopyImage(png)
	}
	return f.copyPNG(png)
}

func (f *fakeHost) RevealOpenFile(path string) bool {
	return f.revealFile != nil && f.revealFile(path)
}

func (f *fakeHost) OpenProjectWindow(path string) bool {
	return f.openProject != nil && f.openProject(path)
}

func (f *fakeHost) PickFile() (string, error) {
	if f.pickFile != nil {
		return f.pickFile()
	}
	if f.openFile != nil {
		return f.openFile()
	}
	return f.NoDesktop.PickFile()
}

func (f *fakeHost) SetWorkspaceSize(width, height int) {
	if f.resize != nil {
		f.resize(width, height)
	}
}

func (f *fakeHost) SetAppearance(appearance string) {
	if f.appearance != nil {
		f.appearance(appearance)
	}
}

func (f *fakeHost) OpenFontPopover(anchor ui.Rect, terminal bool) {
	if f.fontPopover != nil {
		f.fontPopover(anchor, terminal)
	}
}
