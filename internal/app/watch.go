package app

import (
	"context"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo"

	"see/internal/git"
	"see/internal/langserver"
	"see/internal/lsp"
	"see/internal/workspace"
)

// watchFiles starts a watch; it is mygo.WatchFiles in the app, and a
// test's own in tests.
var watchFiles = func(ctx context.Context, root string, opts mygo.FileWatchOptions) (fileWatcher, error) {
	return mygo.WatchFiles(ctx, root, opts)
}

type fileWatcher interface {
	Next(ctx context.Context) ([]mygo.FileEvent, error)
	Close() error
}

// startWatch follows changes in the workspace in the background. A tree
// too large to watch in full falls back to coarse hints; when watching
// fails altogether, reading still works and Refresh reads again.
func (a *App) startWatch() {
	if watchFiles == nil {
		return
	}
	for _, folder := range a.workspaceFolders {
		a.startWatchFolder(folder.Path)
	}
	a.startWorkspaceFileWatch()
}

// startWorkspaceFileWatch follows the workspace file itself. It normally
// lives outside every root, so the folder watches never see VS Code adding
// or removing a member.
func (a *App) startWorkspaceFileWatch() {
	watch := watchFiles
	if watch == nil || !a.isCodeWorkspace() {
		return
	}
	if a.workspaceFileCancel != nil {
		a.workspaceFileCancel()
	}
	ctx, cancel := context.WithCancel(a.wsCtx)
	a.workspaceFileCancel = cancel
	workspaceFile := a.workspaceID
	targets := workspaceFileWatchTargets(workspaceFile)
	gen := a.gen
	for dir, bases := range targets {
		go func(dir string, bases map[string]bool) {
			w, err := watch(ctx, dir, mygo.FileWatchOptions{})
			if err != nil {
				a.post.Post(func() {
					if a.gen == gen {
						a.logf("workspace watch: %v", err)
					}
				})
				return
			}
			defer w.Close()
			for {
				evs, err := w.Next(ctx)
				if err != nil {
					return
				}
				touched := false
				for _, ev := range evs {
					if bases[ev.Path] || bases[ev.OldPath] {
						touched = true
						break
					}
				}
				if touched {
					a.post.Post(func() {
						if a.gen == gen {
							a.reloadCodeWorkspace(workspaceFile)
						}
					})
				}
			}
		}(dir, bases)
	}
}

func workspaceFileWatchTargets(workspaceFile string) map[string]map[string]bool {
	targets := map[string]map[string]bool{}
	add := func(file string) {
		dir, base := filepath.Dir(file), filepath.Base(file)
		if targets[dir] == nil {
			targets[dir] = map[string]bool{}
		}
		targets[dir][base] = true
	}
	add(workspaceFile)
	target := ""
	if resolved, err := filepath.EvalSymlinks(workspaceFile); err == nil {
		target = resolved
	} else if link, err := os.Readlink(workspaceFile); err == nil {
		if !filepath.IsAbs(link) {
			link = filepath.Join(filepath.Dir(workspaceFile), link)
		}
		target = filepath.Clean(link)
	}
	if target != "" && target != workspaceFile {
		add(target)
	}
	return targets
}

func (a *App) reloadCodeWorkspace(workspaceFile string) {
	if !a.isCodeWorkspace() || a.workspaceID != workspaceFile {
		return
	}
	parsed, err := workspace.ReadCodeWorkspace(workspaceFile)
	if err != nil {
		// Editors replace workspace files through a temporary name; a remove
		// or half-written rename is followed by the complete file. Keeping the
		// last valid membership is safer than dropping the workspace under the user.
		a.logf("workspace reload: %v", err)
		return
	}
	a.applyWorkspaceSettings(parsed.Settings)
	a.syncWorkspaceFolders(parsed.Folders)
}

func (a *App) startWatchFolder(root string) {
	watch := watchFiles
	if watch == nil || root == "" {
		return
	}
	if a.watchCancels == nil {
		a.watchCancels = map[string]context.CancelFunc{}
	}
	if cancel := a.watchCancels[root]; cancel != nil {
		cancel()
	}
	ctx, cancel := context.WithCancel(a.wsCtx)
	a.watchCancels[root] = cancel
	gen := a.gen
	watchExclude := workspaceExcludeRules{patterns: a.workspaceSettings.FilesWatcherExclude, clauses: a.workspaceSettings.FilesWatcherExcludeWhen}
	current := func() bool {
		return a.gen == gen && ctx.Err() == nil && a.isWorkspaceFolder(root)
	}
	go func() {
		// What git ignores is left out first, as build output, so that a
		// large project stays within what a watch can follow file by file.
		// Single files git ignores are still watched, as an .env open in
		// a tab.
		ignored := readIgnored(ctx, root)
		a.post.Post(func() {
			if current() {
				if a.ignored == nil {
					a.ignored = map[string]*git.Ignored{}
				}
				a.ignored[root] = ignored
			}
		})
		ignore := func(rel string, isDir bool) bool {
			return excludedByRules(watchExclude, root, filepath.ToSlash(rel)) || isDir && (workspace.IgnoredRel(rel) || ignored.Dir(rel))
		}
		w, err := watch(ctx, root, mygo.FileWatchOptions{Recursive: true, Ignore: ignore})
		if errors.Is(err, mygo.ErrWatchLimit) {
			a.logf("watch: too many entries, falling back to hints")
			w, err = watch(ctx, root, mygo.FileWatchOptions{Recursive: true, Ignore: ignore, HintsOnly: true})
		}
		a.post.Post(func() {
			if current() {
				a.watching, a.watchErr = err == nil, err
			}
		})
		if err != nil {
			return
		}
		defer w.Close()
		for {
			evs, err := w.Next(ctx)
			if err != nil {
				if ctx.Err() == nil {
					if errors.Is(err, io.EOF) {
						err = errors.New("the folder was removed or moved")
					}
					a.post.Post(func() {
						if current() {
							a.watching, a.watchErr = false, err
						}
					})
				}
				return
			}
			a.post.Post(func() {
				if current() {
					a.applyEventsAt(root, evs)
				}
			})
		}
	}()
}

// applyEvents refreshes what changes on disk touch: the loaded
// directories they are in, and the open documents they name.
func (a *App) applyEvents(evs []mygo.FileEvent) {
	root := a.root
	if root == "" {
		return
	}
	a.applyEventsAt(root, evs)
}

func (a *App) applyEventsAt(root string, evs []mygo.FileEvent) {
	if !a.withinWorkspace(root) {
		return
	}
	if len(evs) > 0 {
		a.refreshGit()
		a.invalidateHover()
	}
	dirs := map[string]bool{}
	changes := map[*langserver.Server][]lsp.FileChange{}
	notify := func(p string, kind lsp.FileChangeType) {
		server := a.serverFor(p)
		if name := filepath.Base(p); name == "go.mod" || name == "go.work" {
			server = a.lsp
		}
		if server != nil {
			changes[server] = append(changes[server], lsp.FileChange{URI: uriOf(p), Type: kind})
		}
	}
	for _, ev := range evs {
		if path.Base(ev.Path) == ".gitignore" || path.Base(ev.Path) == "exclude" && strings.Contains(ev.Path, ".git/info") {
			a.rereadIgnoredAt(root, a.wsCtx, a.gen)
		}
		// What a watch reports is relative to the root; joining it
		// confined keeps a malformed report from naming anything outside
		// the workspace, however it got into the stream.
		p, err := workspace.JoinRel(root, ev.Path)
		if err != nil {
			continue
		}
		switch ev.Op {
		case mygo.FileHint:
			notify(p, lsp.FileChanged)
			// Something under p changed: read again what is loaded there,
			// and the documents in it.
			for dir := range a.dirs {
				if workspace.Within(p, dir) {
					dirs[dir] = true
				}
			}
			for _, d := range a.docs {
				if workspace.Within(p, d.path) {
					a.reloadDoc(d)
				}
			}
			continue
		case mygo.FileRename:
			old, err := workspace.JoinRel(root, ev.OldPath)
			if err != nil {
				continue
			}
			dirs[filepath.Dir(old)] = true
			// A rename the watch reports closes one identity and opens
			// another: documents at the old path are gone where they stand,
			// never silently moved to a path the user did not put them at.
			a.removed(old)
			notify(old, lsp.FileDeleted)
			notify(p, lsp.FileCreated)
		case mygo.FileRemove:
			a.removed(p)
			notify(p, lsp.FileDeleted)
		case mygo.FileCreate, mygo.FileWrite:
			delete(a.problems.disk, p)
			if ev.Op == mygo.FileCreate {
				notify(p, lsp.FileCreated)
			} else {
				notify(p, lsp.FileChanged)
			}
			if d := a.docs[p]; d != nil {
				a.reloadDoc(d)
			}
		}
		if a.problems.started && !ev.IsDir && (ev.Op == mygo.FileCreate || ev.Op == mygo.FileRename) {
			a.startProblemServer(p)
		}
		if ev.Path != "." {
			dirs[filepath.Dir(p)] = true
		}
		if ev.IsDir {
			dirs[p] = true
		}
	}
	for server, serverChanges := range changes {
		server.WatchedFiles(serverChanges)
	}
	for dir := range dirs {
		a.refreshDir(dir)
	}
	if len(evs) > 0 {
		a.quick.files, a.quick.err = nil, nil // Open Quickly lists them again when next opened
		a.quick.scanGen++
		if q := &a.quick; q.open && !strings.HasPrefix(q.query, ">") &&
			!strings.HasPrefix(q.query, "#") && !strings.HasPrefix(q.query, "@") {
			// An open panel consuming the file list re-ranks — and with the
			// list gone, re-scans — on its next frame: the invalidation is
			// the retry point for a sticky scan error, and the answer to a
			// file list that no longer matches the disk. Command and symbol
			// queries do not consume the list and keep their results.
			q.ranked = "\x00"
		}
		a.filter.forQuery = ""
	}
}

// removed marks the documents at or under p as gone; their tabs stay
// until the user closes them.
func (a *App) removed(p string) {
	for path := range a.problems.disk {
		if workspace.Within(p, path) {
			delete(a.problems.disk, path)
		}
	}
	for _, d := range a.docs {
		if workspace.Within(p, d.path) {
			if d.buf != nil {
				d.gone = true
			} else {
				a.reloadDoc(d) // shows why it cannot be read
			}
		}
	}
	// The folder itself and those under it: their rows go once their
	// parent is read again.
	for dir := range a.dirs {
		if workspace.Within(p, dir) {
			delete(a.dirs, dir)
		}
	}
	a.bookmarks.Forget(p)
}

// readIgnored is what git ignores in root, nothing outside a repository;
// tests leave git out of their watches.
var readIgnored = func(ctx context.Context, root string) *git.Ignored {
	ig, _ := git.ReadIgnored(ctx, root)
	return ig
}

// rereadIgnored reads what git ignores again after a .gitignore changed,
// for the tree; the watch keeps what it was started with until the
// workspace opens again.
func (a *App) rereadIgnored() {
	root, ctx, gen := a.root, a.wsCtx, a.gen
	a.rereadIgnoredAt(root, ctx, gen)
}

func (a *App) rereadIgnoredAt(root string, ctx context.Context, gen int) {
	rootsEpoch := a.workspaceRootsEpoch
	go func() {
		ig := readIgnored(ctx, root)
		a.post.Post(func() {
			if a.gen == gen && a.workspaceRootsEpoch == rootsEpoch && a.isWorkspaceFolder(root) {
				if a.ignored == nil {
					a.ignored = map[string]*git.Ignored{}
				}
				a.ignored[root] = ig
			}
		})
	}()
}

// gitIgnores reports whether git ignores p, a path of the workspace,
// which the tree dims, as VS Code does.
func (a *App) gitIgnores(p string) bool {
	root := a.workspaceRootForPath(p)
	if root == "" {
		root = a.root
	}
	ignored := a.ignored[root]
	if ignored == nil || !workspace.Within(root, p) || p == root {
		return false
	}
	rel, err := filepath.Rel(root, p)
	return err == nil && ignored.Has(filepath.ToSlash(rel))
}
