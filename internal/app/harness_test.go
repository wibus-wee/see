package app

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"see/internal/config"
	"see/internal/lsp"

	"see/internal/langserver"
)

func init() { watchFiles = nil } // tests feed file events themselves

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
	code := m.Run()
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
	t    *testing.T
	a    *App
	tt   *ui.Tester
	q    chan func()
	fc   *fakeClient
	host *fakeHost
	ran  int // queued results run
}

func newHarness(t *testing.T, root string) *harness {
	t.Helper()
	h := &harness{t: t, q: make(chan func(), 1024), fc: &fakeClient{enc: lsp.UTF16}, host: &fakeHost{}}
	h.a = newApp(&config.Config{})
	h.a.host = h.host
	h.a.post = func(fn func()) { h.q <- fn }
	h.a.logf = testLogf(t)
	h.a.startLSP = func(func(string, ...any)) langserver.Starter {
		return func(ctx context.Context, root string, onExit func(error)) (langserver.Client, error) {
			if h.fc.startErr != nil {
				return nil, h.fc.startErr
			}
			return h.fc, nil
		}
	}
	h.tt = ui.NewTester(h.a.view, harnessWidth, harnessHeight)
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
	mu       sync.Mutex
	enc      string
	startErr error
	def      func(ctx context.Context, uri string, pos lsp.Position) ([]lsp.Location, error)
	hover    func(ctx context.Context, uri string, pos lsp.Position) (string, *lsp.Range, error)
	log      []string
	shutdown bool
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

func (f *fakeClient) Encoding() string    { return f.enc }
func (f *fakeClient) HasDefinition() bool { return true }
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
func (f *fakeClient) Shutdown(time.Duration) {
	f.mu.Lock()
	f.shutdown = true
	f.mu.Unlock()
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
	return langserver.New(langserver.Options{Name: "gopls", Root: root, Start: func(context.Context, string, func(error)) (langserver.Client, error) { return c, nil }})
}

// fakeHost is a host without a desktop, as noHost, but for what a test
// gives it to see what the app asks.
type fakeHost struct {
	noHost
	newFile     func(root string) (string, error)
	confirm     func(message string) (int, error)
	trash       func(path string) error
	copy        func(paths ...string) error
	copyPNG     func(png []byte) error
	openProject func(path string) bool
	resize      func(width, height int)
	appearance  func(string)
	fontPopover func(anchor ui.Rect, terminal bool)
}

func (f *fakeHost) pickNewFile(root string) (string, error) {
	if f.newFile == nil {
		return f.noHost.pickNewFile(root)
	}
	return f.newFile(root)
}

func (f *fakeHost) confirmDirty(message string) (int, error) {
	if f.confirm == nil {
		return f.noHost.confirmDirty(message)
	}
	return f.confirm(message)
}

func (f *fakeHost) trashItem(path string) error {
	if f.trash == nil {
		return f.noHost.trashItem(path)
	}
	return f.trash(path)
}

func (f *fakeHost) copyFiles(paths ...string) error {
	if f.copy == nil {
		return f.noHost.copyFiles(paths...)
	}
	return f.copy(paths...)
}

func (f *fakeHost) copyImage(png []byte) error {
	if f.copyPNG == nil {
		return f.noHost.copyImage(png)
	}
	return f.copyPNG(png)
}

func (f *fakeHost) openProjectWindow(path string) bool {
	return f.openProject != nil && f.openProject(path)
}

func (f *fakeHost) setWorkspaceSize(width, height int) {
	if f.resize != nil {
		f.resize(width, height)
	}
}

func (f *fakeHost) setAppearance(appearance string) {
	if f.appearance != nil {
		f.appearance(appearance)
	}
}

func (f *fakeHost) openFontPopover(anchor ui.Rect, terminal bool) {
	if f.fontPopover != nil {
		f.fontPopover(anchor, terminal)
	}
}
