package main

import (
	"context"
	"errors"
	"io"
	"path/filepath"

	"github.com/egoist/mygo"

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
func (a *app) startWatch() {
	if watchFiles == nil {
		return
	}
	root, ctx, gen := a.root, a.wsCtx, a.gen
	ignore := func(rel string, isDir bool) bool { return isDir && workspace.IgnoredRel(rel) }
	go func() {
		w, err := watchFiles(ctx, root, mygo.FileWatchOptions{Recursive: true, Ignore: ignore})
		if errors.Is(err, mygo.ErrWatchLimit) {
			a.logf("watch: too many entries, falling back to hints")
			w, err = watchFiles(ctx, root, mygo.FileWatchOptions{Recursive: true, Ignore: ignore, HintsOnly: true})
		}
		a.post(func() {
			if a.gen == gen {
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
					a.post(func() {
						if a.gen == gen {
							a.watching, a.watchErr = false, err
						}
					})
				}
				return
			}
			a.post(func() {
				if a.gen == gen {
					a.applyEvents(evs)
				}
			})
		}
	}()
}

// applyEvents refreshes what changes on disk touch: the loaded
// directories they are in, and the open documents they name.
func (a *app) applyEvents(evs []mygo.FileEvent) {
	if len(evs) > 0 {
		a.invalidateHover()
	}
	dirs := map[string]bool{}
	for _, ev := range evs {
		p := filepath.Join(a.root, filepath.FromSlash(ev.Path))
		switch ev.Op {
		case mygo.FileHint:
			// Something under p changed: read again what is loaded there,
			// and the documents in it.
			for dir := range a.dirs {
				if within(p, dir) {
					dirs[dir] = true
				}
			}
			for _, d := range a.docs {
				if within(p, d.path) {
					a.reloadDoc(d)
				}
			}
			continue
		case mygo.FileRename:
			old := filepath.Join(a.root, filepath.FromSlash(ev.OldPath))
			dirs[filepath.Dir(old)] = true
			a.removed(old)
		case mygo.FileRemove:
			a.removed(p)
		case mygo.FileCreate, mygo.FileWrite:
			if d := a.docs[p]; d != nil {
				a.reloadDoc(d)
			}
		}
		if ev.Path != "." {
			dirs[filepath.Dir(p)] = true
		}
		if ev.IsDir {
			dirs[p] = true
		}
	}
	for dir := range dirs {
		a.refreshDir(dir)
	}
	if len(evs) > 0 {
		a.quick.files = nil // Open Quickly lists them again when next opened
	}
}

// removed marks the documents at or under p as gone; their tabs stay
// until the user closes them.
func (a *app) removed(p string) {
	for _, d := range a.docs {
		if within(p, d.path) {
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
		if within(p, dir) {
			delete(a.dirs, dir)
		}
	}
}
