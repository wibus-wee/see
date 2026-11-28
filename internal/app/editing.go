package app

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"see/locales"
	"strings"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/atomicfile"
	"see/internal/config"
	"see/internal/fileobs"
	"see/internal/platform"
	"see/internal/workspace"
)

// openFileDialog asks for a file, off the UI thread, and opens it.
func (a *App) openFileDialog() {
	pick := a.host.PickFile
	go func() {
		p, err := pick()
		a.post.Post(func() {
			switch {
			case err != nil:
				a.fail(locales.MsgCouldNotShowTheFileDialog() + err.Error())
			case p != "":
				if a.multiRootWorkspacesEnabled() && strings.EqualFold(filepath.Ext(p), ".code-workspace") {
					a.openWorkspace(p)
				} else if a.root == "" {
					a.showWelcome = false
					a.openFile(p, nil)
				} else {
					a.openCLIPath(p)
				}
			}
		})
	}()
}

func (a *App) newFile() {
	dir, err := draftDir()
	if err != nil {
		a.newFileError(locales.MsgCouldNotCreateRecoveryCache() + err.Error())
		return
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		a.newFileError(locales.MsgCouldNotCreateRecoveryCache() + err.Error())
		return
	}
	var path string
	for {
		var id [16]byte
		if _, err = rand.Read(id[:]); err != nil {
			a.newFileError(locales.MsgCouldNotCreateFile() + err.Error())
			return
		}
		path = filepath.Join(dir, fmt.Sprintf("%x", id))
		err = createDraft(path)
		if !os.IsExist(err) {
			break
		}
	}
	if err != nil {
		a.newFileError(locales.MsgCouldNotCacheUnsavedFile() + err.Error())
		return
	}
	name := a.untitledName()
	if a.root == "" && len(a.docs) == 0 {
		a.host.SetWorkspaceSize(config.DefaultWindowWidth, config.DefaultWindowHeight)
	}
	d := &doc{path: path, uri: uriOf(path), name: name, untitled: true, cache: &draftCache{}, buf: editor.NewBuffer(""), kind: workspace.Text}
	a.docs[path] = d
	a.openFile(path, nil)
	a.focusEditor = a.focus.eds[path]
	a.welcomeNote = ""
	a.showWelcome = false
	a.saveConfig()
}

// untitledName is what the next untitled document is called: the first
// of "Untitled", "Untitled-1" and so on no other has.
func (a *App) untitledName() string {
	name := "Untitled"
	for n := 1; ; n++ {
		used := false
		for _, d := range a.docs {
			if d.untitled && d.name == name {
				used = true
				break
			}
		}
		if !used {
			return name
		}
		name = fmt.Sprintf("Untitled-%d", n)
	}
}

func (a *App) newFileError(msg string) {
	a.fail(msg)
	if a.showWelcome {
		a.welcomeNote = msg
	}
}

// createDraft makes the initial recovery cache before the welcome window is
// dismissed. Later edits replace this file through cacheDraft.
func createDraft(path string) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

// writeFileIfUnchanged publishes through atomicfile like writeFileAtomically
// and additionally guards the publish with the document's save condition:
// unless overwrite is explicit, the target is replaced only while its
// content still matches expectation — digest-strength, built from the
// bytes the buffer was read from. The condition evaluates inside the
// publish boundary, after the staging file is durable and closed and
// immediately before the rename. POSIX cannot compare-and-rename in one
// step, so this is best-effort conflict detection whose race window is
// the rename boundary (documented per REQ-COND-070), never an atomic
// compare-and-publish — and it shrinks the window from the entire
// staging write to that boundary. Both ends of the condition are
// digest-carrying observations: the re-observation is ObserveContent,
// never ObservePath, so a weakened comparison cannot assemble silently.
func writeFileIfUnchanged(path string, expectation fileobs.Observation, overwrite bool, write func(io.Writer) error) error {
	s, err := atomicfile.Begin(path)
	if err != nil {
		return err
	}
	defer func() { _ = s.Abort() }()
	if err := write(s); err != nil {
		return err
	}
	if overwrite {
		return s.Commit()
	}
	return s.CommitIf(func() error {
		current, err := fileobs.ObserveContent(fileobs.SourceRead, path)
		if err != nil {
			return err
		}
		return fileobs.Check(expectation, current)
	})
}

// writeFileAtomically writes to a sibling and only moves it into place after
// the contents are complete and durable. Rename deliberately replaces an
// existing destination: the native save panel has already asked the user to
// approve that overwrite.
func writeFileAtomically(path string, write func(io.Writer) error) error {
	return atomicfile.WriteFile(path, write)
}

func draftDir() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "See", "drafts"), nil
}

func validDraftPath(path string) bool {
	dir, err := draftDir()
	return err == nil && filepath.Dir(filepath.Clean(path)) == dir && len(filepath.Base(path)) == 32 && !strings.ContainsAny(filepath.Base(path), "/\\")
}

func (a *App) cacheDraft(d *doc) {
	cache, path, snap := d.cache, d.path, d.buf.Snapshot()
	post := a.post.Post
	go func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.removed || snap.Version() < cache.version {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			post(func() { a.fail(locales.MsgCouldNotCacheUnsavedFile() + err.Error()) })
			return
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".draft-")
		if err != nil {
			post(func() { a.fail(locales.MsgCouldNotCacheUnsavedFile() + err.Error()) })
			return
		}
		name := tmp.Name()
		defer os.Remove(name)
		_, err = snap.WriteTo(tmp)
		if e := tmp.Close(); err == nil {
			err = e
		}
		if err == nil {
			err = os.Rename(name, path)
		}
		if err == nil {
			cache.version = snap.Version()
		}
		post(func() {
			if err != nil {
				a.fail(locales.MsgCouldNotCacheUnsavedFile() + err.Error())
			}
		})
	}()
}

func removeDraft(d *doc) {
	cache, path := d.cache, d.path
	go func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		cache.removed = true
		_ = os.Remove(path)
	}()
}

type confirmation struct {
	message string
	actions []action
	// inWindow shows the question in the window, where the host has no
	// dialog to ask it in.
	inWindow bool
}

func (a *App) dirtyDocs() []*doc {
	var docs []*doc
	for _, d := range a.docs {
		if d.buf != nil && d.buf.Modified() {
			docs = append(docs, d)
		}
	}
	return docs
}

// guardDirty holds a destructive action until the user saves or discards.
func (a *App) guardDirty(docs []*doc, proceed func()) bool {
	if a.allowDirty || len(docs) == 0 {
		return false
	}
	if !a.cfg.ConfirmClose {
		a.discardDocs(docs)
		return false
	}
	if a.confirm != nil {
		return true
	}
	a.showWelcome = false
	a.confirm = &confirmation{message: locales.UnsavedCount(len(docs))}
	a.confirm.actions = []action{
		{locales.MsgSave(), func() {
			a.saveDocs(docs, func() { a.confirm = nil; proceed() })
		}},
		{locales.DiscardChanges(), func() {
			a.confirm = nil
			a.discardDocs(docs)
			a.allowDirty = true
			defer func() { a.allowDirty = false }()
			proceed()
		}},
		{locales.Cancel(), func() { a.confirm = nil }},
	}
	q, ask, post := a.confirm, a.host.ConfirmDirty, a.post.Post
	go func() {
		choice, err := ask(q.message)
		post(func() {
			if a.confirm != q {
				return
			}
			switch {
			case errors.Is(err, platform.ErrNoDialog):
				q.inWindow = true
			case err != nil:
				a.fail(locales.MsgCouldNotShowTheConfirmation() + err.Error())
				a.confirm = nil
			default:
				if choice < 0 || choice >= len(q.actions) {
					choice = 2
				}
				q.actions[choice].fn()
			}
		})
	}()
	return true
}

func (a *App) discardDocs(docs []*doc) {
	for _, d := range docs {
		a.discardedSessionDocs[d.path] = struct{}{}
		if d.untitled {
			removeDraft(d)
		} else {
			a.removeRecovery(d)
		}
	}
	a.saveConfig()
}

func (a *App) saveActive() {
	if a.focus != nil && a.focus.diffs[a.focus.active] != nil && a.focus.diffs[a.focus.active].search != nil {
		v := a.focus.diffs[a.focus.active]
		a.saveSearchEdit(v.search)
		return
	}
	if a.focus != nil {
		if d := a.docs[tabPath(a.focus.active)]; d != nil {
			a.saveDocs([]*doc{d}, nil)
		}
	}
}

func (a *App) saveAll() {
	a.saveDocs(a.dirtyDocs(), nil)
}

func (a *App) saveDocs(docs []*doc, done func()) {
	if len(docs) == 0 {
		if done != nil {
			done()
		}
		return
	}
	d := docs[0]
	if d.untitled {
		a.saveUntitled(d, func(ok bool) {
			if ok {
				a.saveDocs(docs[1:], done)
			}
		})
	} else if a.saveDoc(d, false) {
		a.saveDocs(docs[1:], done)
	}
}

func (a *App) saveUntitled(d *doc, done func(bool)) {
	if d.saving {
		return
	}
	d.saving = true
	root, name, gen, pick := a.root, d.name, a.gen, a.host.PickNewFile
	go func() {
		path, err := pick(root, name)
		a.post.Post(func() {
			if a.gen != gen || a.docs[d.path] != d {
				return
			}
			if err != nil || path == "" {
				d.saving = false
				if err != nil {
					a.fail(locales.MsgCouldNotShowTheSaveDialog() + err.Error())
				}
				done(false)
				return
			}
			path = a.docPath(path)
			alreadyOpen := a.docs[path]
			if alreadyOpen == nil && a.root == "" {
				alreadyOpen = standaloneDocForPath(a, path)
			}
			if alreadyOpen != nil {
				d.saving = false
				a.fail(locales.MsgThatFileIsAlreadyOpen())
				done(false)
				return
			}
			claimed := a.root == ""
			if claimed && !reserveStandalonePath(a, d, path) {
				d.saving = false
				a.host.RevealOpenFile(path)
				a.fail(locales.MsgThatFileIsAlreadyOpen())
				done(false)
				return
			}
			// write is the save's write, its own step so the Retry of a
			// failed one runs it again, without the dialog another
			// saveUntitled would show. var, not :=, as the closure
			// refers to itself in the failure report.
			var write func()
			write = func() {
				snap := d.buf.Snapshot()
				go func() {
					err := writeFileAtomically(path, func(w io.Writer) error {
						_, err := snap.WriteTo(w)
						return err
					})
					a.post.Post(func() {
						if a.gen != gen || a.docs[d.path] != d {
							if claimed {
								releaseStandalonePath(a, d, path)
							}
							return
						}
						d.saving = false
						if err != nil {
							if claimed {
								releaseStandalonePath(a, d, path)
							}
							msg := locales.MsgCouldNotSaveFile() + err.Error()
							if d.buf != nil {
								a.failAction("save:"+d.path, msg, action{locales.Retry(), write})
							} else {
								a.fail(msg)
							}
							done(false)
							return
						}
						a.resolveFeedback("save:" + d.path)
						if claimed {
							finishStandalonePathWrite(a, d, path)
						}
						old := d.path
						removeDraft(d)
						delete(a.docs, old)
						d.path, d.uri, d.name, d.untitled, d.cache = path, uriOf(path), filepath.Base(path), false, nil
						d.diskText = snap.String()
						expectOnDisk(d)
						if d.buf.Version() == snap.Version() {
							d.buf.MarkSaved()
						} else {
							a.cacheRecovery(d)
						}
						a.setDocumentLanguage(d)
						if fi, e := os.Stat(path); e == nil {
							d.stamp = fileStamp{fi.Size(), fi.ModTime()}
						}
						a.docs[path] = d
						d.blameKey = ""
						a.syncBlame(d)
						for _, g := range a.groups() {
							for i, p := range g.order {
								if p == old {
									g.order[i] = path
								}
							}
							for i, p := range g.mru {
								if p == old {
									g.mru[i] = path
								}
							}
							if g.active == old {
								g.active = path
							}
							if g.preview == old {
								g.preview = path
							}
							if st := g.eds[old]; st != nil {
								delete(g.eds, old)
								delete(g.minimaps, old)
								g.eds[path] = st
								st.Label = d.name
								a.applyEditorPrefs(st)
							}
							if p, ok := g.pending[old]; ok {
								delete(g.pending, old)
								g.pending[path] = p
							}
						}
						if m := a.serverFor(path); m != nil {
							if a.workspaceTrusted() {
								go a.openLanguageDocument(m, a.wsCtx, d, d.uri)
							} else if !a.trustDismissed {
								a.trustAsk = true
							}
						}
						a.refreshDir(filepath.Dir(path))
						a.saveConfig()
						a.flash(locales.MsgSaved() + d.name)
						done(!d.buf.Modified())
					})
				}()
			}
			write()
		})
	}()
}

// expectationBytes are the on-disk bytes a document's text represents:
// the decoded text with its BOM prepended when it had one. The save
// condition compares content, so the expectation is built from these
// bytes rather than re-observed — re-observing would race the file
// instead of measuring what the buffer was read from.
func expectationBytes(text string, bom bool) []byte {
	if !bom {
		return []byte(text)
	}
	return append([]byte{0xef, 0xbb, 0xbf}, text...)
}

// expectOnDisk rebuilds the document's save condition: the digest of the
// bytes last read from disk. The condition is content-strength by
// construction — digest only, never size+mtime — so an identical-content
// rewrite never conflicts and a same-length replacement always does.
func expectOnDisk(d *doc) {
	d.expectation = fileobs.ExpectContent(fileobs.SourceRead, d.path, expectationBytes(d.diskText, d.bom))
}

// saveDoc checks the last-read disk contents before writing, including the BOM.
func (a *App) saveDoc(d *doc, overwrite bool) bool {
	if a.git.resolvingPath != "" && a.git.resolvingPath == d.path {
		a.flash(gitConflictText("gitConflictSaving"))
		return false
	}
	if d.buf == nil || d.lossy {
		return false
	}
	if !d.buf.Modified() && !overwrite {
		return true
	}
	if !overwrite {
		// The cheap pre-check skips staging for the common conflict: it is
		// a courtesy, never the condition. The condition is evaluated
		// again inside the publish boundary below, so a file passing here
		// still cannot be silently overwritten when it changes mid-write.
		current, err := os.ReadFile(d.path)
		if err != nil && !os.IsNotExist(err) {
			a.failKeyed("save:"+d.path, locales.MsgCouldNotSave()+d.name+": "+err.Error(), nil)
			return false
		}
		if err != nil || !bytes.Equal(current, expectationBytes(d.diskText, d.bom)) {
			d.conflict = true
			a.reportConflict(d)
			return false
		}
	}
	var out bytes.Buffer
	if d.bom {
		out.Write([]byte{0xef, 0xbb, 0xbf})
	}
	var err error
	if _, err = d.buf.WriteTo(&out); err == nil {
		// The publish is conditional: the target is replaced only while
		// it still holds the bytes this buffer was read from. The
		// condition evaluates after the staging file is durable and
		// closed, immediately before the rename — the window in which an
		// external write is silently overwritten shrinks from the entire
		// staging write to the rename boundary, which POSIX cannot close
		// for regular files. The re-observation is ObserveContent, never
		// ObservePath: a digest-less observation here is incomparable
		// with the content-strength expectation, which would be a wiring
		// bug — ErrIncomparableIdentity surfaces as a save failure, and
		// must never be "fixed" by weakening the expectation.
		err = writeFileIfUnchanged(d.path, d.expectation, overwrite, func(w io.Writer) error {
			_, err := w.Write(out.Bytes())
			return err
		})
	}
	if err != nil {
		if errors.Is(err, fileobs.ErrConditionMismatch) {
			d.conflict = true
			a.reportConflict(d)
			return false
		}
		a.failAction("save:"+d.path, locales.MsgCouldNotSave()+d.name+": "+err.Error(), action{locales.Retry(), func() { a.saveDoc(d, overwrite) }})
		return false
	}
	d.seq++ // an older asynchronous read must not replace the saved text
	a.finishDocLoading(d)
	d.buf.MarkSaved()
	a.captureCurrentLocalHistory(d)
	a.removeRecovery(d)
	d.diskText = string(out.Bytes())
	if d.bom {
		d.diskText = d.diskText[3:]
	}
	expectOnDisk(d)
	if fi, err := os.Stat(d.path); err == nil {
		d.stamp = fileStamp{fi.Size(), fi.ModTime()}
	}
	d.gone, d.conflict = false, false
	d.blameKey = ""
	a.syncBlame(d)
	if a.root == "" {
		// A clean saved file is no longer crash-recovery state.
		a.saveConfig()
	}
	a.resolveFeedback("save:" + d.path)
	a.resolveFeedback("conflict:" + d.path)
	a.flash(locales.MsgSaved() + d.name)
	return true
}

// reportConflict records the file-changed-on-disk conflict the save
// just found, the cheap pre-check's or the publish boundary's: the
// editor of the file carries it as its own banner while it shows, so
// the feedback one reports it only where that one cannot be seen, as a
// file Save All could not write in another group or behind a tab.
// Review goes to the file, where the editor banner's Overwrite resolves
// it. The key is the same however the conflict was found, so a conflict
// surfacing twice — pre-check, then publish — is one error, and the
// save that goes through resolves both.
func (a *App) reportConflict(d *doc) {
	if a.conflictBannerVisible(d) {
		return
	}
	a.failAction("conflict:"+d.path, locales.MsgTheFileChangedOnDiskReviewItBefore(), action{locales.Review(), func() {
		a.openFile(d.path, nil)
	}})
}

// conflictBannerVisible reports whether d's file-changed-on-disk banner
// can show: its document is the active tab of some group, the only place
// the editor area builds one.
func (a *App) conflictBannerVisible(d *doc) bool {
	for _, g := range a.groups() {
		if tabPath(g.active) == d.path {
			return true
		}
	}
	return false
}
