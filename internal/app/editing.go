package app

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui/editor"
	"see/internal/workspace"
)

func (a *App) newFile() {
	if a.root == "" {
		return
	}
	dir, err := draftDir()
	if err != nil {
		a.note("Could not create recovery cache: "+err.Error(), true)
		return
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		a.note("Could not create file: "+err.Error(), true)
		return
	}
	path := filepath.Join(dir, fmt.Sprintf("%x", id))
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
			break
		}
		name = fmt.Sprintf("Untitled-%d", n)
	}
	d := &doc{path: path, uri: uriOf(path), name: name, untitled: true, cache: &draftCache{}, buf: editor.NewBuffer(""), kind: workspace.Text}
	a.docs[path] = d
	a.openFile(path, nil)
	a.focusEditor = a.focus.eds[path]
	a.cacheDraft(d)
	a.saveConfig()
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
	post := a.post
	go func() {
		cache.mu.Lock()
		defer cache.mu.Unlock()
		if cache.removed || snap.Version() < cache.version {
			return
		}
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			post(func() { a.note("Could not cache unsaved file: "+err.Error(), true) })
			return
		}
		tmp, err := os.CreateTemp(filepath.Dir(path), ".draft-")
		if err != nil {
			post(func() { a.note("Could not cache unsaved file: "+err.Error(), true) })
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
				a.note("Could not cache unsaved file: "+err.Error(), true)
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
	if a.confirm != nil {
		return true
	}
	a.showWelcome = false
	a.confirm = &confirmation{message: fmt.Sprintf("%d file(s) have unsaved changes.", len(docs))}
	a.confirm.actions = []action{
		{"Save", func() {
			a.saveDocs(docs, func() { a.confirm = nil; proceed() })
		}},
		{"Discard Changes", func() {
			a.confirm = nil
			for _, d := range docs {
				if d.untitled {
					removeDraft(d)
				} else {
					a.removeRecovery(d)
				}
			}
			a.allowDirty = true
			defer func() { a.allowDirty = false }()
			proceed()
		}},
		{"Cancel", func() { a.confirm = nil }},
	}
	q, ask, post := a.confirm, a.host.confirmDirty, a.post
	go func() {
		choice, err := ask(q.message)
		post(func() {
			if a.confirm != q {
				return
			}
			switch {
			case errors.Is(err, errNoDialog):
				q.inWindow = true
			case err != nil:
				a.note("Could not show the confirmation: "+err.Error(), true)
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

func (a *App) saveActive() {
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
	root, gen, pick := a.root, a.gen, a.host.pickNewFile
	go func() {
		path, err := pick(root)
		a.post(func() {
			if a.gen != gen || a.docs[d.path] != d {
				return
			}
			if err != nil || path == "" {
				d.saving = false
				if err != nil {
					a.note("Could not show the save dialog: "+err.Error(), true)
				}
				done(false)
				return
			}
			path = workspace.DocPath(a.root, a.realRoot, path)
			if a.docs[path] != nil {
				d.saving = false
				a.note("That file is already open.", true)
				done(false)
				return
			}
			snap := d.buf.Snapshot()
			go func() {
				f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
				if err == nil {
					_, err = snap.WriteTo(f)
					if e := f.Close(); err == nil {
						err = e
					}
				}
				a.post(func() {
					if a.gen != gen || a.docs[d.path] != d {
						return
					}
					d.saving = false
					if err != nil {
						a.note("Could not save file: "+err.Error(), true)
						done(false)
						return
					}
					old := d.path
					removeDraft(d)
					delete(a.docs, old)
					d.path, d.uri, d.name, d.untitled, d.cache = path, uriOf(path), filepath.Base(path), false, nil
					d.diskText = snap.String()
					if d.buf.Version() == snap.Version() {
						d.buf.MarkSaved()
					}
					d.buf.SetLanguage(editor.LanguageFor(d.name))
					if fi, e := os.Stat(path); e == nil {
						d.stamp = fileStamp{fi.Size(), fi.ModTime()}
					}
					a.docs[path] = d
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
							g.eds[path] = st
							st.Label = d.name
						}
						if p, ok := g.pending[old]; ok {
							delete(g.pending, old)
							g.pending[path] = p
						}
					}
					if m := a.serverFor(path); m != nil {
						if a.cfg.IsTrusted(a.root) {
							ctx, docURI, current := a.wsCtx, d.uri, d.buf.Snapshot()
							go a.openLanguageDocument(m, ctx, d, docURI, current)
						} else if !a.trustDismissed {
							a.trustAsk = true
						}
					}
					a.refreshDir(filepath.Dir(path))
					a.saveConfig()
					a.note("Saved "+d.name, false)
					done(!d.buf.Modified())
				})
			}()
		})
	}()
}

// saveDoc checks the last-read disk contents before writing, including the BOM.
func (a *App) saveDoc(d *doc, overwrite bool) bool {
	if d.buf == nil || d.lossy {
		return false
	}
	if !d.buf.Modified() && !overwrite {
		return true
	}
	current, err := os.ReadFile(d.path)
	if err != nil && !os.IsNotExist(err) {
		a.note("Could not save "+d.name+": "+err.Error(), true)
		return false
	}
	expected := []byte(d.diskText)
	if d.bom {
		expected = append([]byte{0xef, 0xbb, 0xbf}, expected...)
	}
	if !overwrite && (err != nil || !bytes.Equal(current, expected)) {
		d.conflict = true
		a.note("The file changed on disk. Review it before choosing Overwrite.", true)
		return false
	}
	var out bytes.Buffer
	if d.bom {
		out.Write([]byte{0xef, 0xbb, 0xbf})
	}
	if _, err = d.buf.WriteTo(&out); err == nil {
		err = os.WriteFile(d.path, out.Bytes(), 0666)
	}
	if err != nil {
		a.note("Could not save "+d.name+": "+err.Error(), true)
		return false
	}
	d.seq++ // an older asynchronous read must not replace the saved text
	d.loading = false
	d.buf.MarkSaved()
	a.removeRecovery(d)
	d.diskText = string(out.Bytes())
	if d.bom {
		d.diskText = d.diskText[3:]
	}
	if fi, err := os.Stat(d.path); err == nil {
		d.stamp = fileStamp{fi.Size(), fi.ModTime()}
	}
	d.gone, d.conflict = false, false
	a.note("Saved "+d.name, false)
	return true
}
