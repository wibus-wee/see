package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/workspace"
)

type entryPrompt struct {
	dir, name, err string
	folder         bool
}

func (a *app) requestTrash(path string) {
	if path == a.root || !workspace.Within(a.root, path) || a.deleteConfirm != nil {
		return
	}
	var dirty []*doc
	for _, d := range a.dirtyDocs() {
		if workspace.Within(path, d.path) {
			dirty = append(dirty, d)
		}
	}
	if a.guardDirty(dirty, func() { a.requestTrash(path) }) {
		return
	}
	q := &confirmation{message: fmt.Sprintf("Move %q to Trash?", filepath.Base(path))}
	q.actions = []action{
		{"Move to Trash", func() {
			if a.deleteConfirm != q {
				return
			}
			a.deleteConfirm = nil
			gen, trash, post := a.gen, a.host.trashItem, a.post
			go func() {
				err := trash(path)
				post(func() {
					if a.gen != gen {
						return
					}
					if err != nil {
						a.note("Could not move to Trash: "+err.Error(), true)
						return
					}
					a.removed(path)
					a.refreshDir(filepath.Dir(path))
					a.refreshGit()
					a.quick.files = nil
					a.filter.forQuery = ""
				})
			}()
		}},
		{"Cancel", func() {
			if a.deleteConfirm == q {
				a.deleteConfirm = nil
			}
		}},
	}
	a.deleteConfirm = q
}

func (a *app) entryTarget() string {
	if a.treeRow >= 0 && a.treeRow < a.tree.Rows() {
		p := a.tree.Item(a.treeRow)
		if !isPlaceholder(p) {
			if a.isTreeDir(p) {
				return p
			}
			return filepath.Dir(p)
		}
	}
	return a.root
}

func (a *app) beginEntry(dir string, folder bool) {
	a.filter.query = ""
	a.syncFilter()
	a.createEntry = &entryPrompt{dir: dir, folder: folder}
	if dir != a.root {
		a.revealInTree(dir)
		a.tree.Open.Add(dir)
	}
}

func (a *app) withEntryPrompt(dir string, entries []string) []string {
	if a.createEntry != nil && a.createEntry.dir == dir {
		at := 0
		if !a.createEntry.folder {
			for at < len(entries) && a.isTreeDir(entries[at]) {
				at++
			}
		}
		return slices.Insert(entries, at, dir+entrySuffix)
	}
	return entries
}

func (a *app) entryPanel(c *ui.Context) {
	q := a.createEntry
	title := "New File"
	if q.folder {
		title = "New Folder"
	}
	ui.Column(c).Grow(1).Gap(3).Children(func() {
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			if q.folder {
				folderGlyph(c, false, 16)
			} else {
				fileIcon(c, q.name, 16)
			}
			field := ui.TextInput(c, &q.name).Label(title+" name").Placeholder("Path").Grow(1).Height(20).Padding(0, 4).FontSize(12).AutoFocus()
			if field.Submitted() {
				a.submitEntry(q)
			}
			if field.Shortcut(0, ui.KeyEscape) {
				a.createEntry = nil
			}
		})
		if q.err != "" {
			ui.Text(c, q.err).TextColor(colors(c).err).FontSize(12).Wrap()
		}
	})
}

func (a *app) submitEntry(q *entryPrompt) {
	if a.createEntry != q {
		return
	}
	name := strings.TrimSpace(q.name)
	clean := filepath.Clean(name)
	if name == "" || filepath.IsAbs(name) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		q.err = "Enter a relative path within this folder."
		return
	}
	path, gen, post := filepath.Join(q.dir, clean), a.gen, a.post
	a.createEntry = nil
	go func() {
		var err error
		err = os.MkdirAll(filepath.Dir(path), 0755)
		if err == nil && q.folder {
			err = os.Mkdir(path, 0755)
		} else if err == nil {
			var f *os.File
			f, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0666)
			if err == nil {
				err = f.Close()
			}
		}
		post(func() {
			if a.gen != gen {
				return
			}
			if err != nil {
				q.err = "Could not create: " + err.Error()
				if a.createEntry == nil {
					a.createEntry = q
				} else {
					a.note(q.err, true)
				}
				return
			}
			a.refreshDir(q.dir)
			a.quick.files = nil
			a.filter.forQuery = ""
			a.tree.Open.Add(q.dir)
			a.revealInTree(path)
			if !q.folder {
				a.userOpen(path)
			}
		})
	}()
}

func (a *app) openDropped(g *group, zone side, paths []string) {
	target := g
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil || !info.Mode().IsRegular() {
			a.note("Could not open dropped file: "+path, true)
			continue
		}
		if zone != sideNone {
			target = a.splitBeside(g, zone)
			zone = sideNone
		}
		a.cancelNav()
		a.openIn(target, path, goTo{})
	}
}

func (a *app) copyInto(dir string, paths []string) {
	gen, post := a.gen, a.post
	go func() {
		var failures []string
		for _, src := range paths {
			dst := filepath.Join(dir, filepath.Base(filepath.Clean(src)))
			if err := copyEntry(src, dst); err != nil {
				failures = append(failures, fmt.Sprintf("%s: %v", filepath.Base(src), err))
			}
		}
		post(func() {
			if a.gen != gen {
				return
			}
			a.refreshDir(dir)
			a.quick.files = nil
			a.filter.forQuery = ""
			a.tree.Open.Add(dir)
			if len(failures) > 0 {
				a.note("Could not copy: "+strings.Join(failures, "; "), true)
			}
		})
	}()
}

func copyEntry(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if info.IsDir() {
		realSrc, err := filepath.EvalSymlinks(src)
		if err != nil {
			return err
		}
		realParent, err := filepath.EvalSymlinks(filepath.Dir(dst))
		if err != nil {
			return err
		}
		if workspace.Within(realSrc, realParent) {
			return fmt.Errorf("cannot copy a folder into itself")
		}
		if err = os.Mkdir(dst, info.Mode().Perm()); err != nil {
			return err
		}
		entries, err := os.ReadDir(src)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			if err = copyEntry(filepath.Join(src, entry.Name()), filepath.Join(dst, entry.Name())); err != nil {
				return err
			}
		}
		return nil
	}
	if info.Mode()&os.ModeSymlink != 0 {
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		return os.Symlink(target, dst)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("unsupported file type")
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	_, err = io.Copy(out, in)
	if closeErr := out.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		_ = os.Remove(dst)
	}
	return err
}
