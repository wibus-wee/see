package app

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"see/internal/nav"
	"see/internal/workspace"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
)

type renamePrompt struct {
	path, name, err string
	selected        bool
}

func (a *App) beginRename(path string) {
	if path == a.root || !workspace.Within(a.root, path) || isPlaceholder(path) {
		return
	}
	a.renameEntry = &renamePrompt{path: path, name: filepath.Base(path)}
}

func (a *App) renameField(c *ui.Context, q *renamePrompt) {
	ui.Column(c).Grow(1).Children(func() {
		field := ui.TextInput(c, &q.name).Label("Rename "+filepath.Base(q.path)).Grow(1).Height(20).Padding(0, 4).FontSize(12).AutoFocus()
		if !q.selected {
			q.selected = true
			stem := strings.TrimSuffix(q.name, filepath.Ext(q.name))
			if stem == "" {
				stem = q.name
			}
			field.SetTextSelection(0, utf8.RuneCountInString(stem))
		}
		if field.Submitted() {
			a.submitRename(q)
		}
		if field.Shortcut(0, ui.KeyEscape) {
			a.renameEntry = nil
		}
		if q.err != "" {
			ui.Text(c, q.err).TextColor(colors(c).Err).FontSize(11).Wrap()
		}
	})
}

func (a *App) submitRename(q *renamePrompt) {
	if a.renameEntry != q {
		return
	}
	name := q.name
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") {
		q.err = "Enter a file name without path separators."
		return
	}
	dst := filepath.Join(filepath.Dir(q.path), name)
	if dst == q.path {
		a.renameEntry = nil
		return
	}
	gen := a.gen
	a.renameEntry = nil
	go func() {
		target, err := os.Lstat(dst)
		if err == nil {
			source, sourceErr := os.Lstat(q.path)
			if sourceErr == nil && strings.EqualFold(filepath.Base(q.path), name) && os.SameFile(source, target) {
				err = os.Rename(q.path, dst)
			} else {
				err = fmt.Errorf("an item with that name already exists")
			}
		} else if os.IsNotExist(err) {
			err = os.Rename(q.path, dst)
		}
		a.post.Post(func() {
			if a.gen != gen {
				return
			}
			if err != nil {
				q.err = "Could not rename: " + err.Error()
				a.renameEntry = q
				return
			}
			a.renamed(q.path, dst)
		})
	}()
}

func (a *App) renamed(old, dst string) {
	for _, entries := range [][]nav.Entry{a.hist.Back, a.hist.Forward} {
		for i := range entries {
			if workspace.Within(old, entries[i].Path) {
				rel, _ := filepath.Rel(old, entries[i].Path)
				entries[i].Path = filepath.Join(dst, rel)
			}
		}
	}
	for path, d := range a.docs {
		if !workspace.Within(old, path) {
			continue
		}
		rel, _ := filepath.Rel(old, path)
		next := filepath.Join(dst, rel)
		if m := a.serverFor(path); m != nil {
			m.Close(d.uri)
		}
		if !d.untitled {
			a.removeRecovery(d)
		}
		delete(a.docs, path)
		d.seq++
		d.loading = false
		d.path, d.uri, d.name = next, uriOf(next), filepath.Base(next)
		d.gone = false
		d.markdownOptions = nil
		d.hex = nil
		if d.buf != nil {
			d.buf.SetLanguage(editor.LanguageFor(d.name))
		}
		a.docs[next] = d
		for _, g := range a.groups() {
			for i, p := range g.order {
				if p == path {
					g.order[i] = next
				} else if p == markdownTab(path) {
					g.order[i] = markdownTab(next)
				}
			}
			for i, p := range g.mru {
				if p == path {
					g.mru[i] = next
				} else if p == markdownTab(path) {
					g.mru[i] = markdownTab(next)
				}
			}
			if g.active == path {
				g.active = next
			} else if g.active == markdownTab(path) {
				g.active = markdownTab(next)
			}
			if g.preview == path {
				g.preview = next
			}
			if st := g.eds[path]; st != nil {
				delete(g.eds, path)
				g.eds[next] = st
				st.Label = d.name
			}
			if v, ok := g.pending[path]; ok {
				delete(g.pending, path)
				g.pending[next] = v
			}
			if v, ok := g.hexPreview[path]; ok {
				delete(g.hexPreview, path)
				g.hexPreview[next] = v
			}
			if v, ok := g.hexControls[path]; ok {
				if v.cancel != nil {
					v.cancel()
					v.cancel = nil
					v.searching = false
					v.seq++
				}
				delete(g.hexControls, path)
				g.hexControls[next] = v
			}
			if v, ok := g.hexLists[path]; ok {
				delete(g.hexLists, path)
				g.hexLists[next] = v
			}
			if v, ok := g.markdownPreview[path]; ok {
				delete(g.markdownPreview, path)
				g.markdownPreview[next] = v
			}
		}
		if d.buf == nil {
			a.loadDoc(d)
		} else {
			if d.buf.Modified() {
				a.cacheRecovery(d)
			}
			if m := a.serverFor(next); m != nil && a.cfg.IsTrusted(a.root) {
				go a.openLanguageDocument(m, a.wsCtx, d, d.uri, d.buf.Snapshot())
			}
		}
	}
	for dir := range a.dirs {
		if workspace.Within(old, dir) {
			delete(a.dirs, dir)
		}
	}
	a.refreshDir(filepath.Dir(old))
	a.quick.files = nil
	a.filter.forQuery = ""
	a.revealInTree(dst)
	a.refreshGit()
	a.saveConfigSoon()
}
