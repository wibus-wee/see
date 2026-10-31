package app

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/lsp"

	"see/internal/langserver"
	"see/internal/workspace"
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
	h.a = newApp(&config{})
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

// ed is the focused group's editor of d, nil until d loads.
func (h *harness) ed(d *doc) *editor.State {
	if d == nil {
		return nil
	}
	return h.a.focus.eds[d.path]
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

func (h *harness) click(s string) {
	h.t.Helper()
	if err := h.tt.Click(s); err != nil {
		h.t.Fatalf("click %q: %v\ntexts: %q", s, err, h.tt.Texts())
	}
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

func TestWelcomeWithoutWorkspace(t *testing.T) {
	h := newHarness(t, "")
	if !h.tt.HasText("Open Folder…") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	h.a.openWorkspace(filepath.Join(t.TempDir(), "missing"))
	h.tt.Frame()
	if h.a.root != "" || !strings.Contains(h.a.welcomeNote, "does not exist") {
		t.Errorf("root %q, note %q", h.a.root, h.a.welcomeNote)
	}
}

func TestTreeLoadsLazily(t *testing.T) {
	root := writeTree(t, map[string]string{
		"b.go":           "package b\n",
		"sub/c.go":       "package sub\n",
		"sub/deep/d.go":  "package deep\n",
		"node_modules/x": "x",
	})
	h := newHarness(t, root)
	for _, s := range []string{"sub", "node_modules", "b.go"} {
		if !h.tt.HasText(s) {
			t.Fatalf("missing %q in %q", s, h.tt.Texts())
		}
	}
	if h.tt.HasText("c.go") || h.a.dirs[filepath.Join(root, "sub")] != nil {
		t.Fatal("a closed folder was read")
	}
	// Directories come before files.
	if got := h.a.dirs[root].entries; got[0].Name != "node_modules" || got[1].Name != "sub" || got[2].Name != "b.go" {
		t.Errorf("order %v", got)
	}
	h.click("sub")
	sub := filepath.Join(root, "sub")
	h.settle(func() bool { d := h.a.dirs[sub]; return d != nil && d.loaded })
	if !h.tt.HasText("c.go") || !h.tt.HasText("deep") {
		t.Errorf("sub not shown: %q", h.tt.Texts())
	}
	if h.a.dirs[filepath.Join(sub, "deep")] != nil {
		t.Error("read a folder below the one opened")
	}
	h.click("sub")
	if h.tt.HasText("c.go") {
		t.Error("sub did not close")
	}
}

func TestOpenFilesAndTabs(t *testing.T) {
	root := writeTree(t, map[string]string{
		"a.go": "package a\n", "b.go": "package b\n", "c.go": "package c\n",
		"x/util.go": "package x\n", "y/util.go": "package y\n",
	})
	h := newHarness(t, root)
	h.click("a.go")
	pa := filepath.Join(root, "a.go")
	h.settle(func() bool { return h.a.docs[pa] != nil && h.ed(h.a.docs[pa]) != nil })
	if h.a.activePath() != pa || h.a.docs[pa].buf.String() != "package a\n" {
		t.Fatalf("active %q, texts %q", h.a.activePath(), h.tt.Texts())
	}
	// Opening it again only switches to it.
	h.a.userOpen(pa)
	if len(h.a.focus.order) != 1 {
		t.Errorf("tabs %v", h.a.focus.order)
	}
	h.open("b.go")
	h.open("c.go")
	if got := names(h.a.focus.order); got != "a.go b.go c.go" {
		t.Errorf("tabs %s", got)
	}
	h.a.userActivate(pa)
	h.tt.Frame()
	// A new tab opens to the right of the active one.
	h.open("x/util.go")
	if got := names(h.a.focus.order); got != "a.go util.go b.go c.go" {
		t.Errorf("tabs %s", got)
	}
	// Same names show their folder.
	h.open("y/util.go")
	if !h.tt.HasText("x") || !h.tt.HasText("y") {
		t.Errorf("folders of same-named tabs missing: %q", h.tt.Texts())
	}
	// Closing goes back to the most recently used tab.
	h.a.userActivate(filepath.Join(root, "c.go"))
	h.a.userActivate(pa)
	h.tt.Frame()
	h.tt.Key(ui.Cmd, ui.KeyW)
	if h.a.activePath() != filepath.Join(root, "c.go") || h.a.docs[pa] != nil {
		t.Errorf("after closing a.go: active %q", h.a.activePath())
	}
	// The tab's close button.
	h.click("Close c.go")
	if h.a.activePath() != filepath.Join(root, "y", "util.go") {
		t.Errorf("after closing c.go: active %q", h.a.activePath())
	}
	// Clicking the file chosen in the tree opens it again.
	h.click("b.go")
	h.settle(func() bool { d := h.a.docs[filepath.Join(root, "b.go")]; return d != nil && !d.loading })
	h.a.closeDoc(h.a.activePath())
	h.tt.Frame()
	h.click("b.go")
	if h.a.docs[filepath.Join(root, "b.go")] == nil {
		t.Error("clicking the chosen file again did not reopen it")
	}
	h.settle(func() bool { return !h.a.docs[filepath.Join(root, "b.go")].loading })
	for len(h.a.focus.order) > 0 {
		h.a.closeDoc(h.a.activePath())
	}
	h.tt.Frame()
	if !h.tt.HasText("No Editor") {
		t.Errorf("no empty state: %q", h.tt.Texts())
	}
}

func names(paths []string) string {
	var out []string
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	return strings.Join(out, " ")
}

func TestTabsKeepPlaceAndCycle(t *testing.T) {
	long := strings.Repeat("// line\n", 500)
	root := writeTree(t, map[string]string{"long.go": long, "b.go": "package b\n"})
	h := newHarness(t, root)
	d := h.open("long.go")
	h.ed(d).GoTo(editor.Pos{Line: 300, Col: 3})
	h.tt.Frame()
	h.open("b.go")
	h.tt.Key(ui.Ctrl, ui.KeyTab)
	if h.a.activePath() != d.path {
		t.Fatalf("Ctrl+Tab: active %q", h.a.activePath())
	}
	h.tt.Frame()
	if p := h.ed(d).CursorPos(); p != (editor.Pos{Line: 300, Col: 3}) {
		t.Errorf("place lost: %v", p)
	}
	if first, last := h.ed(d).VisibleLines(); 300 < first || 300 > last {
		t.Errorf("line 300 not in view: %d–%d", first, last)
	}
}

func TestUnreadableFiles(t *testing.T) {
	root := writeTree(t, map[string]string{
		"bin.dat":  "\x7fELF\x00\x00\x01",
		"latin.go": "// caf\xe9\n",
		"crlf.go":  "package a\r\n\r\nfunc 中() {}\r\n",
	})
	h := newHarness(t, root)
	binary := h.open("bin.dat")
	h.settle(func() bool { return binary.hex != nil && len(binary.hex.pages) > 0 })
	if !h.tt.HasText("Byte 0 7F") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	d := h.open("latin.go")
	if !h.tt.HasText("latin.go is not UTF-8 text") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	h.click("Show with Replacement Characters")
	h.settle(func() bool { return h.ed(d) != nil })
	if !strings.Contains(d.buf.String(), "caf�") {
		t.Errorf("text %q", d.buf.String())
	}
	d = h.open("crlf.go")
	if d.buf.LineEnding() != "\r\n" {
		t.Errorf("line ending %q", d.buf.LineEnding())
	}
	// A file gone before it loads shows why, with a retry.
	p := filepath.Join(root, "vanished.go")
	h.a.openFile(p, nil)
	h.settle(func() bool { return !h.a.docs[p].loading })
	if !h.tt.HasText("Could not open vanished.go") || !h.tt.HasText("Retry") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

const aGo = "package a\n\nimport \"example.com/w/b\"\n\nfunc main() {\n\t_ = \"🎉\"; b.名字()\n}\n"
const bGo = "package b\n\n// 名字 is 🎉.\nfunc 名字() {}\n"

func goWorkspace(t *testing.T) (string, *harness) {
	root := writeTree(t, map[string]string{"go.mod": "module example.com/w\n", "a.go": aGo, "b/b.go": bGo})
	h := newHarness(t, root)
	h.a.cfg.trust(h.a.root)
	return h.a.root, h
}

// at is the byte offset of the n-th byte of sub in d.
func at(d *doc, sub string) int { return strings.Index(d.buf.String(), sub) }

func TestDefinitionJumpBackForward(t *testing.T) {
	root, h := goWorkspace(t)
	bPath := filepath.Join(root, "b", "b.go")
	var asked lsp.Position
	h.fc.def = func(ctx context.Context, u string, pos lsp.Position) ([]lsp.Location, error) {
		asked = pos
		// 名字 in b.go, line 3: "func 名字() {}", UTF-16 character 5.
		return []lsp.Location{{URI: uriOf(bPath), Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 5}}}}, nil
	}
	a := h.open("a.go")
	off := at(a, "名字()")
	h.ed(a).GoTo(a.buf.Pos(off))
	h.ed(a).OnDefinition(off)
	h.settle(func() bool { return h.a.activePath() == bPath && h.ed(h.a.docs[bPath]) != nil })
	// "\t_ = \"🎉\"; b." is 1+4+1+4+1+2+2 bytes, 1+4+1+2+1+2+2 UTF-16 units.
	if asked != (lsp.Position{Line: 5, Character: 13}) {
		t.Errorf("asked at %+v", asked)
	}
	b := h.a.docs[bPath]
	if p := h.ed(b).CursorPos(); p != (editor.Pos{Line: 3, Col: 5}) {
		t.Errorf("landed at %v", p)
	}
	if got := strings.Join(h.fc.calls(), ","); !strings.HasPrefix(got, "open a.go,definition") {
		t.Errorf("calls %s", got)
	}
	if h.a.lspState != langserver.Ready {
		t.Errorf("state %v", h.a.lspState)
	}
	h.tt.Key(ui.Ctrl, ui.KeyMinus) // back
	if h.a.activePath() != a.path || h.ed(a).CursorPos() != a.buf.Pos(off) {
		t.Errorf("back: %q %v", h.a.activePath(), h.ed(a).CursorPos())
	}
	h.tt.Key(ui.Ctrl|ui.Shift, ui.KeyMinus) // forward
	if h.a.activePath() != bPath || h.ed(b).CursorPos() != (editor.Pos{Line: 3, Col: 5}) {
		t.Errorf("forward: %q %v", h.a.activePath(), h.ed(b).CursorPos())
	}
}

func TestStaleDefinitionIsDropped(t *testing.T) {
	root, h := goWorkspace(t)
	release, started := make(chan struct{}), make(chan struct{})
	first := true
	var mu sync.Mutex
	h.fc.def = func(ctx context.Context, u string, pos lsp.Position) ([]lsp.Location, error) {
		mu.Lock()
		slow := first
		first = false
		mu.Unlock()
		if slow {
			close(started)
			<-release // answers after the second request, as a slow server
			return []lsp.Location{{URI: uriOf(filepath.Join(root, "go.mod"))}}, nil
		}
		return []lsp.Location{{URI: uriOf(filepath.Join(root, "b", "b.go")), Range: lsp.Range{Start: lsp.Position{Line: 3}}}}, nil
	}
	a := h.open("a.go")
	h.ed(a).OnDefinition(at(a, "main"))
	<-started // the first request is with the server
	h.ed(a).OnDefinition(at(a, "名字()"))
	bPath := filepath.Join(root, "b", "b.go")
	h.settle(func() bool { return h.a.activePath() == bPath })
	h.drainNow()
	ran := h.ran
	close(release)
	// The first answer arrives late and must change nothing.
	h.settle(func() bool { return h.ran > ran })
	if h.a.activePath() != bPath || h.a.docs[filepath.Join(root, "go.mod")] != nil {
		t.Errorf("stale answer applied: active %q, tabs %s", h.a.activePath(), names(h.a.focus.order))
	}
	if len(h.a.hist.Back) != 1 {
		t.Errorf("history %v", h.a.hist.Back)
	}
}

// drainNow runs what is queued, without waiting for more.
func (h *harness) drainNow() {
	for {
		select {
		case fn := <-h.q:
			fn()
			h.ran++
		default:
			h.tt.Frame()
			return
		}
	}
}

func TestSwitchingTabsCancelsDefinition(t *testing.T) {
	root, h := goWorkspace(t)
	started := make(chan struct{})
	h.fc.def = func(ctx context.Context, u string, pos lsp.Position) ([]lsp.Location, error) {
		close(started)
		<-ctx.Done() // canceled when the user moves on
		return nil, ctx.Err()
	}
	h.open("go.mod")
	a := h.open("a.go")
	h.ed(a).OnDefinition(at(a, "名字()"))
	<-started
	h.a.userActivate(filepath.Join(root, "go.mod"))
	h.tt.Frame()
	if h.a.finding || h.a.notice != "" {
		t.Errorf("finding %v, notice %q", h.a.finding, h.a.notice)
	}
}

func TestSeveralDefinitions(t *testing.T) {
	root, h := goWorkspace(t)
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{
			{URI: uriOf(filepath.Join(root, "b", "b.go")), Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 5}}},
			{URI: uriOf(filepath.Join(root, "a.go")), Range: lsp.Range{Start: lsp.Position{Line: 4, Character: 5}}},
			{URI: "jar:file:///x.jar!/X.class"},
		}, nil
	}
	a := h.open("a.go")
	h.ed(a).OnDefinition(at(a, "名字()"))
	h.settle(func() bool { return h.a.choices != nil && h.a.choices.buffers != nil })
	if !h.tt.HasText("2 definitions") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	if _, ok := h.tt.Find("Close Definitions Preview"); !ok {
		t.Fatal("definition preview is missing")
	}
	h.click("a.go:5")
	if h.a.choices == nil || h.a.choices.selected != 1 || h.a.choices.previewEditor == nil {
		t.Fatal("selecting a definition did not show its preview")
	}
	h.click("a.go:5")
	if h.a.choices != nil || h.a.activePath() != a.path || h.ed(a).CursorPos() != (editor.Pos{Line: 4, Col: 5}) {
		t.Errorf("chose: %v %q %v", h.a.choices, h.a.activePath(), h.ed(a).CursorPos())
	}
}

func TestNoDefinitionAndNoGopls(t *testing.T) {
	_, h := goWorkspace(t)
	a := h.open("a.go")
	h.ed(a).OnDefinition(0)
	h.settle(func() bool { return !h.a.finding })
	if !h.tt.HasText("No definition found.") {
		t.Errorf("texts %q", h.tt.Texts())
	}

	root2 := writeTree(t, map[string]string{"m.go": "package m\n"})
	h2 := newHarness(t, root2)
	h2.a.cfg.trust(h2.a.root)
	h2.fc.startErr = errNoGopls
	m := h2.open("m.go")
	h2.ed(m).OnDefinition(0)
	h2.settle(func() bool { return !h2.a.finding && h2.a.lspState == langserver.Failed })
	if !strings.Contains(h2.a.notice, "needs gopls") || !h2.tt.HasText("Retry") {
		t.Errorf("notice %q, texts %q", h2.a.notice, h2.tt.Texts())
	}
	// Reading goes on, and Retry tries again.
	if h2.a.activePath() != m.path || h2.ed(m) == nil {
		t.Error("document hidden")
	}
	h2.fc.startErr = nil
	h2.click("Retry gopls")
	h2.ed(m).OnDefinition(0)
	h2.settle(func() bool { return !h2.a.finding && h2.a.lspState == langserver.Ready })
}

func TestTrustIsAskedFirst(t *testing.T) {
	root := writeTree(t, map[string]string{"m.go": "package m\n"})
	h := newHarness(t, root)
	m := h.open("m.go")
	h.ed(m).OnDefinition(0)
	h.tt.Frame()
	if !h.tt.HasText("Trust Folder") || len(h.fc.calls()) != 0 {
		t.Fatalf("texts %q, calls %v", h.tt.Texts(), h.fc.calls())
	}
	h.click("Trust Folder")
	h.settle(func() bool { return !h.a.finding && h.a.lspState == langserver.Ready })
	if !h.a.cfg.trusted(h.a.root) || len(h.fc.calls()) == 0 {
		t.Error("not trusted after Trust Folder")
	}
	// Not for files other than Go.
	h.open("m.go")
}

func TestFileChangesOnDisk(t *testing.T) {
	root := writeTree(t, map[string]string{"a.go": "package a\n\nvar x = 1\n", "dir/k.go": "package dir\n"})
	h := newHarness(t, root)
	d := h.open("a.go")
	h.ed(d).GoTo(editor.Pos{Line: 2, Col: 4})
	os.WriteFile(d.path, []byte("package a\n\nvar x = 2\n"), 0o644)
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileWrite, Path: "a.go"}})
	h.settle(func() bool { return strings.Contains(d.buf.String(), "x = 2") })
	if p := h.ed(d).CursorPos(); p != (editor.Pos{Line: 2, Col: 4}) {
		t.Errorf("place lost: %v", p)
	}
	// A new file shows in the tree.
	os.WriteFile(filepath.Join(root, "new.go"), nil, 0o644)
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileCreate, Path: "new.go"}})
	h.settle(func() bool { return !h.a.dirs[h.a.root].loading })
	if !h.tt.HasText("new.go") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	// Removed: the tab stays, marked.
	os.Remove(d.path)
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRemove, Path: "a.go"}})
	h.settle(func() bool { return !h.a.dirs[h.a.root].loading })
	if !d.gone || !h.tt.HasText("This file was deleted or moved on disk. The text below is how it was.") {
		t.Errorf("gone %v, texts %q", d.gone, h.tt.Texts())
	}
	// Back again, and Reload shows it.
	os.WriteFile(d.path, []byte("package a // back\n"), 0o644)
	h.click("Reload")
	h.settle(func() bool { return !d.loading })
	if d.gone || !strings.Contains(d.buf.String(), "back") {
		t.Errorf("gone %v, text %q", d.gone, d.buf.String())
	}
	// A directory renamed: its documents are gone, its cache dropped.
	k := h.open("dir/k.go")
	os.Rename(filepath.Join(root, "dir"), filepath.Join(root, "dir2"))
	h.a.applyEvents([]mygo.FileEvent{{Op: mygo.FileRename, Path: "dir2", OldPath: "dir", IsDir: true}})
	if !k.gone {
		t.Error("document in a renamed folder not gone")
	}
	h.settle(func() bool { return !h.a.dirs[h.a.root].loading })
	// The old folder's rows go, and nothing of it is read again.
	if h.treeHas(filepath.Join(root, "dir")) || !h.treeHas(filepath.Join(root, "dir2")) || h.tt.HasText("Could not read this folder: it does not exist") || !h.tt.HasText("dir2") {
		t.Errorf("texts %q", h.tt.Texts())
	}
	if _, ok := h.a.dirs[filepath.Join(root, "dir")]; ok {
		t.Error("the renamed folder's state stayed")
	}
}

func TestWorkspaceSwitchDropsOldWork(t *testing.T) {
	root, h := goWorkspace(t)
	release := make(chan struct{})
	h.fc.def = func(ctx context.Context, u string, pos lsp.Position) ([]lsp.Location, error) {
		<-release
		return []lsp.Location{{URI: uriOf(filepath.Join(root, "b", "b.go"))}}, nil
	}
	a := h.open("a.go")
	h.ed(a).OnDefinition(at(a, "名字()"))
	h.settle(func() bool { return h.a.lspState == langserver.Ready })
	other := writeTree(t, map[string]string{"o.txt": "other\n"})
	h.a.openWorkspace(other)
	close(release)
	h.settle(func() bool { d := h.a.dirs[h.a.root]; return d != nil && d.loaded })
	h.drainNow()
	if len(h.a.focus.order) != 0 || h.a.lspState != langserver.Off || !h.tt.HasText("o.txt") {
		t.Errorf("tabs %s, lsp %v, texts %q", names(h.a.focus.order), h.a.lspState, h.tt.Texts())
	}
	h.fc.mu.Lock()
	shut := h.fc.shutdown
	h.fc.mu.Unlock()
	if !shut {
		t.Error("gopls of the old workspace not shut down")
	}
}

func TestDirectoryErrorRetry(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	root := writeTree(t, map[string]string{"locked/x.go": "package x\n"})
	locked := filepath.Join(root, "locked")
	os.Chmod(locked, 0)
	defer os.Chmod(locked, 0o755)
	h := newHarness(t, root)
	h.click("locked")
	h.settle(func() bool { d := h.a.dirs[locked]; return d != nil && !d.loading })
	if !h.tt.HasText("Could not read this folder: permission denied") {
		t.Fatalf("texts %q", h.tt.Texts())
	}
	os.Chmod(locked, 0o755)
	h.click("Retry")
	h.settle(func() bool { return h.a.dirs[locked].loaded })
	if !h.tt.HasText("x.go") {
		t.Errorf("texts %q", h.tt.Texts())
	}
}

func TestConfigRoundTrip(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sub", "config.json")
	c := loadConfig(p)
	c.LastWorkspace = "/w"
	c.trust("/w")
	if err := c.save(); err != nil {
		t.Fatal(err)
	}
	c2 := loadConfig(p)
	if c2.LastWorkspace != "/w" || !c2.trusted("/w") {
		t.Errorf("%+v", c2)
	}
	os.WriteFile(p, []byte("{broken"), 0o600)
	if c3 := loadConfig(p); c3.LastWorkspace != "" || c3.path != p {
		t.Errorf("broken: %+v", c3)
	}
}

func TestDocPath(t *testing.T) {
	if got := workspace.DocPath("/tmp/w", "/private/tmp/w", "/private/tmp/w/a/b.go"); got != filepath.Clean("/tmp/w/a/b.go") {
		t.Errorf("got %q", got)
	}
	if got := workspace.DocPath("/tmp/w", "/private/tmp/w", "/usr/lib/go/src/fmt/print.go"); got != "/usr/lib/go/src/fmt/print.go" {
		t.Errorf("got %q", got)
	}
	if workspace.Within("/a/b", "/a/bc") || !workspace.Within("/a/b", "/a/b/c") || workspace.Within("/a/b", "/a") {
		t.Error("within")
	}
}

func TestF12GoesToDefinition(t *testing.T) {
	root, h := goWorkspace(t)
	bPath := filepath.Join(root, "b", "b.go")
	h.fc.def = func(context.Context, string, lsp.Position) ([]lsp.Location, error) {
		return []lsp.Location{{URI: uriOf(bPath), Range: lsp.Range{Start: lsp.Position{Line: 3, Character: 5}}}}, nil
	}
	a := h.open("a.go")
	h.ed(a).GoTo(a.buf.Pos(at(a, "名字()")))
	h.tt.Frame()
	if !h.ed(a).Focused() {
		t.Fatal("the editor of the tab shown has no focus")
	}
	h.tt.Key(0, ui.KeyF12)
	h.settle(func() bool { return h.a.activePath() == bPath })
	if len(h.a.hist.Back) != 1 || h.a.hist.Back[0].Path != a.path {
		t.Errorf("history %v", h.a.hist.Back)
	}
}

// serverOf is a language server whose client is c.
func serverOf(root string, c langserver.Client) *langserver.Server {
	return langserver.New(langserver.Options{Name: "gopls", Root: root, Start: func(context.Context, string, func(error)) (langserver.Client, error) { return c, nil }})
}

// shot draws the window into name in the directory $SEE_SHOT names, for a
// look; without one it does nothing.
func (h *harness) shot(name string) {
	dir := os.Getenv("SEE_SHOT")
	if dir == "" {
		return
	}
	h.tt.Frame()
	f, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		h.t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, h.tt.Image()); err != nil {
		h.t.Fatal(err)
	}
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
