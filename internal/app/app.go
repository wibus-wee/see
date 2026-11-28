package app

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
	"see/internal/languages"
	"see/locales"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown"

	"see/internal/config"
	"see/internal/filenesting"
	"see/internal/fileobs"
	"see/internal/git"
	"see/internal/localhistory"
	"see/internal/lsp"
	"see/internal/nav"
	"see/internal/operr"
	"see/internal/proc"
	"see/internal/uri"
	"see/internal/workspace"

	"see/internal/langserver"
	"see/internal/logs"
	"see/internal/platform"
)

// poster routes work to the UI thread through a Scheduler that may be
// swapped after the app starts: main binds it to a window once the window
// exists, and tests redirect it to watch posts. The atomic load keeps
// Post safe when a swap races a background goroutine's read; a goroutine
// may still post once through the scheduler it already loaded.
type poster struct {
	s atomic.Pointer[platform.Scheduler]
}

func newPoster(s platform.Scheduler) *poster {
	p := &poster{}
	p.set(s)
	return p
}

// Post hands fn to the current scheduler.
func (p *poster) Post(fn func()) {
	if s := p.load(); s != nil {
		s.Post(fn)
		return
	}
	fn()
}

func (p *poster) set(s platform.Scheduler) { p.s.Store(&s) }

// load returns the current scheduler, or nil when none was ever set.
func (p *poster) load() platform.Scheduler {
	if p == nil {
		return nil
	}
	if s := p.s.Load(); s != nil {
		return *s
	}
	return nil
}

// App is the reader's state. It lives on the UI thread: background work
// hands its results back through post, which runs them there.
type App struct {
	post                *poster // runs fn on the UI thread, then redraws
	trustChanged        func()
	editorPrefsChanged  func()
	gitDiscoveryChanged func()
	vibrancyChanged     func()
	host                platform.Desktop // the window and the desktop around it
	windowWidth         int
	windowHeight        int
	startLSP            func(logf func(string, ...any)) langserver.Starter
	cfg                 *config.Config

	diffWhitespaceHidden bool

	logf func(string, ...any)

	// The workspace.
	root                string // the first folder root
	realRoot            string // with symlinks resolved, as servers may report it
	workspaceID         string // folder or .code-workspace path used for saved state
	workspaceFolders    []workspace.Folder
	workspaceChanged    bool
	workspaceSettings   workspace.Settings
	workspaceRootsEpoch uint64
	gen                 int // bumped when the workspace changes; late results of an old one are dropped
	wsCtx               context.Context
	wsCancel            context.CancelFunc
	dirs                map[string]*dirState
	tree                ui.OutlineState[string]
	outline             outlineState
	calls               calls
	treeRow             int
	reveal              string // a path to choose in the tree once its row shows
	filter              treeFilter
	watchErr            error
	watching            bool
	watchCancels        map[string]context.CancelFunc
	workspaceFileCancel context.CancelFunc
	ignored             map[string]*git.Ignored // what Git ignores under each folder root
	git                 gitState

	// Documents, by path, and the editor groups showing them.
	docs              map[string]*doc
	localHistoryStore *localhistory.Store
	localHistoryErr   error
	// localHistoryWrites counts the captures whose store writes are in
	// flight. Tests drain it before their temporary directories go away:
	// a write landing in the teardown window can recreate a removed
	// directory, which the harness cannot see and the cleanup cannot
	// remove.
	localHistoryWrites sync.WaitGroup
	layout            *layout
	maximized         *group
	recents           []string // the files activated lately, across groups, most recent last
	focus             *group
	focusGroupTarget  *group
	nextGroup         int
	focusEditor       *editor.State // an editor to give the keyboard to
	focusImage        string        // an image preview to give the keyboard to once loaded
	closed            []closedTab   // tabs Reopen Closed Tab can bring back, most recent last
	quickHelp         *editor.State
	quickHelpBounds   ui.Rect
	quickHelpRendered bool
	quickHelpRequest  *editor.State
	quickHelpSeq      uint64
	inspector         quickHelpInspector

	// Navigation.
	hist                nav.History
	bookmarks           nav.Bookmarks // the workspace's waypoints, by path and line
	navSeq              uint64        // bumped by every navigation the user asks for
	navCancel           context.CancelFunc
	finding             bool // a navigation request is on its way
	findingLabel        string
	navigationMenuState [8]bool
	scopeSeq            uint64
	scopeCancel         context.CancelFunc
	scopePendingState   *editor.State
	scopePendingGroup   *group
	scopePendingPath    string
	scopeModeState      *editor.State
	scopeModeGroup      *group
	scopeModePath       string
	selCancel           context.CancelFunc // a selection range request on its way
	selStack            []editor.Selection // the selections Expand Selection grew, for Shrink Selection
	choices             *defChoices        // several definitions to choose from
	counterpartMiss     counterpartFeedback
	peek                *peekState // a definition peeked at from an editor
	regexPlayground     *regexPlayground
	regexToolWindow     regexToolWindowState
	references          referenceList
	referencesEmpty     bool         // the last pin found no references
	codeActions         *codeActions // the code actions a request found, for its menu
	rename              *renameState // the symbol rename prompt, if open
	renameSeq           uint64
	renameCancel        context.CancelFunc

	lsp         *langserver.Server
	serverEpoch uint64
	cmakeLSP    *langserver.Server
	cmakeState  langserver.State
	cmakeErr    error
	clangLSP    *langserver.Server
	clangState  langserver.State
	clangErr    error
	tsLSP       *langserver.Server
	tsState     langserver.State
	tsErr       error
	tsEpoch     uint64
	// customLSPs are the configured language servers, by the index of
	// their config.LSP entry; customStates tracks each one's state.
	customLSPs     map[int]*langserver.Server
	customLSPSpecs []config.LSPConfig
	customCovers   map[int]func(path string) bool
	customStates   map[int]struct {
		state langserver.State
		err   error
	}
	serverJob      bool
	serverJobName  string // the tool an installation or a removal is of
	serverJobErr   string
	lspState       langserver.State
	lspErr         error
	trustDismissed bool
	trustAsk       bool   // asking whether to trust the workspace
	afterTrust     func() // what to do once trusted
	feedback       feedbackState

	symbolHighlights symbolHighlightState

	hover         hoverState
	signature     signatureState
	completions   map[int]*completionState
	task          *taskRun
	taskList      ui.ListState
	term          termPanel
	logs          logs.Store
	settings      settingsState
	find          findState
	problems      problemsState
	bookmarksUI   bookmarksState
	quick         quickOpen
	jumpDate      jumpToDate
	confirm       *confirmation
	deleteConfirm *confirmation
	createEntry   *entryPrompt
	renameEntry   *renamePrompt
	allowDirty    bool
	quitApproved  bool

	navigator             int // the sidebar's navigator
	sidebarOff            bool
	titleButton           ui.Handle // the toolbar's project name, over which its menu shows
	titleOpen             bool
	avatars               map[string]*avatarEntry // authors' pictures, by email
	services              ui.Services             // the window's clipboard and browser, for menus built without a context
	branchHandle          ui.Handle               // the toolbar's branch, under which the branch picker shows
	welcomeNote           string
	clone                 welcomeClone // the welcome window's Clone Repository dialog
	showWelcome           bool         // the welcome window over an open workspace
	hasWelcome            bool         // this app owns the welcome window
	version               string       // the app's, shown on the welcome window
	sidebarW              float32
	saveTimer             *time.Timer // a settings save waiting for changes to stop
	restoringSession      bool
	ownsStandaloneSession bool                // the primary window alone restores the empty-root session
	standaloneSessionDocs map[string]struct{} // recovery entries this window owns in the shared empty-root session
	discardedSessionDocs  map[string]struct{} // documents approved for discard until their workspace is gone
	theme                 *ui.Theme
	modifiers             ui.Modifiers  // keyboard modifiers held this frame
	edTheme               *editor.Theme // a stable object: applyAppearance replaces its contents
	themeDark             bool
	themeAccent           ui.Color
	themeGen              int // bumped when the themes' contents are replaced
	// setAppearance forces light or dark, or "" the system's; the app's
	// windows follow it.
}

const (
	navFiles = iota
	navFind
	navGit
	navProblems
	navOutline
	navBookmarks
)

// dirState is a directory of the tree: unread until opened.
type dirState struct {
	entries []workspace.Entry
	// nested are each nesting parent's children, the files the nesting
	// rules fold under it, by their paths; nil until entries are read.
	nested  map[string][]string
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
	colorCache      colorLiteralCache
	video           bool
	image           *ui.Bitmap
	markdownOptions *markdown.Options
	markdownText    string
	markdownVersion uint64
	markdownCached  bool
	hex             *hexState

	previewOpened     bool // previewed open: the editor's birth does not take the keyboard
	focusOnLoad       bool // the opener asked the keyboard once its editor exists
	loading           bool
	loadingVisible    bool
	loadingSeq        uint64
	loadingTimer      *time.Timer
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
	expectation       fileobs.Observation // the on-disk bytes this buffer saves against
	conflict          bool
	lspVersion        uint64
	diagnostics       []editor.Diagnostic
	diagnosticVersion uint64
	gitBase           *editor.Buffer
	gitChanges        *editor.DiffState
	gitBaseSeq        uint64
	gitBaseKey        string // what the committed version was read for; see loadGitBase
	blame             []git.BlameLine
	blameKey          string
	blameVersion      uint64
	blameReady        bool
	blameSeq          uint64
	blameTimer        *time.Timer
	blameCancel       context.CancelFunc
	languageInlays    []editor.Inlay
	readerCache       *readerCache
	parameterInlays   []editor.Inlay
	inlayVersion      uint64 // version of the cached hint offsets
	inlayKey          inlayKey
	inlaySeq          int
	inlayTimer        *time.Timer
	inlayCancel       context.CancelFunc
	docLinks          []docLink
	linkBufVer        uint64 // the buffer's version docLinks were built of
	linkLocal         bool   // docLinks came of the Markdown parser, not a server
	linkVersion       uint64 // the buffer's version links were last asked for
	linkSeq           int
	linkTimer         *time.Timer
	linkCancel        context.CancelFunc
	foldRanges        map[int]int // line its range starts on -> last it hides
	foldBufVer        uint64      // the buffer's version foldRanges came of
	foldVersion       uint64      // the buffer's version ranges were last asked for
	foldSeq           int
	foldTimer         *time.Timer
	foldCancel        context.CancelFunc
	foldFolder        *foldFolder // the language's Folder, once wrapped
	baseLang          *editor.Language
	wrappedLang       *editor.Language
	wrappedInlays     bool
	wrappedFold       bool // wrappedLang folds by the server's ranges
	manualLanguage    *editor.Language
	languageManual    bool
	detectedVersion   uint64
	detectedName      string
	languageChecked   bool
	localHistoryTimer *time.Timer
	localHistoryVer   uint64
	localHistorySaved time.Time
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
	navigation    string
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

func newApp(cfg *config.Config) *App {
	localHistoryStore, localHistoryErr := localhistory.NewCacheStore()
	a := &App{
		cfg:                   cfg,
		docs:                  map[string]*doc{},
		localHistoryStore:     localHistoryStore,
		localHistoryErr:       localHistoryErr,
		dirs:                  map[string]*dirState{},
		post:                  newPoster(platform.SchedulerFunc(func(fn func()) { fn() })),
		host:                  platform.NoDesktop{},
		startLSP:              languages.StartGopls,
		logf:                  log.Printf,
		treeRow:               -1,
		sidebarW:              260,
		ownsStandaloneSession: true,
		standaloneSessionDocs: make(map[string]struct{}),
		discardedSessionDocs:  make(map[string]struct{}),
	}
	if cfg.SidebarWidth >= 160 {
		a.sidebarW = cfg.SidebarWidth
	}
	a.logf = func(format string, args ...any) {
		message := fmt.Sprintf(format, args...)
		a.logs.Add(slog.LevelInfo, "app", message)
		log.Print(message)
	}
	a.tree.List.Selected = &a.treeRow
	a.resetLayout()
	a.find.init()
	a.callsInit()
	return a
}

func uriOf(path string) string { return uri.FromPath(path) }

// activePath is the focused group's active document.
func (a *App) activePath() string {
	if isDiffTab(a.focus.active) {
		return ""
	}
	return tabPath(a.focus.active)
}

// activeEditor is the editor of the focused group's active document.
func (a *App) activeEditor() *editor.State {
	if isMarkdownTab(a.focus.active) {
		return nil
	}
	return a.focus.eds[a.focus.active]
}

// ----- Workspace -----

// openFolderDialog asks for a folder, off the UI thread, and opens it.
func (a *App) openFolderDialog() {
	pick := a.host.PickFolder
	go func() {
		p, err := pick()
		a.post.Post(func() {
			switch {
			case err != nil:
				a.fail(locales.MsgCouldNotShowTheFolderDialog() + err.Error())
			case p != "":
				a.openProject(p)
			}
		})
	}()
}

func (a *App) addWorkspaceFolderDialog() {
	if a.find.replacing || !a.multiRootWorkspacesEnabled() {
		return
	}
	pick := a.host.PickFolder
	go func() {
		p, err := pick()
		a.post.Post(func() {
			switch {
			case err != nil:
				a.fail(locales.MsgCouldNotShowTheFolderDialog() + err.Error())
			case p != "":
				a.addWorkspaceFolder(p)
			}
		})
	}()
}

func (a *App) addWorkspaceFolder(path string) {
	if a.find.replacing || !a.multiRootWorkspacesEnabled() {
		return
	}
	root, err := workspace.CanonicalRoot(path)
	if err != nil {
		a.fail(locales.MsgCouldNotOpen() + path + ": " + describeErr(err))
		return
	}
	var folders []workspace.Folder
	if a.isCodeWorkspace() {
		folders, err = workspace.AddCodeWorkspaceFolder(a.workspaceID, workspace.Folder{Path: root, Name: filepath.Base(root)})
		if err != nil {
			a.fail(workspaceUpdateFailed(a.workspaceID, err))
			return
		}
	} else {
		for _, folder := range a.workspaceFolders {
			if folder.Path == root {
				return
			}
		}
		workspaceFile, created, convertErr := a.plainWorkspaceFile(a.workspaceFolders)
		if convertErr != nil {
			a.fail(locales.MsgCouldNotOpen() + workspaceFile + ": " + describeErr(convertErr))
			return
		}
		folders = nil
		for _, existing := range a.workspaceFolders {
			folders, err = workspace.AddCodeWorkspaceFolder(workspaceFile, existing)
			if err != nil {
				break
			}
		}
		if err == nil {
			folders, err = workspace.AddCodeWorkspaceFolder(workspaceFile, workspace.Folder{Path: root, Name: filepath.Base(root)})
		}
		if err != nil {
			if created {
				_ = os.Remove(workspaceFile)
			}
			a.fail(workspaceUpdateFailed(workspaceFile, err))
			return
		}
		a.adoptCodeWorkspace(workspaceFile)
	}
	a.syncWorkspaceFolders(folders)
}

// plainWorkspaceFile chooses or creates the VS Code workspace file a plain
// folder becomes when it gains a second root. An existing sibling that already
// contains the first root is reused; unrelated files are never overwritten.
func (a *App) plainWorkspaceFile(folders []workspace.Folder) (string, bool, error) {
	if len(a.workspaceFolders) == 0 {
		return "", false, errors.New("workspace has no folders")
	}
	first := a.workspaceFolders[0]
	base := filepath.Base(first.Path)
	for i := 0; i < 100; i++ {
		name := base
		if i > 0 {
			name += fmt.Sprintf("-%d", i+1)
		}
		candidate := filepath.Join(filepath.Dir(first.Path), name+".code-workspace")
		if _, err := os.Stat(candidate); errors.Is(err, fs.ErrNotExist) {
			if err := workspace.WriteNewCodeWorkspace(candidate, folders); err != nil {
				return candidate, false, err
			}
			return candidate, true, nil
		}
		parsed, err := workspace.ReadCodeWorkspace(candidate)
		if err == nil {
			for _, folder := range parsed.Folders {
				if folder.Path == first.Path {
					return candidate, false, nil
				}
			}
		}
	}
	return "", false, errors.New("no available workspace file name")
}

func (a *App) adoptCodeWorkspace(workspaceFile string) {
	if a.workspaceID == workspaceFile {
		return
	}
	old := a.workspaceID
	a.workspaceID = workspaceFile
	a.workspaceChanged = false
	if a.cfg.Workspaces != nil {
		if session, ok := a.cfg.Workspaces[old]; ok {
			if _, exists := a.cfg.Workspaces[workspaceFile]; !exists {
				session.Folders = nil
				a.cfg.Workspaces[workspaceFile] = session
			}
		}
	}
	a.cfg.AddRecent(workspaceFile)
	a.cfg.LastWorkspace = workspaceFile
	if parsed, err := workspace.ReadCodeWorkspace(workspaceFile); err == nil {
		a.applyWorkspaceSettings(parsed.Settings)
	}
	a.host.SetTitle(a.workspaceTitle())
	a.startWorkspaceFileWatch()
}

func (a *App) removeWorkspaceFolder(path string) {
	if a.find.replacing || len(a.workspaceFolders) <= 1 || !a.isWorkspaceFolder(path) {
		return
	}
	var folders []workspace.Folder
	if a.isCodeWorkspace() {
		var err error
		folders, err = workspace.RemoveCodeWorkspaceFolder(a.workspaceID, path)
		if err != nil {
			a.fail(workspaceUpdateFailed(a.workspaceID, err))
			return
		}
	} else {
		for _, folder := range a.workspaceFolders {
			if folder.Path != path {
				folders = append(folders, folder)
			}
		}
	}
	a.syncWorkspaceFolders(folders)
}

func (a *App) isCodeWorkspace() bool {
	return strings.EqualFold(filepath.Ext(a.workspaceID), ".code-workspace")
}

func (a *App) syncWorkspaceFolders(folders []workspace.Folder) {
	if slices.EqualFunc(a.workspaceFolders, folders, func(a, b workspace.Folder) bool { return a == b }) {
		return
	}
	previous := make(map[string]bool, len(a.workspaceFolders))
	next := make(map[string]bool, len(folders))
	for _, folder := range a.workspaceFolders {
		previous[folder.Path] = true
	}
	for _, folder := range folders {
		next[folder.Path] = true
	}
	for root := range previous {
		if next[root] {
			continue
		}
		if cancel := a.watchCancels[root]; cancel != nil {
			cancel()
			delete(a.watchCancels, root)
		}
		delete(a.ignored, root)
		a.tree.Open.Remove(root)
	}
	a.workspaceFolders = slices.Clone(folders)
	a.workspaceChanged = !a.isCodeWorkspace()
	a.root = folders[0].Path
	a.realRoot = a.root
	if resolved, err := filepath.EvalSymlinks(a.root); err == nil {
		a.realRoot = resolved
	}
	for dir := range a.dirs {
		if a.workspaceRootForPath(dir) == "" {
			delete(a.dirs, dir)
		}
	}
	if a.ignored == nil {
		a.ignored = map[string]*git.Ignored{}
	}
	for _, folder := range folders {
		if !previous[folder.Path] {
			a.loadDir(folder.Path)
			a.startWatchFolder(folder.Path)
		}
		if len(folders) > 1 || a.isCodeWorkspace() {
			a.tree.Open.Add(folder.Path)
		}
	}
	a.workspaceRootsEpoch++
	a.invalidateQuickFiles()
	a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
	a.runFind()
	a.refreshGitDiscovery()
	if !a.workspaceTrusted() {
		a.enforceTrust()
	} else {
		a.stopLanguageServers()
		a.initLanguageServers()
		a.openServerDocuments(nil)
		if a.navigator == navProblems && !a.sidebarOff {
			a.problems.started, a.problems.starting = false, 0
			a.startProjectDiagnostics()
		}
	}
	a.saveConfig()
}

// openWorkspace closes the current workspace and opens dir: its first
// level of entries loads in the background, and nothing else is read
// until the user opens it.
func (a *App) openWorkspace(dir string) {
	workspaceID, err := filepath.Abs(dir)
	if err != nil {
		a.welcomeNote = locales.Catalog.Format(i18n.Locale(), "msgCouldNotOpenSPath", map[string]any{"path": dir, "reason": describeErr(err)})
		return
	}
	workspaceID = filepath.Clean(workspaceID)
	var folders []workspace.Folder
	var settings workspace.Settings
	isCodeWorkspace := strings.EqualFold(filepath.Ext(workspaceID), ".code-workspace")
	if isCodeWorkspace {
		if !a.multiRootWorkspacesEnabled() {
			err = errors.New(locales.WorkspaceRequiresBeta())
		} else {
			var parsed *workspace.CodeWorkspace
			parsed, err = workspace.ReadCodeWorkspace(workspaceID)
			if err == nil {
				folders, settings = parsed.Folders, parsed.Settings
			}
		}
	} else {
		var root string
		root, err = workspace.CanonicalRoot(workspaceID)
		if err == nil {
			workspaceID = root
			folders = []workspace.Folder{{Path: root, Name: filepath.Base(root)}}
		}
	}
	if err == nil && !isCodeWorkspace {
		if saved := a.cfg.Workspaces[workspaceID]; a.multiRootWorkspacesEnabled() && len(saved.Folders) > 0 {
			if persisted, folderErr := loadSavedWorkspaceFolders(saved.Folders); folderErr == nil && len(persisted) > 0 {
				folders = persisted
			}
		}
		if len(folders) == 0 {
			err = errors.New("workspace has no folders")
		}
	}
	if err != nil {
		a.welcomeNote = locales.Catalog.Format(i18n.Locale(), "msgCouldNotOpenSPath", map[string]any{"path": dir, "reason": describeErr(err)})
		if errors.Is(err, fs.ErrNotExist) && slices.Contains(a.cfg.Recent, dir) {
			a.cfg.RemoveRecent(dir)
			a.saveConfig()
		}
		return
	}
	if a.guardDirty(a.dirtyDocs(), func() { a.openWorkspace(workspaceID) }) {
		return
	}
	a.closeWorkspace()
	a.workspaceID = workspaceID
	a.workspaceFolders = folders
	a.workspaceSettings = normalizedWorkspaceSettings(settings)
	a.workspaceChanged = !isCodeWorkspace && a.multiRootWorkspacesEnabled() && len(a.cfg.Workspaces[workspaceID].Folders) > 0
	a.workspaceRootsEpoch++
	a.invalidateQuickFiles()
	a.root = folders[0].Path
	a.realRoot = a.root
	if r, err := filepath.EvalSymlinks(a.root); err == nil {
		a.realRoot = r
	}
	a.welcomeNote = ""
	a.showWelcome = false
	a.wsCtx, a.wsCancel = context.WithCancel(context.Background())
	a.initLanguageServers()
	for _, folder := range folders {
		a.loadDir(folder.Path)
		if len(folders) > 1 || a.isCodeWorkspace() {
			a.tree.Open.Add(folder.Path)
		}
	}
	a.startWatch()
	a.startGit()
	a.restoreSession()
	if a.navigator == navProblems && !a.sidebarOff {
		a.startProjectDiagnostics()
	}
	a.windowWidth, a.windowHeight = config.WorkspaceSize(a.cfg.Workspaces[workspaceID])
	a.host.SetWorkspaceSize(a.windowWidth, a.windowHeight)
	a.cfg.LastWorkspace = workspaceID
	a.cfg.AddRecent(workspaceID)
	a.saveConfig()
	a.host.SetTitle(a.workspaceTitle())
}

func (a *App) initLanguageServers() {
	a.serverEpoch++
	gen, epoch, root := a.gen, a.serverEpoch, a.root
	folders := lspWorkspaceFolders(a.workspaceFolders)
	a.lsp = langserver.New(langserver.Options{
		Name:    "gopls",
		Root:    root,
		Folders: folders,
		Start:   a.startLSP(a.serverLog("gopls")),
		OnState: func(s langserver.State, err error) {
			a.post.Post(func() {
				if a.gen == gen && a.serverEpoch == epoch {
					a.lspState, a.lspErr = s, err
					a.serverStateLog("gopls", s, err)
				}
			})
		},
		OnDiagnostics: a.diagnosticsHandler(gen),
		Logf:          a.serverLog("gopls"),
	})
	a.wireApplyEdit(a.lsp)
	a.resetTypeScriptServer()
	a.clangLSP = languages.NewClangdServer(root, folders, func(s langserver.State, err error) {
		a.post.Post(func() {
			if a.gen == gen && a.serverEpoch == epoch {
				a.clangState, a.clangErr = s, err
				a.serverStateLog("clangd", s, err)
			}
		})
	}, a.diagnosticsHandler(gen), a.serverLog("clangd"))
	a.cmakeLSP = languages.NewCMakeServer(root, folders, func(s langserver.State, err error) {
		a.post.Post(func() {
			if a.gen == gen && a.serverEpoch == epoch {
				a.cmakeState, a.cmakeErr = s, err
				a.serverStateLog("cmake-language-server", s, err)
			}
		})
	}, a.diagnosticsHandler(gen), a.serverLog("cmake-language-server"))
	a.wireApplyEdit(a.clangLSP)
	a.wireApplyEdit(a.cmakeLSP)
	a.customLSPSpecs = languages.ConfiguredSpecs(a.cfg.LSP)
	a.customLSPs = map[int]*langserver.Server{}
	a.customCovers = map[int]func(string) bool{}
	a.customStates = map[int]struct {
		state langserver.State
		err   error
	}{}
	for i, spec := range a.customLSPSpecs {
		a.customCovers[i] = languages.ConfiguredCovers(spec)
		idx, name := i, spec.Name
		a.customLSPs[i] = languages.NewConfiguredServer(spec, root, folders, func(s langserver.State, err error) {
			a.post.Post(func() {
				if a.gen == gen && a.serverEpoch == epoch {
					a.customStates[idx] = struct {
						state langserver.State
						err   error
					}{s, err}
					a.serverStateLog(name, s, err)
				}
			})
		}, a.diagnosticsHandler(gen), a.serverLog(name))
		a.wireApplyEdit(a.customLSPs[i])
	}
}

// closeWorkspace forgets the workspace: its tabs, caches, searches, watch
// and language server.
func (a *App) closeWorkspace() {
	a.createEntry = nil
	a.renameEntry = nil
	a.cancelRename()
	a.deleteConfirm = nil
	// Persist the session before throwing its documents away. In particular,
	// a standalone window may own entries in the shared empty-root session.
	a.saveConfig()
	a.regexPlayground = nil
	a.regexToolWindow.resetWorkspace()
	for _, g := range a.groups() {
		a.cancelPendingPreview(g)
	}
	a.gen++
	a.workspaceRootsEpoch++
	a.invalidateQuickFiles()
	if a.wsCancel != nil {
		a.wsCancel()
	}
	a.cancelNav()
	a.endTabCycle()
	a.stopGoTask()
	a.task = nil
	a.taskList = ui.ListState{}
	a.closeTerminals()
	for _, g := range a.groups() {
		for path := range g.videos {
			g.closeVideo(path)
		}
	}
	a.stopLanguageServers()
	a.root, a.realRoot, a.workspaceID = "", "", ""
	a.workspaceFolders, a.workspaceChanged = nil, false
	a.workspaceSettings = workspace.Settings{}
	a.watchCancels = nil
	a.recents = nil
	a.dirs = map[string]*dirState{}
	a.tree = ui.OutlineState[string]{}
	a.treeRow = -1
	a.tree.List.Selected = &a.treeRow
	a.callsStop()
	a.calls.tree = ui.OutlineState[int]{}
	a.calls.row = -1
	a.calls.tree.List.Selected = &a.calls.row
	a.reveal = ""
	a.filter = treeFilter{}
	a.docs = map[string]*doc{}
	a.closed = nil
	a.resetLayout()
	a.hist.Clear()
	a.choices = nil
	a.peek = nil
	a.trustAsk, a.afterTrust = false, nil
	a.trustDismissed = false
	a.lspState, a.lspErr = langserver.Off, nil
	a.watchErr, a.watching = nil, false
	a.git = gitState{}
	a.ignored = nil
	a.feedback = feedbackState{}
	a.hover = hoverState{}
	a.find.reset()
	a.quick = quickOpen{}
	a.problems = problemsState{}
	a.bookmarksUI = bookmarksState{}
	a.bookmarks = nav.Bookmarks{}
	releaseStandalonePaths(a)
	a.standaloneSessionDocs = make(map[string]struct{})
	a.discardedSessionDocs = make(map[string]struct{})
	a.host.SetTitle("See")
}

// saveConfigSoon saves the settings once they stop changing for a
// moment, as while the sidebar's divider is dragged.
func (a *App) saveConfigSoon() {
	if a.restoringSession {
		return
	}
	if a.saveTimer != nil {
		a.saveTimer.Stop()
	}
	a.saveTimer = time.AfterFunc(500*time.Millisecond, func() { a.post.Post(a.saveConfig) })
}

var configSaveMu sync.Mutex

func (a *App) saveConfig() {
	// All windows share Config. Keep capture-and-save atomic so standalone
	// windows can merge their recovery entries without losing one another's.
	configSaveMu.Lock()
	defer configSaveMu.Unlock()
	if a.saveTimer != nil {
		a.saveTimer.Stop()
		a.saveTimer = nil
	}
	if !a.restoringSession {
		a.captureSession()
	}
	if err := a.cfg.Save(); err != nil {
		a.logf("saving settings: %v", err)
	}
}

// loadDir reads a directory's entries in the background.
func (a *App) loadDir(dir string) {
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
		a.post.Post(func() {
			if a.gen != gen || a.dirs[dir] != d || d.seq != seq {
				return
			}
			d.loading = false
			if err != nil {
				d.err = err
				return
			}
			d.entries, d.err, d.loaded = es, nil, true
			a.computeNested(d)
		})
	}()
}

// children lists a tree item's children: nil for a file, an empty list
// for a closed directory, and for an open one its entries, or a
// placeholder while they load or when reading them failed. Opening a
// directory is what reads it.
func (a *App) children(p string) []string {
	if a.filtering() {
		return a.filter.tree[p]
	}
	if isPlaceholder(p) {
		return nil
	}
	if e, ok := a.entry(p); !ok || !e.Dir {
		if ok && !e.Dir {
			// A file the nesting rules give children is expandable,
			// as a folder is; the outline shows them while it is open.
			if d := a.dirs[filepath.Dir(p)]; d != nil {
				if kids := d.nested[p]; len(kids) > 0 {
					return kids
				}
			}
		}
		return nil
	}
	d := a.dirs[p]
	if !a.tree.Open.Has(p) {
		if d != nil && d.loaded {
			return a.entryPaths(d)
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
	out := a.entryPaths(d)
	if d.err != nil {
		out = append(out, p+errorSuffix) // a refresh failed: keep the old list
	}
	return a.withEntryPrompt(p, out)
}

func (a *App) entryPaths(d *dirState) []string {
	nestedChild := map[string]bool{}
	for _, kids := range d.nested {
		for _, k := range kids {
			nestedChild[k] = true
		}
	}
	out := make([]string, 0, len(d.entries))
	for _, e := range d.entries {
		// A nested workspace root is owned by its own top-level branch. Showing
		// it inside the enclosing root too would give one filesystem node two
		// tree identities and duplicate every scan under it.
		if a.isWorkspaceFolder(e.Path) {
			continue
		}
		if !a.hiddenPath(e.Path) && !nestedChild[e.Path] {
			out = append(out, e.Path)
		}
	}
	return out
}

// computeNested groups the directory's files under their parents, by
// vscode-file-nesting-config's rules: a component's tests and styles,
// a config's sibling dotfiles.
func (a *App) computeNested(d *dirState) {
	names := make([]string, 0, len(d.entries))
	byName := map[string]workspace.Entry{}
	for _, e := range d.entries {
		if e.Dir || a.hiddenPath(e.Path) {
			continue
		}
		names = append(names, e.Name)
		byName[e.Name] = e
	}
	d.nested = map[string][]string{}
	for child, parent := range filenesting.Parents(names) {
		pe, ce := byName[parent], byName[child]
		if pe.Path == "" || ce.Path == "" {
			continue
		}
		d.nested[pe.Path] = append(d.nested[pe.Path], ce.Path)
	}
}

func (a *App) hiddenPath(p string) bool {
	if !filepath.IsAbs(p) {
		p = filepath.Join(a.root, p)
	}
	root := a.workspaceRootForPath(p)
	if root == "" {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	if a.cfg == nil {
		return false
	}
	for _, part := range strings.Split(filepath.ToSlash(rel), "/") {
		for _, name := range strings.Split(a.cfg.HiddenNames, ",") {
			if name = strings.TrimSpace(name); name != "" && part == name {
				return true
			}
		}
	}
	slashRel := filepath.ToSlash(rel)
	filesRules := workspaceExcludeRules{patterns: a.workspaceSettings.FilesExclude, clauses: a.workspaceSettings.FilesExcludeWhen}
	excepted := workspace.ExceptedWithSiblings(filesRules.patterns, filesRules.clauses, slashRel, siblingChecker(root))
	if root != "" && excludedByRules(filesRules, root, slashRel) {
		if info, err := os.Stat(p); err == nil && info.IsDir() && workspace.ExceptionMayReveal(exceptionRevealPatterns(filesRules), slashRel) {
			return false
		}
		return true
	}
	return a.workspaceSettings.ExplorerExcludeGitIgnore && !excepted && a.gitIgnores(p)
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
func (a *App) entry(p string) (workspace.Entry, bool) {
	if a.isWorkspaceFolder(p) {
		return workspace.Entry{Name: a.workspaceFolderLabel(p), Path: p, Dir: true}, true
	}
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
func (a *App) retryDir(dir string) {
	if d := a.dirs[dir]; d != nil {
		d.loading = false
	}
	a.loadDir(dir)
}

// refreshDir reads a loaded directory again, as after a change on disk.
func (a *App) refreshDir(dir string) {
	if d := a.dirs[dir]; d != nil && (d.loaded || d.err != nil) {
		d.loading = false
		a.loadDir(dir)
	}
}

// refreshAll reads every loaded directory and open document again, for
// when watching is not possible.
func (a *App) refreshAll() {
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
	a.quick.files, a.quick.err = nil, nil // listed again when next opened
	a.quick.scanGen++
	a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
}

// revealInTree opens the directories above p and chooses its row.
func (a *App) revealInTree(p string) {
	if a.root == "" || !a.withinWorkspace(p) || a.isWorkspaceFolder(p) {
		return
	}
	root := a.workspaceRootForPath(p)
	a.tree.Open.Add(root)
	for dir := filepath.Dir(p); dir != root && workspace.Within(root, dir); dir = filepath.Dir(dir) {
		a.tree.Open.Add(dir)
	}
	a.reveal = p
}

// ----- Documents -----

type standalonePathOwner struct {
	app      *App
	doc      *doc
	inFlight bool
}

var standalonePathRegistry = struct {
	sync.Mutex
	owners map[string]standalonePathOwner
}{owners: make(map[string]standalonePathOwner)}

// standalonePathKey identifies the file a standalone path reaches rather than
// only the spelling used to reach it. Existing files resolve fully; for a new
// Save As destination the parent resolves so aliases such as /tmp and
// /private/tmp still share one owner.
func standalonePathKey(path string) string {
	if abs, err := filepath.Abs(path); err == nil {
		path = abs
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		return filepath.Clean(workspace.DiskSpelling(real))
	}
	if realParent, err := filepath.EvalSymlinks(filepath.Dir(path)); err == nil {
		path = filepath.Join(realParent, filepath.Base(path))
	}
	return filepath.Clean(workspace.DiskSpelling(path))
}

// claimStandalonePath gives one document process-wide ownership of a saved
// path while it is open. Folderless windows share one recovery namespace, so
// two buffers must never write the same record.
func claimStandalonePath(a *App, d *doc, path string) bool {
	return claimStandalonePathForWrite(a, d, path, false)
}

// reserveStandalonePath gives a first save exclusive ownership until its
// asynchronous write completes, even if its window closes in the meantime.
func reserveStandalonePath(a *App, d *doc, path string) bool {
	return claimStandalonePathForWrite(a, d, path, true)
}

func claimStandalonePathForWrite(a *App, d *doc, path string, inFlight bool) bool {
	key := standalonePathKey(path)
	standalonePathRegistry.Lock()
	defer standalonePathRegistry.Unlock()
	if owner, ok := standalonePathRegistry.owners[key]; ok {
		if owner.app != a || owner.doc != d {
			return false
		}
		inFlight = inFlight || owner.inFlight
	}
	standalonePathRegistry.owners[key] = standalonePathOwner{app: a, doc: d, inFlight: inFlight}
	return true
}

// finishStandalonePathWrite turns a completed first-save reservation into the
// ordinary ownership held by its now-saved document.
func finishStandalonePathWrite(a *App, d *doc, path string) {
	key := standalonePathKey(path)
	standalonePathRegistry.Lock()
	defer standalonePathRegistry.Unlock()
	if owner, ok := standalonePathRegistry.owners[key]; ok && owner.app == a && owner.doc == d {
		owner.inFlight = false
		standalonePathRegistry.owners[key] = owner
	}
}

func releaseStandalonePath(a *App, d *doc, path string) {
	releaseStandalonePathWhen(a, d, path, true)
}

func releaseStandalonePathIfIdle(a *App, d *doc, path string) {
	releaseStandalonePathWhen(a, d, path, false)
}

func releaseStandalonePathWhen(a *App, d *doc, path string, includeInFlight bool) {
	key := standalonePathKey(path)
	standalonePathRegistry.Lock()
	defer standalonePathRegistry.Unlock()
	if owner, ok := standalonePathRegistry.owners[key]; ok && owner.app == a && owner.doc == d && (includeInFlight || !owner.inFlight) {
		delete(standalonePathRegistry.owners, key)
	}
}

func releaseStandalonePaths(a *App) {
	standalonePathRegistry.Lock()
	defer standalonePathRegistry.Unlock()
	for path, owner := range standalonePathRegistry.owners {
		if owner.app == a && !owner.inFlight {
			delete(standalonePathRegistry.owners, path)
		}
	}
}

func standaloneDocForPath(a *App, path string) *doc {
	key := standalonePathKey(path)
	for _, d := range a.docs {
		if !d.untitled && standalonePathKey(d.path) == key {
			return d
		}
	}
	return nil
}

// openFile shows the document of path in the focused group, opening it
// if needed, at pos when given. When another group already shows the
// document it shows there instead, so jumps like Go Back never open a
// second tab of it.
func (a *App) openFile(path string, pos *editor.Pos) *doc {
	return a.openFileAt(path, goTo{pos: pos})
}

func (a *App) openFileAt(path string, to goTo) *doc {
	g := a.focus
	docPath := a.docPath(path)
	if d := a.docs[docPath]; d != nil {
		docPath = d.path
	}
	if !g.hasDocument(docPath) {
		for _, other := range a.groups() {
			if other != g && other.hasDocument(docPath) {
				g = other
				break
			}
		}
	}
	return a.openIn(g, path, to)
}

// openIn shows path in g, at the place to says, if any.
func (a *App) openIn(g *group, path string, to goTo) *doc {
	return a.openInMode(g, path, to, true)
}

func (a *App) openInMode(g *group, path string, to goTo, addTab bool) *doc {
	if a.root == "" && len(a.docs) == 0 {
		a.host.SetWorkspaceSize(config.DefaultWindowWidth, config.DefaultWindowHeight)
	}
	a.term.maximized = false
	a.git.diffSeq++
	a.git.peek = nil
	path = a.docPath(path)
	// However the path reached here — typed, dragged, restored or reported
	// by a server — its identity is the disk's spelling, so that case and
	// Unicode variants of one file share one document rather than two
	// buffers editing the same bytes.
	path = workspace.DiskSpelling(path)
	d := a.docs[path]
	if d == nil && a.root == "" {
		d = standaloneDocForPath(a, path)
		if d != nil {
			path = d.path
		}
	}
	if d == nil {
		d = &doc{path: path, uri: uriOf(path), name: filepath.Base(path)}
		if a.root == "" && !claimStandalonePath(a, d, path) {
			a.host.RevealOpenFile(path)
			a.fail(locales.MsgThatFileIsAlreadyOpen())
			return nil
		}
		// A previously discarded document can be opened and edited again in
		// the same folderless window; it then owns fresh recovery state.
		delete(a.discardedSessionDocs, path)
		a.docs[path] = d
		a.loadDoc(d)
	}
	if addTab && !g.has(path) {
		// Open to the right of the active tab, as Xcode and VS Code do.
		at := len(g.order)
		if i := slices.Index(g.order, g.active); i >= 0 {
			at = i + 1
		}
		g.addTab(path, at)
	}
	if addTab && (to.pos != nil || to.lsp != nil) {
		delete(g.markdownPreview, path)
		g.pending[path] = to
	}
	if addTab {
		a.activateIn(g, path)
		a.showLoaded(g, d)
	}
	return d
}

// activate makes a tab of the focused group the active one.
func (a *App) activate(path string) { a.activateIn(a.focus, path) }

// closeDoc closes a tab of the focused group.
func (a *App) closeDoc(path string) { a.closeTab(a.focus, path) }

// showLoaded gives g an editor of d once d's text is there, and goes
// where g was asked to go in it.
func (a *App) showLoaded(g *group, d *doc) {
	if d.buf == nil || !g.hasDocument(d.path) {
		return
	}
	st := g.eds[d.path]
	if st == nil {
		st = a.newEditor(g, d)
		g.eds[d.path] = st
		if d.focusOnLoad {
			d.focusOnLoad = false
			if g == a.focus {
				a.focusEditor = st
			}
		}
		if g.pending[d.path].folds == nil && a.cfg.EditorFoldImports {
			st.FoldImports()
		}
		if m := a.serverFor(d.path); m != nil && !a.workspaceTrusted() && !a.trustDismissed {
			a.trustAsk = true
		}
		if m := a.serverFor(d.path); m != nil && a.workspaceTrusted() {
			go a.openLanguageDocument(m, a.wsCtx, d, d.uri)
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
	if to.folds != nil {
		st.Run(editor.UnfoldAll)
		for _, line := range to.folds {
			if line >= 0 && line < d.buf.LineCount() {
				st.Fold(line)
			}
		}
	}
	if to.selLen > 0 {
		off := d.buf.Offset(p)
		st.Select(off, min(off+to.selLen, d.buf.Len()))
	}
	if g == a.focus {
		a.focusEditor = st
	}
}

// loadDoc reads a document's file in the background.
func (a *App) loadDoc(d *doc) {
	d.loading, d.err = true, nil
	d.seq++
	seq, gen := d.seq, a.gen
	a.scheduleLoadingIndicator(d, seq, documentLoadingIndicatorDelay)
	lossy, needBuf, stamp, untitled := d.lossy, d.buf == nil, d.stamp, d.untitled
	checkStamp := !needBuf || d.video
	path, root, name := d.path, a.root, d.name
	var snap editor.Snapshot
	if !needBuf {
		snap = d.buf.Snapshot() // cheap: a version of the rope
	}
	go func() {
		if checkStamp {
			// A reload: skip files whose size and time are as they were,
			// without reading them.
			if fi, err := os.Stat(path); err == nil && (fileStamp{fi.Size(), fi.ModTime()}) == stamp {
				a.post.Post(func() {
					if a.gen == gen && a.docs[d.path] == d && d.seq == seq {
						a.finishDocLoading(d)
						d.gone, d.err = false, nil
					}
				})
				return
			}
		}
		f, err := readDocumentFile(path)
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
				buf.SetLanguage(languageFor(name, f.Text))
			}
		}
		// Compare with what is shown here, off the UI thread, so the UI
		// thread replaces the text only when it changed.
		same := err == nil && f.Kind == workspace.Text && !needBuf && snapEquals(snap, f.Text)
		a.post.Post(func() {
			if a.gen != gen || a.docs[d.path] != d || d.seq != seq {
				return
			}
			if d.buf != nil && d.buf.Version() != snap.Version() {
				a.finishDocLoading(d)
				a.reloadDoc(d)
				return
			}
			if d.video {
				a.closeDocumentVideos(d.path)
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
				if !f.Plain {
					buf.SetLanguage(languageFor(name, recovery.Text))
				}
				a.loaded(d, f, buf, false, nil)
				d.diskText, d.bom = recovery.Base, recovery.BOM
				expectOnDisk(d)
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

const documentLoadingIndicatorDelay = 800 * time.Millisecond

func (a *App) scheduleLoadingIndicator(d *doc, seq int, delay time.Duration) {
	stopLoadingIndicator(d)
	indicator := d.loadingSeq
	gen := a.gen
	d.loadingTimer = time.AfterFunc(delay, func() {
		a.post.Post(func() {
			if a.gen != gen || a.docs[d.path] != d || d.seq != seq || d.loadingSeq != indicator || !d.loading {
				return
			}
			d.loadingVisible = true
			d.loadingTimer = nil
		})
	})
}

func stopLoadingIndicator(d *doc) {
	d.loadingSeq++
	if d.loadingTimer != nil {
		d.loadingTimer.Stop()
		d.loadingTimer = nil
	}
	d.loadingVisible = false
}

func (a *App) finishDocLoading(d *doc) {
	d.loading = false
	stopLoadingIndicator(d)
}

// loaded takes a document's file as read.
func (a *App) loaded(d *doc, f workspace.File, buf *editor.Buffer, same bool, err error) {
	a.finishDocLoading(d)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) && d.buf != nil {
			d.gone = true // keep showing what it was
			a.failPendingPreview(d)
			return
		}
		d.err = err
		a.failPendingPreview(d)
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
	expectOnDisk(d)
	d.kind, d.size, d.plain = f.Kind, f.Size, f.Plain
	d.video = isVideoPath(d.path) && f.Kind == workspace.Binary
	d.stamp = fileStamp{f.Size, f.ModTime}
	if f.Kind != workspace.Text {
		a.finishPendingPreview(d)
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
		for st, p := range places {
			st.GoTo(clampPos(d.buf, p.pos))
			st.SetScrollY(p.y)
		}
		if m := a.serverFor(d.path); m != nil {
			m.Changed(d.uri, d.buf.Snapshot())
		}
	}
	a.setDocumentLanguage(d)
	a.captureLoadedLocalHistory(d, f.Text)
	for _, g := range a.groups() {
		a.showLoaded(g, d)
	}
	a.finishPendingPreview(d)
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
func (a *App) reloadDoc(d *doc) {
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
func (a *App) newEditor(g *group, d *doc) *editor.State {
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
	st.OnCompletion = func(req editor.CompletionRequest) { a.requestCompletions(g, d, st, req) }
	a.loadGitBase(d)
	st.ReplaceContextMenu = true
	st.ContextMenu = func(menu *ui.Menu) { a.editorContextMenu(menu, g, d, st) }
	st.OnHover = func(r editor.HoverRequest) { a.hoverAt(d, st, r) }
	st.RenderHover = func(c *ui.Context, panel ui.Element, h *editor.HoverCard) {
		a.renderHover(st, c, panel, h)
	}
	return st
}

// ----- Navigation -----

// cancelNav makes the navigation on its way, if any, stale.
func (a *App) cancelNav() {
	a.navSeq++
	if a.navCancel != nil {
		a.navCancel()
		a.navCancel = nil
	}
	a.finding = false
	a.choices = nil
	a.peek = nil
}

// here is the reading position of the focused group's document.
func (a *App) here() (nav.Entry, bool) {
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
func (a *App) userOpen(path string) {
	a.cancelNav()
	d := a.openFile(path, nil)
	if d != nil {
		d.previewOpened, d.focusOnLoad = false, false
	}
	a.focusImage = ""
	if isImagePath(a.activePath()) {
		a.focusImage = a.activePath()
	}
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}

// previewOpen previews path in the focused group. focus asks the
// keyboard for the document once it shows, as Open Quickly does; without
// it the keyboard stays where it is, as in the tree the click came from.
func (a *App) previewOpen(path string, focus ...bool) {
	focusEditor := len(focus) > 0 && focus[0]
	g := a.focus
	path = a.docPath(path)
	if d := a.docs[path]; d != nil {
		path = d.path
	} else if a.root == "" {
		if d := standaloneDocForPath(a, path); d != nil {
			path = d.path
		}
	}
	if p := g.pendingPreview; p != nil {
		if p.doc.path == path {
			return
		}
		a.cancelPendingPreview(g)
	}
	if g.has(path) {
		if focusEditor {
			a.userActivateIn(g, path)
		} else {
			a.activateIn(g, path)
		}
		a.focusImage = ""
		return
	}
	for _, other := range a.groups() {
		if other != g && other.hasDocument(path) {
			if focusEditor {
				a.userOpen(path)
			} else {
				a.openIn(other, path, goTo{})
			}
			a.focusImage = ""
			return
		}
	}
	old := g.preview
	replaceable := old != ""
	if d := a.docs[tabPath(old)]; d != nil && d.buf != nil && d.buf.Modified() {
		g.preview = ""
		replaceable = false
	}
	at := -1
	if replaceable {
		at = slices.Index(g.order, old)
		if at < 0 {
			replaceable = false
		}
	}
	outgoing := g.active
	a.cancelNav()
	d := a.openInMode(g, path, goTo{}, false)
	a.focusImage = "" // a single-click preview keeps focus in the tree
	if a.focus != g {
		return
	}
	if d == nil {
		return
	}
	path = d.path
	if d.loading && d.buf == nil && g.has(outgoing) {
		g.pendingPreview = &previewTransition{doc: d, outgoing: outgoing, replaced: old, replaceable: replaceable, focusSeq: g.focusSeq, focusEditor: focusEditor}
		if !focusEditor {
			d.previewOpened = true
		}
		return
	}
	if replaceable {
		g.addTab(path, at)
		a.closeTab(g, old)
	} else {
		at = len(g.order)
		if i := slices.Index(g.order, outgoing); i >= 0 {
			at = i + 1
		}
		g.addTab(path, at)
	}
	g.preview = path
	a.activateIn(g, path)
	a.showLoaded(g, d)
	if focusEditor {
		if st := g.eds[path]; st != nil {
			a.focusEditor = st
		} else if d.loading {
			d.focusOnLoad = true
		}
	} else {
		d.previewOpened = true
	}
}

// userActivate switches tabs of the focused group because the user asked.
func (a *App) userActivate(path string) { a.userActivateIn(a.focus, path) }

func (a *App) userActivateIn(g *group, path string) {
	if p := g.pendingPreview; p != nil && p.doc.path == path && !g.has(path) {
		return
	}
	a.term.maximized = false
	a.git.diffSeq++
	a.git.peek = nil
	if p := g.pendingPreview; p != nil {
		if p.doc.path == path {
			g.pendingPreview = nil
			g.preview = path
		} else {
			a.cancelPendingPreview(g)
		}
	}
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

func (a *App) goBack() {
	a.peek = nil
	cur, ok := a.here()
	if !ok {
		return
	}
	if to, ok := a.hist.GoBack(cur); ok {
		a.cancelNav()
		a.openFile(to.Path, &editor.Pos{Line: to.Line, Col: to.ByteColumn})
	}
}

func (a *App) goForward() {
	a.peek = nil
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
func (a *App) activateTabNumber(n int) {
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

// goToDefinition asks the language server where the symbol at offset of d is defined
// and goes there, in g. Only the latest navigation the user asked for
// wins: a response to an older one is dropped.
func (a *App) goToDefinition(g *group, d *doc, offset int) {
	// A Cmd+click on a document link opens its target instead of asking
	// for a definition; anything else falls through to the server.
	if l := linkAt(d, offset); l != nil {
		a.openDocLinkTarget(d, l)
		return
	}
	a.goToLocation(g, d, offset, "definition")
}

// peekDefinition asks for the definition of the symbol at offset of d,
// as Go to Definition does, but shows it in a peek of the editor instead
// of going there.
func (a *App) peekDefinition(g *group, d *doc, offset int) {
	a.goToLocation(g, d, offset, "peek")
}

func (a *App) goToLocation(g *group, d *doc, offset int, kind string) {
	if d.buf == nil {
		return
	}
	a.observeFocusedGroup(g)
	m := a.serverFor(d.path)
	if m == nil {
		a.flash(locales.MsgNoLanguageServerIsConfiguredForThisFile())
		return
	}
	if !a.workspaceTrusted() {
		a.trustAsk = true
		a.afterTrust = func() { a.goToLocation(g, d, offset, kind) }
		return
	}
	a.cancelNav()
	snap := d.buf.Snapshot()
	pos := snap.Pos(offset)
	from := nav.Entry{Path: d.path, Line: pos.Line, ByteColumn: pos.Col}
	line := snap.Line(pos.Line)
	seq, gen := a.navSeq, a.gen
	ctx, cancel := context.WithTimeout(a.wsCtx, 20*time.Second)
	a.navCancel = cancel
	a.finding = true
	a.findingLabel = "Finding definition…"
	if kind != "definition" && kind != "peek" {
		a.findingLabel = navigationText(kind, "Finding")
	}
	go func() {
		defer cancel()
		locs, enc, err := locationRequest(ctx, m, d.uri, snap, pos, line, kind)
		targets, skipped := targetsOf(locs)
		a.post.Post(func() {
			if a.gen != gen || a.navSeq != seq {
				return // the user has moved on
			}
			a.finding = false
			a.navCancel = nil
			a.locationFound(g, from, targets, enc, skipped, err, kind)
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
		// Where a server answers is spelled as the server spells it;
		// the disk's spelling is the identity the user lands in, so two
		// spellings of one file are one target, not two windows into it.
		targets = append(targets, target{name: l.Name, path: workspace.DiskSpelling(p), pos: l.Range.Start, end: l.Range.End})
	}
	return targets, skipped
}

func definition(ctx context.Context, m *langserver.Server, docURI string, snap editor.Snapshot, pos editor.Pos, line string) ([]lsp.Location, string, error) {
	return locationRequest(ctx, m, docURI, snap, pos, line, "definition")
}

func locationRequest(ctx context.Context, m *langserver.Server, docURI string, snap editor.Snapshot, pos editor.Pos, line, kind string) ([]lsp.Location, string, error) {
	c, err := m.Get(ctx)
	if err != nil {
		return nil, "", err
	}
	if !supportsNavigation(c, kind) {
		return nil, "", errors.New(navigationText(kind, "Unsupported"))
	}
	if err := m.Sync(ctx, c, docURI, snap); err != nil {
		return nil, "", err
	}
	enc := c.Encoding()
	position := lsp.Position{Line: pos.Line, Character: lsp.ColumnToLSP(line, pos.Col, enc)}
	if kind == "peek" {
		kind = "definition"
	}
	var locs []lsp.Location
	switch kind {
	case "implementation":
		locs, err = c.(implementationClient).Implementation(ctx, docURI, position)
	case "typeDefinition":
		locs, err = c.(typeDefinitionClient).TypeDefinition(ctx, docURI, position)
	default:
		locs, err = c.Definition(ctx, docURI, position)
	}
	return locs, enc, err
}

func (a *App) definitionFound(g *group, from nav.Entry, targets []target, enc string, skipped int, err error) {
	a.locationFound(g, from, targets, enc, skipped, err, "definition")
}

func (a *App) locationFound(g *group, from nav.Entry, targets []target, enc string, skipped int, err error, kind string) {
	if kind == "peek" {
		a.peekFound(g, from, targets, enc, skipped, err)
		return
	}
	switch {
	case err != nil:
		a.lspFailure(err)
	case len(targets) == 0 && skipped > 0:
		if kind != "definition" {
			a.flash(navigationText(kind, "Unavailable"))
			return
		}
		a.flash(locales.MsgTheDefinitionIsInAPlaceThatIs())
	case len(targets) == 0:
		if kind != "definition" {
			a.flash(navigationText(kind, "Empty"))
			return
		}
		a.flash(locales.MsgNoDefinitionFound())
	case len(targets) == 1:
		a.jump(g, from, targets[0], enc)
	default:
		a.choices = &defChoices{g: g, from: from, enc: enc, locs: targets, title: "definitions"}
		if kind != "definition" {
			a.choices.title = navigationText(kind, "Results")
			a.choices.navigation = kind
		}
		a.git.peek = nil
		a.loadRelatedPreviews(a.choices)
	}
}

// lspFailure reports why a language server request failed.
func (a *App) lspFailure(err error) {
	var failure *operr.Error
	switch {
	case errors.As(err, &failure) && failure.Kind == operr.Unsupported:
		a.flash(locales.Unavailable() + ": " + failure.Operation)
	case errors.As(err, &failure) && failure.Status == operr.Persistent:
		// A spent restart budget surfaces as bare "unexpected (persistent)"
		// in Error(); the user needs the server named and the way back.
		name := ""
		if m := a.serverFor(a.activePath()); m != nil {
			name = m.Name()
		}
		a.fail(strings.TrimSpace(name + " " + locales.MsgServerCrashedRepeatedly()))
	case errors.Is(err, proc.ErrUntrustedExecutable):
		a.fail("Denied: executables inside this workspace are never run.")
	case errors.Is(err, languages.ErrNoGopls):
		a.fail(locales.MsgGoToDefinitionNeedsGoplsInstallItWith())
	case errors.Is(err, languages.ErrNoCMakeServer):
		a.fail(locales.MsgInstallCmakeLanguageServerAndMakeItAvailable())
	case errors.Is(err, languages.ErrNoClangd):
		a.fail(locales.MsgInstallClangdAndMakeItAvailableOnPATH())
	case errors.Is(err, languages.ErrNoTypeScriptServer):
		a.fail(locales.MsgInstallTheTypeScriptLanguageServerInSettingsThen())
	case errors.Is(err, context.DeadlineExceeded):
		a.fail(locales.MsgTheLanguageServerTookTooLongToAnswer())
	case errors.Is(err, context.Canceled):
	default:
		a.fail(locales.MsgLanguageServerFailed() + err.Error())
	}
}

// jump goes to a definition in g, recording where it came from.
func (a *App) jump(g *group, from nav.Entry, t target, enc string) {
	a.choices = nil
	a.hist.Push(from)
	pos := t.pos
	a.openIn(g, t.path, goTo{lsp: &pos, enc: enc})
}

// trust records that the user trusts the workspace to run language servers in.
func (a *App) trust() {
	if len(a.workspaceFolders) == 0 {
		a.cfg.Trust(a.root)
	} else {
		for _, folder := range a.workspaceFolders {
			a.cfg.Trust(folder.Path)
		}
	}
	a.saveConfig()
	a.trustAsk = false
	if a.lsp == nil {
		a.initLanguageServers()
	}
	a.openServerDocuments(nil)
	if a.navigator == navProblems {
		a.startProjectDiagnostics()
	}
	if fn := a.afterTrust; fn != nil {
		a.afterTrust = nil
		fn()
	}
}

func (a *App) retryLSP() {
	if a.lsp != nil {
		a.invalidateHover()
		a.lsp.Retry()
		a.lspState, a.lspErr = a.lsp.Status()
		a.openServerDocuments(a.lsp)
		if a.problems.started {
			a.startProblemServer(filepath.Join(a.root, "go.mod"))
		}
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

func (a *App) serverFor(path string) *langserver.Server {
	if strings.EqualFold(filepath.Ext(path), ".go") {
		return a.lsp
	}
	if languages.TypeScriptLanguage(path) != "" {
		return a.tsLSP
	}
	if languages.CMakeLanguage(path) != "" {
		return a.cmakeLSP
	}
	if languages.ClangLanguage(path) != "" {
		return a.clangLSP
	}
	// A configured server handles what no built-in server claims.
	for i := range a.customLSPSpecs {
		if m := a.customLSPs[i]; m != nil && a.customCovers[i](path) {
			return m
		}
	}
	return nil
}

// customServerFor returns the configured server for path, or nil when a
// built-in server already claims it or none is configured for it.
func (a *App) customServerFor(path string) *langserver.Server {
	if path == "" {
		return nil
	}
	if strings.EqualFold(filepath.Ext(path), ".go") ||
		languages.TypeScriptLanguage(path) != "" ||
		languages.CMakeLanguage(path) != "" ||
		languages.ClangLanguage(path) != "" {
		return nil
	}
	for i := range a.customLSPSpecs {
		if m := a.customLSPs[i]; m != nil && a.customCovers[i](path) {
			return m
		}
	}
	return nil
}

// customIndex finds a configured server's index by its Server.
func (a *App) customIndex(m *langserver.Server) int {
	for i, s := range a.customLSPs {
		if s == m {
			return i
		}
	}
	return -1
}

func (a *App) retryCustomLSP(idx int) {
	m := a.customLSPs[idx]
	if m == nil {
		return
	}
	a.invalidateHover()
	m.Retry()
	s, err := m.Status()
	a.customStates[idx] = struct {
		state langserver.State
		err   error
	}{s, err}
	a.openServerDocuments(m)
}

func (a *App) retryTypeScriptLSP() {
	if a.tsLSP != nil {
		a.invalidateHover()
		a.tsLSP.Retry()
		a.tsState, a.tsErr = a.tsLSP.Status()
		a.openServerDocuments(a.tsLSP)
	}
}

func (a *App) resetTypeScriptServer() {
	a.tsEpoch++
	a.tsState, a.tsErr = langserver.Off, nil
	a.tsLSP = nil
	if a.root == "" {
		return
	}
	gen, epoch := a.gen, a.tsEpoch
	a.tsLSP = languages.NewTypeScriptServer(a.root, lspWorkspaceFolders(a.workspaceFolders), languages.StartTypeScript(a.serverLog("tsrs")), func(s langserver.State, err error) {
		a.post.Post(func() {
			if a.gen == gen && a.tsEpoch == epoch {
				a.tsState, a.tsErr = s, err
				a.serverStateLog("tsrs", s, err)
			}
		})
	}, a.diagnosticsHandler(gen), a.serverLog("tsrs"))
	a.wireApplyEdit(a.tsLSP)
}

func (a *App) openLanguageDocument(m *langserver.Server, ctx context.Context, d *doc, docURI string) {
	client, err := m.Get(ctx)
	if err != nil {
		return
	}
	a.post.Post(func() {
		if d.uri == docURI && a.docs[d.path] == d && a.serverFor(d.path) == m && d.buf != nil && m.Open(client, docURI, d.buf.Snapshot()) {
			a.upgradeLanguage(d, m, client)
		}
	})
}

// upgradeLanguage wraps the document buffer's language in a
// highlighter layering semantic tokens and folding ranges over the tokenizer,
// and following edits to keep cached inlay offsets current. It
// runs when the document first syncs, and again whenever a server
// takes over — start, restart and retry all reach it through
// openLanguageDocument — and when a lane learns its capability.
func (a *App) upgradeLanguage(d *doc, m *langserver.Server, client langserver.Client) {
	if a.docs[d.path] != d || d.buf == nil || d.plain || d.lossy || a.serverFor(d.path) != m || a.wsCtx == nil {
		return
	}
	lang := d.buf.Language()
	if lang == nil {
		return
	}
	// The language the wrap goes over: on a re-wrap — folding arriving
	// after semantic — lang is the copy made then, and baseLang the one
	// it copied.
	baseLang := lang
	if d.wrappedLang == lang {
		baseLang = d.baseLang
	}
	sem, fold, inlays := false, false, false
	if sc, ok := client.(semanticTokensClient); ok && sc.HasSemanticTokens() &&
		baseLang.Tokenizer != nil && baseLang.Highlighter == nil {
		sem = true // the semantic layer needs a tokenizer to build on
	}
	// The fold layer goes on once ranges are in; until then the
	// language's own highlighter keeps the gutter's indentation guess.
	if fc, ok := client.(foldingRangeClient); ok && fc.HasFoldingRange() &&
		d.foldRanges != nil && (baseLang.Tokenizer != nil || baseLang.Highlighter != nil) {
		fold = true
	}
	if ic, ok := client.(inlayHintClient); ok && ic.HasInlayHints() &&
		(baseLang.Tokenizer != nil || baseLang.Highlighter != nil) {
		inlays = true
	}
	if !sem && !fold && !inlays || d.wrappedLang == lang && (!fold || d.wrappedFold) && (!inlays || d.wrappedInlays) {
		return // nothing to add, or already wrapped with all it gets
	}
	ctx, gen := a.wsCtx, a.gen
	orig := baseLang.Highlighter
	wrapped := *baseLang
	wrapped.Highlighter = func(text editor.Snapshot, invalidate func()) editor.Highlighter {
		var base editor.Highlighter
		if orig != nil {
			base = orig(text, invalidate)
		} else {
			base = editor.NewTokenHighlighter(baseLang.Tokenizer, text)
		}
		if sem {
			h := &semanticHighlighter{
				base:       base,
				app:        a,
				doc:        d,
				server:     m,
				gen:        gen,
				uri:        d.uri,
				ctx:        ctx,
				invalidate: invalidate,
			}
			base = h
			h.wake(text)
			if hook := semanticHighlighterHook; hook != nil {
				hook(h)
			}
		}
		if fold {
			d.foldFolder = &foldFolder{base: base, doc: d}
			base = d.foldFolder
		}
		if inlays {
			if d.inlayVersion != text.Version() {
				a.clearInlayHints(d)
			}
			base = trackInlayEdits(base, d)
		}
		return base
	}
	d.wrappedInlays = inlays
	d.baseLang, d.wrappedLang, d.wrappedFold = baseLang, &wrapped, fold
	d.buf.SetLanguage(&wrapped)
}

func (a *App) openServerDocuments(only *langserver.Server) {
	if a.wsCtx == nil || !a.workspaceTrusted() {
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
		go a.openLanguageDocument(m, a.wsCtx, d, d.uri)
	}
}

// findUsages requests references or incoming calls for the focused symbol.
func (a *App) findUsages(callers bool) {
	g := a.focus
	if g == nil {
		return
	}
	a.findUsagesAt(callers, g.caretAnchor, true)
}

func (a *App) findUsagesAt(callers bool, anchor ui.Point, validAnchor bool) {
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
		a.flash(locales.MsgNoLanguageServerIsConfiguredForThisFile())
		return
	}
	if !a.workspaceTrusted() {
		a.trustAsk = true
		a.afterTrust = func() { a.findUsagesAt(callers, anchor, validAnchor) }
		return
	}
	a.term.maximized = false
	a.cancelNav()
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
		a.post.Post(func() {
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
					a.flash(locales.MsgResultsAreNotInLocalFiles())
				} else {
					display := referenceTitle(name)
					a.flash(locales.Catalog.Format(i18n.Locale(), "msgNoSFound", map[string]any{"name": display}))
				}
				return
			}
			a.choices = &defChoices{g: g, from: from, enc: enc, locs: targets, title: name}
			a.git.peek = nil
			a.loadRelatedPreviews(a.choices)
		})
	}()
}

func (a *App) loadRelatedPreviews(ch *defChoices) {
	// Snapshots preserve unsaved edits without reading editor state in a worker.
	snapshots := make(map[string]editor.Snapshot)
	for _, t := range ch.locs {
		path := a.docPath(t.path)
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
					buffers[t.path].SetLanguage(languageFor(t.path, snap.Line(0)))
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
					buffers[t.path].SetLanguage(languageFor(t.path, f.Text))
				}
				files[t.path] = lines
			}
			if t.pos.Line >= 0 && t.pos.Line < len(lines) {
				t.previewStart = max(0, t.pos.Line-2)
				t.preview = slices.Clone(lines[t.previewStart:min(len(lines), t.pos.Line+3)])
			}
		}
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			if a.choices != ch {
				// The list left the peek: a pinned run may still own it.
				run := a.references.activeRun()
				if run == nil || run.choices != ch {
					return
				}
			}
			ch.locs = targets
			ch.buffers = buffers
			a.selectRelated(ch, ch.selected)
		})
	}()
}

func (a *App) selectRelated(ch *defChoices, index int) {
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
		if ch.navigation != "" {
			label = navigationText(ch.navigation, "Preview")
		}
		ch.previewEditor = &editor.State{Buffer: buf, ReadOnly: true, Label: label, Theme: a.edTheme}
		a.applyEditorPrefs(ch.previewEditor)
		ch.previewPath = t.path
	}
	line := min(max(t.pos.Line, 0), buf.LineCount()-1)
	col := lsp.ColumnFromLSP(buf.Line(line), t.pos.Character, ch.enc)
	ch.previewEditor.LineBackgrounds = []editor.LineBackground{{First: line, Last: line, Color: a.colors().Pill}}
	ch.previewEditor.GoTo(editor.Pos{Line: line, Col: col})
	if t.end.Line == t.pos.Line && t.end.Character > t.pos.Character {
		end := lsp.ColumnFromLSP(buf.Line(line), t.end.Character, ch.enc)
		ch.previewEditor.TextBackgrounds = []editor.TextBackground{{Start: buf.Offset(editor.Pos{Line: line, Col: col}), End: buf.Offset(editor.Pos{Line: line, Col: end}), Color: a.colors().Match}}
	} else {
		ch.previewEditor.TextBackgrounds = nil
	}
}

func (a *App) openProject(path string) {
	if a.root != "" && a.host.OpenProjectWindow(path) {
		return
	}
	a.openWorkspace(path)
}

func (a *App) retryClangd() {
	if a.clangLSP != nil {
		a.invalidateHover()
		a.clangLSP.Retry()
		a.clangState, a.clangErr = a.clangLSP.Status()
		a.openServerDocuments(a.clangLSP)
	}
}

func (a *App) retryCMake() {
	if a.cmakeLSP != nil {
		a.invalidateHover()
		a.cmakeLSP.Retry()
		a.cmakeState, a.cmakeErr = a.cmakeLSP.Status()
		a.openServerDocuments(a.cmakeLSP)
	}
}

// configLoadNote is what the startup tells the user when the settings
// file degraded on load, or nothing when it loaded cleanly.
func configLoadNote(cfg *config.Config) string {
	issues := cfg.Issues()
	if len(issues) == 0 {
		return ""
	}
	details := make([]string, len(issues))
	for i, issue := range issues {
		details[i] = issue.Error()
	}
	return locales.Catalog.Format(i18n.Locale(), "msgConfigLoadProblem", map[string]any{"detail": strings.Join(details, "; ")})
}
