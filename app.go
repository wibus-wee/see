package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/uri"
	"see/internal/workspace"

	"see/internal/langserver"
)

// app is the reader's state. It lives on the UI thread: background work
// hands its results back through post, which runs them there.
type app struct {
	post         func(func()) // runs fn on the UI thread, then redraws
	host         host         // the window and the desktop around it
	windowWidth  int
	windowHeight int
	startLSP     func(logf func(string, ...any)) langserver.Starter
	cfg          *config
	logf         func(string, ...any)

	// The workspace.
	root     string // as the user opened it
	realRoot string // with symlinks resolved, as servers may report it
	gen      int    // bumped when the workspace changes; late results of an old one are dropped
	wsCtx    context.Context
	wsCancel context.CancelFunc
	dirs     map[string]*dirState
	tree     ui.OutlineState[string]
	treeRow  int
	reveal   string // a path to choose in the tree once its row shows
	filter   treeFilter
	watchErr error
	watching bool
	git      gitState

	// Documents, by path, and the editor groups showing them.
	docs        map[string]*doc
	layout      *layout
	focus       *group
	nextGroup   int
	focusEditor *editor.State // an editor to give the keyboard to
	focusImage  string        // an image preview to give the keyboard to once loaded

	// Navigation.
	hist         nav.History
	navSeq       uint64 // bumped by every navigation the user asks for
	navCancel    context.CancelFunc
	finding      bool // a navigation request is on its way
	findingLabel string
	choices      *defChoices // several definitions to choose from

	lsp            *langserver.Server
	tsLSP          *langserver.Server
	tsState        langserver.State
	tsErr          error
	tsEpoch        uint64
	serverJob      bool
	serverJobErr   string
	lspState       langserver.State
	lspErr         error
	trustDismissed bool
	trustAsk       bool   // asking whether to trust the workspace
	afterTrust     func() // what to do once trusted
	notice         string // the last message for the user
	noticeError    bool

	hover         hoverState
	term          termPanel
	logs          logStore
	settings      settingsState
	find          findState
	quick         quickOpen
	confirm       *confirmation
	deleteConfirm *confirmation
	createEntry   *entryPrompt
	renameEntry   *renamePrompt
	allowDirty    bool
	quitApproved  bool

	navigator        int // the sidebar's navigator: navFiles or navFind
	sidebarOff       bool
	titleButton      ui.Handle // the toolbar's project name, over which its menu shows
	titleOpen        bool
	welcomeNote      string
	showWelcome      bool   // the welcome window over an open workspace
	version          string // the app's, shown on the welcome window
	sidebarW         float32
	saveTimer        *time.Timer // a settings save waiting for changes to stop
	restoringSession bool
	theme            *ui.Theme
	edTheme          *editor.Theme
	themeDark        bool
	themeAccent      ui.Color
	// setAppearance forces light or dark, or "" the system's; the app's
	// windows follow it.
}

const (
	navFiles = iota
	navFind
	navGit
)

// dirState is a directory of the tree: unread until opened.
type dirState struct {
	entries []workspace.Entry
	loading bool
	loaded  bool
	err     error
	seq     int
}

// doc is an open document, one per path, shared by the groups showing
// it: its buffer and how loading it went.
type doc struct {
	path, uri, name string
	untitled        bool
	cache           *draftCache
	saving          bool
	buf             *editor.Buffer
	image           *ui.Bitmap
	markdownOptions *markdown.Options
	markdownText    string
	markdownVersion uint64
	markdownCached  bool
	hex             *hexState

	loading           bool
	err               error
	kind              workspace.Kind
	size              int64
	plain             bool
	gone              bool      // removed or moved away on disk
	seq               int       // of the latest load, so a stale one is dropped
	stamp             fileStamp // the file as last read, to skip reading it again
	lossy             bool      // invalid UTF-8 shown with replacement characters
	bom               bool
	diskText          string
	conflict          bool
	lspVersion        uint64
	diagnostics       []editor.Diagnostic
	diagnosticVersion uint64
	gitBase           *editor.Buffer
	gitChanges        *editor.DiffState
	gitBaseSeq        uint64
	gitBaseKey        string // what the committed version was read for; see loadGitBase
}

type draftCache struct {
	mu      sync.Mutex
	version uint64
	removed bool
}

// fileStamp tells whether a file changed since it was read.
type fileStamp struct {
	size    int64
	modTime time.Time
}

// defChoices are the definitions a request found, to choose from.
type defChoices struct {
	g             *group
	from          nav.Entry
	enc           string
	locs          []target
	title         string
	selected      int
	previewEditor *editor.State
	previewPath   string
	buffers       map[string]*editor.Buffer
	collapsed     map[string]bool
}

type target struct {
	name         string
	path         string
	pos          lsp.Position
	preview      []string
	previewStart int
	end          lsp.Position
}

func newApp(cfg *config) *app {
	a := &app{
		cfg:      cfg,
		docs:     map[string]*doc{},
		dirs:     map[string]*dirState{},
		post:     func(fn func()) { fn() },
		host:     noHost{},
		startLSP: startGopls,
		logf:     log.Printf,
		treeRow:  -1,
		sidebarW: 260,
	}
	if cfg.SidebarWidth >= 160 {
		a.sidebarW = cfg.SidebarWidth
	}
	a.logf = func(format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		a.logs.add(slog.LevelInfo, "app", message)
		log.Print(message)
	}
	a.tree.List.Selected = &a.treeRow
	a.resetLayout()
	a.find.init()
	return a
}

func uriOf(path string) string { return uri.FromPath(path) }

// note shows a message in the activity view.
func (a *app) note(msg string, isErr bool) { a.notice, a.noticeError = msg, isErr }

// activePath is the focused group's active document.
func (a *app) activePath() string {
	if isDiffTab(a.focus.active) {
		return ""
	}
	return tabPath(a.focus.active)
}

// activeEditor is the editor of the focused group's active document.
func (a *app) activeEditor() *editor.State {
	if isMarkdownTab(a.focus.active) {
		return nil
	}
	return a.focus.eds[a.focus.active]
}

// ----- Workspace -----

// openFolderDialog asks for a folder, off the UI thread, and opens it.
func (a *app) openFolderDialog() {
	pick := a.host.pickFolder
	go func() {
		p, err := pick()
		a.post(func() {
			switch {
			case err != nil:
				a.note("Could not show the folder dialog: "+err.Error(), true)
			case p != "":
				a.openProject(p)
			}
		})
	}()
}

// openWorkspace closes the current workspace and opens dir: its first
// level of entries loads in the background, and nothing else is read
// until the user opens it.
func (a *app) openWorkspace(dir string) {
	root, err := workspace.CanonicalRoot(dir)
	if err != nil {
		a.welcomeNote = fmt.Sprintf("Could not open %s: %v", dir, describeErr(err))
		if errors.Is(err, fs.ErrNotExist) && slices.Contains(a.cfg.Recent, dir) {
			a.cfg.removeRecent(dir)
			a.saveConfig()
		}
		return
	}
	if a.guardDirty(a.dirtyDocs(), func() { a.openWorkspace(root) }) {
		return
	}
	a.closeWorkspace()
	a.root = root
	a.realRoot = root
	if r, err := filepath.EvalSymlinks(root); err == nil {
		a.realRoot = r
	}
	a.welcomeNote = ""
	a.showWelcome = false
	a.wsCtx, a.wsCancel = context.WithCancel(context.Background())
	gen := a.gen
	a.lsp = langserver.New(langserver.Options{
		Name:  "gopls",
		Root:  root,
		Start: a.startLSP(a.serverLog("gopls")),
		OnState: func(s langserver.State, err error) {
			a.post(func() {
				if a.gen == gen {
					a.lspState, a.lspErr = s, err
					a.serverStateLog("gopls", s, err)
				}
			})
		},
		OnDiagnostics: a.diagnosticsHandler(gen),
		Logf:          a.serverLog("gopls"),
	})
	a.resetTypeScriptServer()
	a.loadDir(root)
	a.startWatch()
	a.startGit()
	a.restoreSession()
	a.windowWidth, a.windowHeight = workspaceSize(a.cfg.Workspaces[root])
	a.host.setWorkspaceSize(a.windowWidth, a.windowHeight)
	a.cfg.LastWorkspace = root
	a.cfg.addRecent(root)
	a.saveConfig()
	a.host.setTitle(filepath.Base(root))
}

// closeWorkspace forgets the workspace: its tabs, caches, searches, watch
// and language server.
func (a *app) closeWorkspace() {
	a.createEntry = nil
	a.renameEntry = nil
	a.deleteConfirm = nil
	if a.root != "" {
		a.saveConfig()
	}
	a.gen++
	if a.wsCancel != nil {
		a.wsCancel()
	}
	a.cancelNav()
	a.closeTerminals()
	if a.lsp != nil {
		a.lsp.Stop()
		a.lsp = nil
	}
	if a.tsLSP != nil {
		a.tsLSP.Stop()
		a.tsLSP = nil
	}
	a.tsState, a.tsErr = langserver.Off, nil
	a.root, a.realRoot = "", ""
	a.dirs = map[string]*dirState{}
	a.tree = ui.OutlineState[string]{}
	a.treeRow = -1
	a.tree.List.Selected = &a.treeRow
	a.reveal = ""
	a.filter = treeFilter{}
	a.docs = map[string]*doc{}
	a.resetLayout()
	a.hist.Clear()
	a.choices = nil
	a.trustAsk, a.afterTrust = false, nil
	a.trustDismissed = false
	a.lspState, a.lspErr = langserver.Off, nil
	a.watchErr, a.watching = nil, false
	a.git = gitState{}
	a.notice = ""
	a.hover = hoverState{}
	a.find.reset()
	a.quick = quickOpen{}
	a.host.setTitle("See")
}

// saveConfigSoon saves the settings once they stop changing for a
// moment, as while the sidebar's divider is dragged.
func (a *app) saveConfigSoon() {
	if a.restoringSession {
		return
	}
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(500*time.Millisecond, func() { a.post(a.saveConfig) })
}

func (a *app) saveConfig() {
	if a.saveTimer != nil {
		a.saveTimer.Stop()
		a.saveTimer = nil
	}
	if a.root != "" && !a.restoringSession {
		a.captureSession()
	}
	if err := a.cfg.save(); err != nil {
		a.logf("saving settings: %v", err)
	}
}

// loadDir reads a directory's entries in the background.
func (a *app) loadDir(dir string) {
	d := a.dirs[dir]
	if d == nil {
		d = &dirState{}
		a.dirs[dir] = d
	}
	if d.loading {
		return
	}
	d.loading = true
	d.seq++
	seq, gen, ctx := d.seq, a.gen, a.wsCtx
	go func() {
		es, err := workspace.ReadDir(ctx, dir)
		a.post(func() {
			if a.gen != gen || a.dirs[dir] != d || d.seq != seq {
				return
			}
			d.loading = false
			if err != nil {
				d.err = err
				return
			}
			d.entries, d.err, d.loaded = es, nil, true
		})
	}()
}

// children lists a tree item's children: nil for a file, an empty list
// for a closed directory, and for an open one its entries, or a
// placeholder while they load or when reading them failed. Opening a
// directory is what reads it.
func (a *app) children(p string) []string {
	if a.filtering() {
		return a.filter.tree[p]
	}
	if isPlaceholder(p) {
		return nil
	}
	if e, ok := a.entry(p); !ok || !e.Dir {
		return nil
	}
	d := a.dirs[p]
	if !a.tree.Open.Has(p) {
		if d != nil && d.loaded {
			return entryPaths(d)
		}
		return []string{}
	}
	if d == nil || (!d.loaded && !d.loading && d.err == nil) {
		a.loadDir(p)
		d = a.dirs[p]
	}
	if !d.loaded {
		if d.err != nil {
			return a.withEntryPrompt(p, []string{p + errorSuffix})
		}
		return a.withEntryPrompt(p, []string{p + loadingSuffix})
	}
	out := entryPaths(d)
	if d.err != nil {
		out = append(out, p+errorSuffix) // a refresh failed: keep the old list
	}
	return a.withEntryPrompt(p, out)
}

func entryPaths(d *dirState) []string {
	out := make([]string, len(d.entries))
	for i, e := range d.entries {
		out[i] = e.Path
	}
	return out
}

const (
	loadingSuffix = "\x00loading"
	errorSuffix   = "\x00error"
	entrySuffix   = "\x00new-entry"
)

func isPlaceholder(p string) bool {
	return strings.HasSuffix(p, loadingSuffix) || strings.HasSuffix(p, errorSuffix) || strings.HasSuffix(p, entrySuffix)
}

// entry finds the tree entry of a path in its loaded parent.
func (a *app) entry(p string) (workspace.Entry, bool) {
	d := a.dirs[filepath.Dir(p)]
	if d == nil {
		return workspace.Entry{}, false
	}
	for _, e := range d.entries {
		if e.Path == p {
			return e, true
		}
	}
	return workspace.Entry{}, false
}

// retryDir reads a directory that failed to read, again; one the state
// of which is gone by now is read afresh.
func (a *app) retryDir(dir string) {
	if d := a.dirs[dir]; d != nil {
		d.loading = false
	}
	a.loadDir(dir)
}

// refreshDir reads a loaded directory again, as after a change on disk.
func (a *app) refreshDir(dir string) {
	if d := a.dirs[dir]; d != nil && (d.loaded || d.err != nil) {
		d.loading = false
		a.loadDir(dir)
	}
}

// refreshAll reads every loaded directory and open document again, for
// when watching is not possible.
func (a *app) refreshAll() {
	a.refreshGit()
	a.invalidateHover()
	for dir := range a.dirs {
		a.refreshDir(dir)
	}
	for _, d := range a.docs {
		if !d.untitled {
			a.reloadDoc(d)
		}
	}
	a.quick.files = nil // listed again when next opened
	a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
}

// revealInTree opens the directories above p and chooses its row.
func (a *app) revealInTree(p string) {
	if a.root == "" || !workspace.Within(a.root, p) || p == a.root {
		return
	}
	for dir := filepath.Dir(p); dir != a.root && workspace.Within(a.root, dir); dir = filepath.Dir(dir) {
		a.tree.Open.Add(dir)
	}
	a.reveal = p
}

// ----- Documents -----

// openFile shows the document of path in the focused group, opening it
// if needed, at pos when given.
func (a *app) openFile(path string, pos *editor.Pos) *doc {
	return a.openIn(a.focus, path, goTo{pos: pos})
}

// openIn shows path in g, at the place to says, if any.
func (a *app) openIn(g *group, path string, to goTo) *doc {
	a.term.maximized = false
	a.git.diffSeq++
	a.git.peek = nil
	path = workspace.DocPath(a.root, a.realRoot, path)
	d := a.docs[path]
	if d == nil {
		d = &doc{path: path, uri: uriOf(path), name: filepath.Base(path)}
		a.docs[path] = d
		a.loadDoc(d)
	}
	if !g.has(path) {
		// Open to the right of the active tab, as Xcode and VS Code do.
		at := len(g.order)
		if i := slices.Index(g.order, g.active); i >= 0 {
			at = i + 1
		}
		g.addTab(path, at)
	}
	if to.pos != nil || to.lsp != nil {
		delete(g.markdownPreview, path)
		g.pending[path] = to
	}
	a.activateIn(g, path)
	a.showLoaded(g, d)
	return d
}

// activate makes a tab of the focused group the active one.
func (a *app) activate(path string) { a.activateIn(a.focus, path) }

// closeDoc closes a tab of the focused group.
func (a *app) closeDoc(path string) { a.closeTab(a.focus, path) }

// showLoaded gives g an editor of d once d's text is there, and goes
// where g was asked to go in it.
func (a *app) showLoaded(g *group, d *doc) {
	if d.buf == nil || !g.hasDocument(d.path) {
		return
	}
	st := g.eds[d.path]
	if st == nil {
		st = a.newEditor(g, d)
		g.eds[d.path] = st
		if m := a.serverFor(d.path); m != nil && !a.cfg.trusted(a.root) && !a.trustDismissed {
			a.trustAsk = true
		}
		if m := a.serverFor(d.path); m != nil && a.cfg.trusted(a.root) {
			ctx := a.wsCtx
			snap, docURI := d.buf.Snapshot(), d.uri
			go a.openLanguageDocument(m, ctx, d, docURI, snap)
		}
	}
	to, ok := g.pending[d.path]
	if !ok {
		return
	}
	delete(g.pending, d.path)
	var p editor.Pos
	switch {
	case to.lsp != nil:
		line := min(max(to.lsp.Line, 0), d.buf.LineCount()-1)
		p = editor.Pos{Line: line, Col: lsp.ColumnFromLSP(d.buf.Line(line), to.lsp.Character, to.enc)}
	case to.pos != nil:
		p = clampPos(d.buf, *to.pos)
	default:
		return
	}
	st.GoTo(p)
	if to.selLen > 0 {
		off := d.buf.Offset(p)
		st.Select(off, min(off+to.selLen, d.buf.Len()))
	}
	if g == a.focus {
		a.focusEditor = st
	}
}

// loadDoc reads a document's file in the background.
func (a *app) loadDoc(d *doc) {
	d.loading, d.err = true, nil
	d.seq++
	seq, gen := d.seq, a.gen
	lossy, needBuf, stamp, untitled := d.lossy, d.buf == nil, d.stamp, d.untitled
	path, root, name := d.path, a.root, d.name
	var snap editor.Snapshot
	if !needBuf {
		snap = d.buf.Snapshot() // cheap: a version of the rope
	}
	go func() {
		if !needBuf {
			// A reload: skip files whose size and time are as they were,
			// without reading them.
			if fi, err := os.Stat(path); err == nil && (fileStamp{fi.Size(), fi.ModTime()}) == stamp {
				a.post(func() {
					if a.gen == gen && a.docs[d.path] == d && d.seq == seq {
						d.loading, d.gone, d.err = false, false, nil
					}
				})
				return
			}
		}
		f, err := workspace.ReadFile(path)
		var recovery *recoveryRecord
		if needBuf && !untitled {
			recovery = readRecovery(root, path)
		}
		var picture *ui.Bitmap
		if err == nil && f.Kind != workspace.TooLarge && isImagePath(path) {
			picture, err = readImage(path)
		}
		var buf *editor.Buffer
		if err == nil && f.Kind == workspace.BadEncoding && lossy {
			f = readLossy(path, f)
		}
		if err == nil && picture == nil && f.Kind == workspace.Text && needBuf {
			// Build the buffer here, off the UI thread: it is safe from
			// any goroutine, and a big file takes a while.
			buf = editor.NewBuffer(f.Text)
			if untitled && f.Text != "" {
				buf = editor.NewBuffer("")
				buf.Set(f.Text)
			}
			if !f.Plain {
				buf.SetLanguage(editor.LanguageFor(name))
			}
		}
		// Compare with what is shown here, off the UI thread, so the UI
		// thread replaces the text only when it changed.
		same := err == nil && f.Kind == workspace.Text && !needBuf && snapEquals(snap, f.Text)
		a.post(func() {
			if a.gen != gen || a.docs[d.path] != d || d.seq != seq {
				return
			}
			if d.buf != nil && d.buf.Version() != snap.Version() {
				d.loading = false
				a.reloadDoc(d)
				return
			}
			if picture != nil {
				f.Kind = workspace.Binary
			}
			if recovery != nil && err == nil && f.Kind == workspace.Text && recovery.Text == f.Text {
				a.removeRecovery(d)
				recovery = nil
			}
			if recovery != nil && (err == nil && f.Kind == workspace.Text || errors.Is(err, fs.ErrNotExist)) {
				if err != nil {
					f = workspace.File{Kind: workspace.Text, Text: recovery.Base, BOM: recovery.BOM}
					buf = editor.NewBuffer(f.Text)
				}
				if recovery.Text != f.Text || err != nil {
					buf.Set(recovery.Text)
				}
				a.loaded(d, f, buf, false, nil)
				d.diskText, d.bom = recovery.Base, recovery.BOM
				d.conflict = err == nil && (f.Text != recovery.Base || f.BOM != recovery.BOM)
				d.gone = errors.Is(err, fs.ErrNotExist)
			} else {
				a.loaded(d, f, buf, same, err)
			}
			if err == nil && (d.buf == nil || !d.buf.Modified()) {
				d.hex = nil
				d.image = picture
			}
		})
	}()
}

// loaded takes a document's file as read.
func (a *app) loaded(d *doc, f workspace.File, buf *editor.Buffer, same bool, err error) {
	d.loading = false
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && d.buf != nil {
			d.gone = true // keep showing what it was
			return
		}
		d.err = err
		return
	}
	if d.buf != nil && d.buf.Modified() {
		if f.Kind != workspace.Text || f.Text != d.diskText || f.BOM != d.bom {
			d.conflict = true
		}
		return
	}
	d.err, d.gone, d.conflict = nil, false, false
	d.bom, d.diskText = f.BOM, f.Text
	d.kind, d.size, d.plain = f.Kind, f.Size, f.Plain
	d.stamp = fileStamp{f.Size, f.ModTime}
	if f.Kind != workspace.Text {
		return
	}
	if d.buf == nil {
		d.buf = buf
	} else if !same {
		// Changed on disk: show the new text, each editor keeping its
		// place.
		type place struct {
			pos editor.Pos
			y   float64
		}
		places := map[*editor.State]place{}
		for _, g := range a.groups() {
			if st := g.eds[d.path]; st != nil {
				places[st] = place{st.CursorPos(), st.ScrollY()}
			}
		}
		d.buf.Reset(f.Text)
		if !f.Plain && d.buf.Language() == nil {
			d.buf.SetLanguage(editor.LanguageFor(d.name))
		}
		for st, p := range places {
			st.GoTo(clampPos(d.buf, p.pos))
			st.SetScrollY(p.y)
		}
		if m := a.serverFor(d.path); m != nil {
			m.Changed(d.uri, d.buf.Snapshot())
		}
	}
	for _, g := range a.groups() {
		a.showLoaded(g, d)
	}
}

// snapEquals reports whether a snapshot holds text, a chunk at a time,
// without copying the snapshot into a string.
func snapEquals(s editor.Snapshot, text string) bool {
	if s.Len() != len(text) {
		return false
	}
	off, eq := 0, true
	s.Chunks(0, s.Len(), func(chunk string) bool {
		if text[off:off+len(chunk)] != chunk {
			eq = false
			return false
		}
		off += len(chunk)
		return true
	})
	return eq
}

// readLossy reads a file that is not valid UTF-8 with its invalid bytes
// shown as replacement characters. UTF-16 stays unsupported.
func readLossy(path string, f workspace.File) workspace.File {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > workspace.MaxSize || bytes.HasPrefix(data, []byte{0xff, 0xfe}) || bytes.HasPrefix(data, []byte{0xfe, 0xff}) {
		return f
	}
	f.Text, f.Kind = strings.ToValidUTF8(string(data), "�"), workspace.Text
	f.Plain = len(data) > workspace.PlainLimit
	return f
}

// reloadDoc reads an open document's file again.
func (a *app) reloadDoc(d *doc) {
	if !d.loading {
		a.loadDoc(d)
	}
}

func clampPos(b *editor.Buffer, p editor.Pos) editor.Pos {
	if p.Line >= b.LineCount() {
		p.Line = max(0, b.LineCount()-1)
	}
	if p.Line < 0 {
		p.Line = 0
	}
	p.Col = min(max(p.Col, 0), len(b.Line(p.Line)))
	return p
}

// newEditor makes an editor of a document in a group.
func (a *app) newEditor(g *group, d *doc) *editor.State {
	st := &editor.State{
		Buffer:   d.buf,
		ReadOnly: d.lossy,
		Label:    d.name,
		Bindings: editorBindings(),
		Theme:    a.edTheme,
		DiffBase: d.gitBase,
	}
	a.applyEditorPrefs(st)
	if d.buf.Version() == d.diagnosticVersion {
		st.SetDiagnostics(d.diagnosticVersion, d.diagnostics)
	}
	st.OnDefinition = func(offset int) { a.goToDefinition(g, d, offset) }
	a.loadGitBase(d)
	st.ContextMenu = func(menu *ui.Menu) {
		if menu.Item("Find References").Shortcut(ui.Shift, ui.KeyF12).Chosen() {
			a.focus = g
			a.findUsagesAt(false, g.pointerAnchor, g.pointerInEditor)
		}
		if menu.Item("Find Callers").Chosen() {
			a.focus = g
			a.findUsagesAt(true, g.pointerAnchor, g.pointerInEditor)
		}
	}
	st.OnHover = func(r editor.HoverRequest) { a.hoverAt(d, st, r) }
	st.RenderHover = a.renderHover
	return st
}

// ----- Navigation -----

// cancelNav makes the navigation on its way, if any, stale.
func (a *app) cancelNav() {
	a.navSeq++
	if a.navCancel != nil {
		a.navCancel()
		a.navCancel = nil
	}
	a.finding = false
	a.choices = nil
}

// here is the reading position of the focused group's document.
func (a *app) here() (nav.Entry, bool) {
	path := a.activePath()
	if path == "" {
		return nav.Entry{}, false
	}
	st := a.activeEditor()
	if st == nil {
		return nav.Entry{Path: path}, true
	}
	p := st.CursorPos()
	return nav.Entry{Path: path, Line: p.Line, ByteColumn: p.Col}, true
}

// userOpen opens a file because the user asked, as from the tree.
func (a *app) userOpen(path string) {
	a.cancelNav()
	a.openFile(path, nil)
	a.focusImage = ""
	if isImagePath(a.activePath()) {
		a.focusImage = a.activePath()
	}
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}

func (a *app) previewOpen(path string) {
	g := a.focus
	path = workspace.DocPath(a.root, a.realRoot, path)
	if g.has(path) {
		a.userActivateIn(g, path)
		a.focusImage = ""
		return
	}
	old := g.preview
	if d := a.docs[old]; d != nil && d.buf != nil && d.buf.Modified() {
		g.preview, old = "", ""
	}
	at := slices.Index(g.order, old)
	a.userOpen(path)
	a.focusImage = "" // a single-click preview keeps focus in the tree
	if old != "" {
		a.closeTab(g, old)
		g.addTab(path, at)
	}
	g.preview = path
}

// userActivate switches tabs of the focused group because the user asked.
func (a *app) userActivate(path string) { a.userActivateIn(a.focus, path) }

func (a *app) userActivateIn(g *group, path string) {
	a.term.maximized = false
	a.git.diffSeq++
	a.git.peek = nil
	if path == g.active && g == a.focus {
		return
	}
	a.cancelNav()
	a.activateIn(g, path)
	a.focusImage = ""
	if isImagePath(path) {
		a.focusImage = path
	}
	if st := g.eds[path]; st != nil {
		a.focusEditor = st
	}
}

func (a *app) goBack() {
	cur, ok := a.here()
	if !ok {
		return
	}
	if to, ok := a.hist.GoBack(cur); ok {
		a.cancelNav()
		a.openFile(to.Path, &editor.Pos{Line: to.Line, Col: to.ByteColumn})
	}
}

func (a *app) goForward() {
	cur, ok := a.here()
	if !ok {
		return
	}
	if to, ok := a.hist.GoForward(cur); ok {
		a.cancelNav()
		a.openFile(to.Path, &editor.Pos{Line: to.Line, Col: to.ByteColumn})
	}
}

// cycleTab activates the tab dir places to the right (1) or left (-1)
// in the focused group.
func (a *app) activateTabNumber(n int) {
	g := a.focus
	if g == nil || n < 1 || len(g.order) == 0 {
		return
	}
	i := n - 1
	if n == 9 {
		i = len(g.order) - 1
	}
	if i < len(g.order) {
		a.userActivate(g.order[i])
	}
}

func (a *app) cycleTab(dir int) {
	g := a.focus
	if len(g.order) < 2 {
		return
	}
	i := slices.Index(g.order, g.active)
	a.userActivate(g.order[(i+dir+len(g.order))%len(g.order)])
}

// goToDefinition asks the language server where the symbol at offset of d is defined
// and goes there, in g. Only the latest navigation the user asked for
// wins: a response to an older one is dropped.
func (a *app) goToDefinition(g *group, d *doc, offset int) {
	if d.buf == nil {
		return
	}
	a.focus = g
	m := a.serverFor(d.path)
	if m == nil {
		a.note("No language server is configured for this file.", false)
		return
	}
	if !a.cfg.trusted(a.root) {
		a.trustAsk = true
		a.afterTrust = func() { a.goToDefinition(g, d, offset) }
		return
	}
	a.cancelNav()
	a.hideLink()
	snap := d.buf.Snapshot()
	pos := snap.Pos(offset)
	from := nav.Entry{Path: d.path, Line: pos.Line, ByteColumn: pos.Col}
	line := snap.Line(pos.Line)
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = "Finding definition…"
	a.notice = ""
	go func() {
		defer cancel()
		locs, enc, err := definition(ctx, m, d.uri, snap, pos, line)
		targets, skipped := targetsOf(locs)
		a.post(func() {
			if a.gen != gen || a.navSeq != seq {
				return // the user has moved on
			}
			a.finding = false
			a.navCancel = nil
			a.definitionFound(g, from, targets, enc, skipped, err)
		})
	}()
}

func targetsOf(locs []lsp.Location) (targets []target, skipped int) {
	for _, l := range locs {
		p, err := uri.ToPath(l.URI)
		if err != nil {
			skipped++
			continue
		}
		targets = append(targets, target{name: l.Name, path: p, pos: l.Range.Start, end: l.Range.End})
	}
	return targets, skipped
}

func definition(ctx context.Context, m *langserver.Server, docURI string, snap editor.Snapshot, pos editor.Pos, line string) ([]lsp.Location, string, error) {
	c, err := m.Get(ctx)
	if err != nil {
		return nil, "", err
	}
	if err := m.Sync(ctx, c, docURI, snap); err != nil {
		return nil, "", err
	}
	enc := c.Encoding()
	locs, err := c.Definition(ctx, docURI, lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)})
	return locs, enc, err
}

func (a *app) definitionFound(g *group, from nav.Entry, targets []target, enc string, skipped int, err error) {
	switch {
	case err != nil:
		a.lspFailure(err)
	case len(targets) == 0 && skipped > 0:
		a.note("The definition is in a place that is not a local file.", false)
	case len(targets) == 0:
		a.note("No definition found.", false)
	case len(targets) == 1:
		a.jump(g, from, targets[0], enc)
	default:
		a.choices = &defChoices{g: g, from: from, enc: enc, locs: targets, title: "definitions"}
		a.git.peek = nil
		a.loadRelatedPreviews(a.choices)
	}
}

// lspFailure reports why a language server request failed.
func (a *app) lspFailure(err error) {
	switch {
	case errors.Is(err, errNoGopls):
		a.note("Go to Definition needs gopls: install it with “go install golang.org/x/tools/gopls@latest”, then Retry.", true)
	case errors.Is(err, errNoTypeScriptServer):
		a.note("Install the TypeScript language server in Settings, then retry.", true)
	case errors.Is(err, context.DeadlineExceeded):
		a.note("The language server took too long to answer. It may still be loading the workspace; try again.", true)
	case errors.Is(err, context.Canceled):
	default:
		a.note("Language server failed: "+err.Error(), true)
	}
}

// jump goes to a definition in g, recording where it came from.
func (a *app) jump(g *group, from nav.Entry, t target, enc string) {
	a.choices = nil
	a.hist.Push(from)
	pos := t.pos
	a.openIn(g, t.path, goTo{lsp: &pos, enc: enc})
}

// trust records that the user trusts the workspace to run language servers in.
func (a *app) trust() {
	a.cfg.trust(a.root)
	a.saveConfig()
	a.trustAsk = false
	a.openServerDocuments(nil)
	if fn := a.afterTrust; fn != nil {
		a.afterTrust = nil
		fn()
	}
}

func (a *app) retryLSP() {
	if a.lsp != nil {
		a.invalidateHover()
		a.lsp.Retry()
		a.lspState, a.lspErr = a.lsp.Status()
		a.notice = ""
		a.openServerDocuments(a.lsp)
	}
}

// describeErr phrases a file error for the user, without the path.
func describeErr(err error) string {
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "it does not exist"
	case errors.Is(err, fs.ErrPermission):
		return "permission denied"
	case errors.Is(err, workspace.ErrNotDir):
		return "it is not a folder"
	case errors.Is(err, workspace.ErrNotRegular):
		return "it is not a regular file"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}

func (a *app) serverFor(path string) *langserver.Server {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return a.lsp
	}
	if typeScriptLanguage(path) != "" {
		return a.tsLSP
	}
	return nil
}

func (a *app) retryTypeScriptLSP() {
	if a.tsLSP != nil {
		a.invalidateHover()
		a.tsLSP.Retry()
		a.tsState, a.tsErr = a.tsLSP.Status()
		a.notice = ""
		a.openServerDocuments(a.tsLSP)
	}
}

func (a *app) resetTypeScriptServer() {
	a.tsEpoch++
	a.tsState, a.tsErr = langserver.Off, nil
	a.tsLSP = nil
	if a.root == "" {
		return
	}
	gen, epoch := a.gen, a.tsEpoch
	a.tsLSP = newTypeScriptServer(a.root, startTypeScriptServer(a.serverLog("tsrs")), func(s langserver.State, err error) {
		a.post(func() {
			if a.gen == gen && a.tsEpoch == epoch {
				a.tsState, a.tsErr = s, err
				a.serverStateLog("tsrs", s, err)
			}
		})
	}, a.diagnosticsHandler(gen), a.serverLog("tsrs"))
}

func (a *app) openLanguageDocument(m *langserver.Server, ctx context.Context, d *doc, docURI string, snap editor.Snapshot) {
	client, err := m.Get(ctx)
	if err != nil {
		return
	}
	if err = m.Sync(ctx, client, docURI, snap); err != nil {
		return
	}
	a.post(func() {
		if d.uri == docURI && a.docs[d.path] == d && a.serverFor(d.path) == m && d.buf != nil && d.buf.Version() != snap.Version() {
			m.Changed(docURI, d.buf.Snapshot())
		}
	})
}

func (a *app) openServerDocuments(only *langserver.Server) {
	if a.wsCtx == nil || !a.cfg.trusted(a.root) {
		return
	}
	for _, d := range a.docs {
		if d.buf == nil {
			continue
		}
		m := a.serverFor(d.path)
		if m == nil || (only != nil && only != m) {
			continue
		}
		go a.openLanguageDocument(m, a.wsCtx, d, d.uri, d.buf.Snapshot())
	}
}

// findUsages requests references or incoming calls for the focused symbol.
func (a *app) findUsages(callers bool) {
	g := a.focus
	if g == nil {
		return
	}
	a.findUsagesAt(callers, g.caretAnchor, true)
}

func (a *app) findUsagesAt(callers bool, anchor ui.Point, validAnchor bool) {
	g := a.focus
	if g == nil {
		return
	}
	d := a.docs[g.active]
	st := g.eds[g.active]
	if d == nil || d.buf == nil || st == nil {
		return
	}
	m := a.serverFor(d.path)
	if m == nil {
		a.note("No language server is configured for this file.", false)
		return
	}
	if !a.cfg.trusted(a.root) {
		a.trustAsk = true
		a.afterTrust = func() { a.findUsagesAt(callers, anchor, validAnchor) }
		return
	}
	a.term.maximized = false
	a.cancelNav()
	a.hideLink()
	snap := d.buf.Snapshot()
	pos := st.CursorPos()
	from := nav.Entry{Path: d.path, Line: pos.Line, ByteColumn: pos.Col}
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = "Finding references…"
	if callers {
		a.findingLabel = "Finding callers…"
	}
	a.notice = ""
	seq, gen := a.navSeq, a.gen
	go func() {
		defer cancel()
		c, err := m.Get(ctx)
		var locations []lsp.Location
		enc := ""
		if err == nil {
			err = m.Sync(ctx, c, d.uri, snap)
		}
		if err == nil {
			enc = c.Encoding()
			at := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(snap.Line(pos.Line), pos.Col, enc)}
			if callers {
				if client, ok := c.(interface {
					Callers(context.Context, string, lsp.Position) ([]lsp.Location, error)
				}); ok {
					locations, err = client.Callers(ctx, d.uri, at)
				} else {
					err = errors.New("this language server does not support finding callers")
				}
			} else {
				if client, ok := c.(interface {
					References(context.Context, string, lsp.Position) ([]lsp.Location, error)
				}); ok {
					locations, err = client.References(ctx, d.uri, at)
				} else {
					err = errors.New("this language server does not support references")
				}
			}
		}
		targets, skipped := targetsOf(locations)
		a.post(func() {
			if a.gen != gen || a.navSeq != seq {
				return
			}
			a.finding = false
			a.navCancel = nil
			if err != nil {
				a.lspFailure(err)
				return
			}
			name := "references"
			if callers {
				name = "callers"
			}
			if len(targets) == 0 {
				if skipped > 0 {
					a.note("Results are not in local files.", false)
				} else {
					a.note("No "+name+" found.", false)
				}
				return
			}
			a.choices = &defChoices{g: g, from: from, enc: enc, locs: targets, title: name}
			a.git.peek = nil
			a.loadRelatedPreviews(a.choices)
		})
	}()
}

func (a *app) loadRelatedPreviews(ch *defChoices) {
	// Snapshots preserve unsaved edits without reading editor state in a worker.
	snapshots := make(map[string]editor.Snapshot)
	for _, t := range ch.locs {
		path := workspace.DocPath(a.root, a.realRoot, t.path)
		if d := a.docs[path]; d != nil && d.buf != nil {
			snapshots[t.path] = d.buf.Snapshot()
		}
	}
	targets := slices.Clone(ch.locs)
	ctx, gen := a.wsCtx, a.gen
	go func() {
		files := make(map[string][]string)
		buffers := make(map[string]*editor.Buffer)
		for i := range targets {
			if ctx.Err() != nil {
				return
			}
			t := &targets[i]
			if snap, ok := snapshots[t.path]; ok {
				if buffers[t.path] == nil {
					buffers[t.path] = editor.NewBuffer(snap.String())
					buffers[t.path].SetLanguage(editor.LanguageFor(t.path))
				}
				if t.pos.Line >= 0 && t.pos.Line < snap.LineCount() {
					t.previewStart = max(0, t.pos.Line-2)
					for line := t.previewStart; line < min(snap.LineCount(), t.pos.Line+3); line++ {
						t.preview = append(t.preview, snap.Line(line))
					}
				}
				continue
			}
			lines, ok := files[t.path]
			if !ok {
				if f, err := workspace.ReadFile(t.path); err == nil && f.Kind == workspace.Text {
					lines = strings.Split(f.Text, "\n")
					buffers[t.path] = editor.NewBuffer(f.Text)
					buffers[t.path].SetLanguage(editor.LanguageFor(t.path))
				}
				files[t.path] = lines
			}
			if t.pos.Line >= 0 && t.pos.Line < len(lines) {
				t.previewStart = max(0, t.pos.Line-2)
				t.preview = slices.Clone(lines[t.previewStart:min(len(lines), t.pos.Line+3)])
			}
		}
		a.post(func() {
			if a.gen == gen && a.choices == ch {
				ch.locs = targets
				ch.buffers = buffers
				a.selectRelated(ch, ch.selected)
			}
		})
	}()
}

func (a *app) selectRelated(ch *defChoices, index int) {
	if index < 0 || index >= len(ch.locs) {
		return
	}
	ch.selected = index
	t := ch.locs[index]
	buf := ch.buffers[t.path]
	if buf == nil {
		ch.previewEditor = nil
		ch.previewPath = ""
		return
	}
	if ch.previewEditor == nil || ch.previewPath != t.path {
		label := "Reference code preview"
		if ch.title == "definitions" {
			label = "Definition code preview"
		}
		ch.previewEditor = &editor.State{Buffer: buf, ReadOnly: true, Label: label, Theme: a.edTheme}
		a.applyEditorPrefs(ch.previewEditor)
		ch.previewPath = t.path
	}
	line := min(max(t.pos.Line, 0), buf.LineCount()-1)
	col := lsp.ColumnFromLSP(buf.Line(line), t.pos.Character, ch.enc)
	ch.previewEditor.LineBackgrounds = []editor.LineBackground{{First: line, Last: line, Color: a.colors().pill}}
	ch.previewEditor.GoTo(editor.Pos{Line: line, Col: col})
	if t.end.Line == t.pos.Line && t.end.Character > t.pos.Character {
		end := lsp.ColumnFromLSP(buf.Line(line), t.end.Character, ch.enc)
		ch.previewEditor.TextBackgrounds = []editor.TextBackground{{Start: buf.Offset(editor.Pos{Line: line, Col: col}), End: buf.Offset(editor.Pos{Line: line, Col: end}), Color: a.colors().match}}
	} else {
		ch.previewEditor.TextBackgrounds = nil
	}
}

func (a *app) openProject(path string) {
	if a.root != "" && a.host.openProjectWindow(path) {
		return
	}
	a.openWorkspace(path)
}
