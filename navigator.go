package main

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/search"
	"see/internal/workspace"
)

// navigatorPane is the sidebar, over the window's material where there
// is one: the navigator selector bar, then the navigator chosen.
func (a *app) navigatorPane(c *ui.Context, vibrant bool) {
	pane := ui.Column(c).Fill().Background(sidebarColor(c, vibrant))
	pane.Children(func() {
		a.navigatorBar(c)
		if a.navigator == navGit {
			a.gitNavigator(c)
		} else if a.navigator == navFind {
			a.findNavigator(c)
		} else {
			a.projectNavigator(c)
		}
	})
}

func sidebarColor(c *ui.Context, vibrant bool) ui.Color {
	if vibrant {
		return colors(c).sidebar.Alpha(0.78)
	}
	return colors(c).sidebar
}

// navigatorBar switches between the file and find navigators.
func (a *app) navigatorBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Padding(0, 10, 10, 10).Children(func() {
		parts := ui.SegmentedBase(c, &a.navigator, 3)
		parts.Track.FillWidth().Height(30).Padding(3).Gap(2).Radius(15).Background(t.Text.Alpha(0.06)).Label("Navigator")
		if c.Preferences().HighContrast {
			parts.Track.Border(1, t.Border)
		}
		segments := []struct {
			icon  *ui.SVG
			label string
		}{
			{icFolder, "Show the Project Navigator"},
			{icSearch, "Show the Find Navigator"},
			{icGit, "Show the Git Navigator"},
		}
		parts.Track.Children(func() {
			for i, s := range segments {
				i, s := i, s
				seg := parts.Segment(i).Width(38).Height(24).Radius(12).Label(s.label)
				if i == a.navigator {
					seg.Material(glass.Glass{Interactive: true})
				}
				seg.Children(func() {
					color := colors(c).muted
					if i == a.navigator {
						color = colors(c).text
					}
					symbolIcon(c, s.icon, 14, color)
				})
			}
		})
	})
}

func (a *app) projectNavigator(c *ui.Context) {
	// The tree's folder rows take drops before this root target.
	container := ui.Column(c).Grow(1).FillWidth()
	if paths := container.DroppedFiles(); paths != nil {
		a.copyInto(a.root, paths)
	}
	if v, ok := ui.Drop[*tabDrag](container); ok && v.from == nil && v.change == nil {
		a.copyInto(a.root, []string{v.path})
	}
	container.Children(func() { a.projectNavigatorContent(c) })
}

func (a *app) projectNavigatorContent(c *ui.Context) {
	a.syncFilter()
	a.treeHeading(c)
	switch {
	case a.filtering():
		switch {
		case a.quick.err != nil:
			ui.Column(c).Grow(1).Padding(8, 12).Gap(6).Children(func() {
				ui.Text(c, "Could not list the files: "+describeErr(a.quick.err)).TextColor(colors(c).err).FontSize(12)
				if ui.Button(c, "Retry").Clicked() {
					a.retryFiles()
				}
			})
		case a.quick.files == nil:
			ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Listing files…").TextColor(colors(c).muted)
			})
		case a.filter.matches == 0:
			ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, "No Results").TextColor(colors(c).muted)
			})
		default:
			a.fileTree(c, a.filter.roots)
		}
	default:
		rootDir := a.dirs[a.root]
		switch {
		case rootDir == nil || (!rootDir.loaded && rootDir.loading):
			ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "Loading…").TextColor(colors(c).muted)
			})
		case !rootDir.loaded:
			a.dirError(c, a.root, rootDir.err)
		case len(rootDir.entries) == 0 && a.createEntry == nil:
			ui.Text(c, "This folder is empty.").TextColor(colors(c).muted).Padding(8, 12)
		default:
			a.fileTree(c, a.withEntryPrompt(a.root, entryPaths(rootDir)))
		}
	}
	if a.filter.toList {
		a.filter.toList = false
		if a.tree.Rows() > 0 {
			a.tree.List.Handle.Focus(c)
		}
	}
	a.filterBar(c)
}

// treeHeading is the navigator's title, as a source list's section: small
// and muted, with what the filter matches at its side, and the actions on
// the tree at its end.
func (a *app) treeHeading(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(4, 12, 4, sidebarInset+6).Gap(6).AlignItems(ui.Center).Children(func() {
		folderGlyph(c, true, 15)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).Bold().SingleLine().Shrink(1)
		if a.filtering() {
			detail := fmt.Sprintf("%d of %d", a.filter.matches, len(a.quick.files))
			if a.quick.files == nil {
				detail = "Filtering…"
			}
			ui.Text(c, detail).FontSize(11).TextColor(colors(c).dim).SingleLine().Shrink(1)
		}
		ui.Box(c).Grow(1)
		if glyphButton(c, icAdd, "New File", "New File in Project", false, false).Clicked() {
			a.beginEntry(a.entryTarget(), false)
		}
		if glyphButton(c, icNewFolder, "New Folder", "New Folder in Project", false, false).Clicked() {
			a.beginEntry(a.entryTarget(), true)
		}
		if glyphButton(c, icCollapse, "Collapse All Folders", "Collapse All Folders", false, a.filtering()).Clicked() {
			a.tree.Open.Clear()
			a.reveal = ""
			a.treeRow = -1
		}
		if glyphButton(c, icRefresh, "Refresh", "Read the folders again", false, false).Clicked() {
			a.refreshAll()
		}
	})
}

// glyphButton is an icon button of a heading: smaller than the toolbar's,
// as the actions beside a source list's title.
func glyphButton(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(3).Radius(4).Shrink(0).KeepFocus().Disabled(disabled)
	color := colors(c).muted
	if on {
		color = c.Theme().Accent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colors(c).pill)
	}
	b.Children(func() { symbolIcon(c, icon, 13, color) })
	return b
}

// filterBar is the field at the bottom of the navigator, as Xcode's and
// Peek's: it filters the tree, Down moves into it, and Escape clears it.
func (a *app) filterBar(c *ui.Context) {
	ui.Row(c).Shrink(0).FillWidth().Padding(6, sidebarInset, 10, sidebarInset).Children(func() {
		// A capsule filled with the label color, borderless, as macOS
		// 27's search fields in a sidebar.
		field := ui.SearchField(c, &a.filter.query).FillWidth().Height(28).Radius(14).Background(colors(c).pill).BorderWidth(0).
			Label("Filter Files").Bind(&a.filter.field)
		if a.filter.focus {
			a.filter.focus = false
			a.filter.field.Focus(c)
		}
		if field.Changed() {
			a.filter.forQuery, a.filter.tree, a.filter.roots = "", nil, nil
		}
		if field.Shortcut(0, ui.KeyDown) {
			a.filter.toList = true
		}
	})
}

// retryFiles lists the workspace's files again after listing failed.
func (a *app) retryFiles() {
	a.quick.files, a.quick.err, a.quick.listing = nil, nil, false
	a.filter.forQuery = ""
	a.ensureFiles()
}

// sidebarInset is how far a source list's rows, and their selection,
// stand in from the sidebar's edges, as AppKit's on macOS 26 and later.
const sidebarInset = 10

func (a *app) fileTree(c *ui.Context, roots []string) {
	a.tree.Indent = 12
	tree := ui.Outline(c, &a.tree, roots, a.children, func(p string) { a.treeItem(c, p) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label("Files")
	a.tree.List.Handle.OnShortcut(c, ui.Cmd, ui.KeyC, func() {
		if a.treeRow >= 0 && a.treeRow < a.tree.Rows() {
			p := a.tree.Item(a.treeRow)
			if !strings.HasSuffix(p, entrySuffix) && !strings.HasSuffix(p, loadingSuffix) && !strings.HasSuffix(p, errorSuffix) {
				a.copyFile(p)
			}
		}
	})
	// Choose the row of the active document once it shows.
	if a.reveal != "" {
		for i := range a.tree.Rows() {
			if a.tree.Item(i) == a.reveal {
				a.treeRow = i
				a.tree.List.ScrollTo(i, ui.Center)
				a.reveal = ""
				break
			}
		}
	}
	// Enter renames the selected item on macOS; elsewhere it opens it.
	if tree.Submitted() && a.treeRow >= 0 && a.treeRow < a.tree.Rows() {
		p := a.tree.Item(a.treeRow)
		if runtime.GOOS == "darwin" {
			a.beginRename(p)
		} else if a.isTreeDir(p) {
			a.toggleDir(p)
		} else {
			a.openFromTree(p, false)
		}
	}
}

// The chosen row is already visible, so opening it need not reposition the tree.
func (a *app) openFromTree(path string, preview bool) {
	a.git.diffSeq++
	if preview {
		a.previewOpen(path)
	} else {
		a.userOpen(path)
	}
	for i := range a.tree.Rows() {
		if a.tree.Item(i) == path {
			a.treeRow = i
			break
		}
	}
	a.reveal = ""
}

// isTreeDir reports whether a row is a folder, in the tree or in a
// filter's tree.
func (a *app) isTreeDir(p string) bool {
	if a.filtering() {
		return a.filter.tree[p] != nil
	}
	e, ok := a.entry(p)
	return ok && e.Dir
}

func (a *app) toggleDir(p string) {
	if a.tree.Open.Has(p) {
		a.tree.Open.Remove(p)
	} else {
		a.tree.Open.Add(p)
	}
}

func (a *app) treeItem(c *ui.Context, p string) {
	switch {
	case strings.HasSuffix(p, entrySuffix):
		if a.createEntry != nil && p == a.createEntry.dir+entrySuffix {
			a.entryPanel(c)
		}
		return
	case strings.HasSuffix(p, loadingSuffix):
		ui.Text(c, "Loading…").TextColor(colors(c).muted).FontSize(12)
		return
	case strings.HasSuffix(p, errorSuffix):
		dir := strings.TrimSuffix(p, errorSuffix)
		msg := "Could not read this folder"
		if d := a.dirs[dir]; d != nil && d.err != nil {
			msg += ": " + describeErr(d.err)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).TextColor(colors(c).err).FontSize(12).SingleLine().Shrink(1)
			if ui.Button(c, "Retry").Padding(0, 6).Clicked() {
				a.retryDir(dir)
			}
		})
		return
	}
	e, _ := a.entry(p)
	if a.filtering() {
		e = workspace.Entry{Name: filepath.Base(p), Path: p, Dir: a.filter.tree[p] != nil}
	}
	name := e.Name
	if name == "" {
		name = filepath.Base(p)
	}
	dim := (e.Dir && workspace.Ignored(name)) || strings.HasPrefix(name, ".")
	// Extend the hit area into the outline's row padding and the
	// sidebar's inset, to the sidebar's edge as AppKit's rows, keeping
	// the disclosure arrow as a separate target.
	row := ui.Row(c).Grow(1).AlignItems(ui.Center).Gap(6).Margin(-3, 0).MarginEnd(-(6 + sidebarInset)).Padding(3, 0)
	row.Children(func() {
		if e.Dir {
			folderGlyph(c, a.tree.Open.Has(p), 16)
		} else {
			fileIcon(c, name, 16)
		}
		if a.renameEntry != nil && a.renameEntry.path == p {
			a.renameField(c, a.renameEntry)
			return
		}
		t := ui.Text(c, name).SingleLine().Shrink(1)
		if dim {
			t.TextColor(colors(c).dim)
		}
		if e.Link {
			t.Italic()
			symbolIcon(c, icLink, 11, colors(c).dim)
		}
		if d := a.docs[p]; d != nil && d.gone {
			t.Strikethrough()
		}
		if rel, err := filepath.Rel(a.root, p); err == nil {
			status := a.git.decor[filepath.Join(a.realRoot, rel)]
			if status != 0 {
				ui.Box(c).Grow(1)
				ui.Row(c).Padding(0, 12+sidebarInset+6, 0, 0).Shrink(0).Children(func() {
					if e.Dir {
						ui.Box(c).Size(5, 5).Radius(3).Background(a.gitStatusColor(status))
					} else {
						ui.Text(c, string(status)).FontSize(11).TextColor(a.gitStatusColor(status))
					}
				})
			}
		}
	})
	services := c.Services()
	row.ContextMenu(func(m *ui.Menu) {
		dir := p
		if !e.Dir {
			dir = filepath.Dir(p)
		}
		if m.Item("New File…").Chosen() {
			a.beginEntry(dir, false)
		}
		if m.Item("New Folder…").Chosen() {
			a.beginEntry(dir, true)
		}
		m.Separator()
		if m.Item("Rename…").Chosen() {
			a.beginRename(p)
		}
		if m.Item("Copy").Chosen() {
			a.copyFile(p)
		}
		if m.Item("Copy Path").Chosen() {
			services.WriteClipboard(p)
		}
		if m.Item("Show in Finder").Chosen() {
			mygo.Shell.ShowItemInFolder(p)
		}
		if !e.Dir && m.Item("Open with External Editor").Chosen() {
			_ = mygo.Shell.OpenPath(p)
		}
		if !e.Dir {
			m.Separator()
			if m.Item("Open").Chosen() {
				a.openFromTree(p, false)
			}
		}
		m.Separator()
		if m.Item("Move to Trash").Chosen() {
			a.requestTrash(p)
		}
	})
	if e.Dir {
		if paths := row.DroppedFiles(); paths != nil {
			a.copyInto(p, paths)
		}
		if v, ok := ui.Drop[*tabDrag](row); ok && v.from == nil && v.change == nil {
			a.copyInto(p, []string{v.path})
		}
		_, treeOver := ui.DragOver[*tabDrag](row)
		if row.FileDragOver() || treeOver {
			row.Background(colors(c).dropZone)
		}
		row.OnClick(func() {
			for i := range a.tree.Rows() {
				if a.tree.Item(i) == p {
					a.treeRow = i
					break
				}
			}
			a.toggleDir(p)
		})
	} else {
		row.OnClick(func() { a.openFromTree(p, true) })
		if row.DoubleClicked() {
			a.openFromTree(p, true)
			a.focus.preview = ""
		}
	}
	row.Drag(&tabDrag{path: p})
}

func (a *app) dirError(c *ui.Context, dir string, err error) {
	ui.Column(c).Padding(8, 12).Gap(6).Children(func() {
		ui.Text(c, "Could not read this folder: "+describeErr(err)).TextColor(colors(c).err)
		if ui.Button(c, "Retry").Clicked() {
			a.retryDir(dir)
		}
	})
}

// findNavigator is Xcode's Find navigator: the query, its options, and
// the matches by file, found with ripgrep.
func (a *app) findNavigator(c *ui.Context) {
	f := &a.find
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		field := ui.SearchField(c, &f.opts.Query).Label("Find in workspace").FillWidth().Bind(&f.field)
		if f.focus {
			f.focus = false
			f.field.Focus(c)
		}
		if field.Changed() {
			a.findSoon()
		}
		if field.Submitted() {
			a.runFind()
		}
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			changed := false
			changed = ui.Toggle(c, &f.opts.CaseSensitive, "Aa").Label("Match Case").Tooltip("Match Case").KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.WholeWord, "W").Label("Whole Words").Tooltip("Match Whole Words").KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.Regexp, ".*").Label("Regular Expression").Tooltip("Regular Expression").KeepFocus().Changed() || changed
			ui.Box(c).Grow(1)
			ui.Toggle(c, &f.showFilters, "Filters").Tooltip("Files to include and exclude").KeepFocus()
			if changed {
				a.runFind()
			}
		})
		if f.showFilters {
			if ui.TextInput(c, &f.opts.Include).Placeholder("Include: *.go, cmd/**").FillWidth().Changed() {
				a.findSoon()
			}
			if ui.TextInput(c, &f.opts.Exclude).Placeholder("Exclude: *_test.go").FillWidth().Changed() {
				a.findSoon()
			}
		}
		a.findSummary(c)
	})
	if len(f.results) == 0 {
		return
	}
	roots := make([]findKey, len(f.results))
	for i, r := range f.results {
		roots[i] = findKey{path: r.Path, match: -1}
	}
	f.tree.Indent = 6
	list := ui.Outline(c, &f.tree, roots, a.findChildren, func(k findKey) { a.findRow(c, k) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label("Find results")
	if list.Submitted() && f.row >= 0 && f.row < f.tree.Rows() {
		a.openMatch(f.tree.Item(f.row))
	}
}

func (a *app) findSummary(c *ui.Context) {
	f := &a.find
	var msg string
	color := colors(c).muted
	switch {
	case f.err != nil && f.err == search.ErrNoRipgrep:
		msg, color = "Finding needs ripgrep: "+search.InstallHint()+".", colors(c).err
	case f.err != nil:
		msg, color = f.err.Error(), colors(c).err
	case f.running && f.stats.Matches == 0:
		msg = "Searching…"
	case strings.TrimSpace(f.ran.Query) == "":
		return
	case f.stats.Matches == 0 && !f.running:
		msg = "No results"
	default:
		msg = fmt.Sprintf("%d results in %d files", f.stats.Matches, f.stats.Files)
		if f.stats.Truncated {
			msg += " (stopped at the limit)"
		}
	}
	ui.Text(c, msg).FontSize(11).TextColor(color)
}

func (a *app) findRow(c *ui.Context, k findKey) {
	file := a.findFile(k.path)
	if file == nil {
		return
	}
	if k.match < 0 {
		ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Children(func() {
			fileIcon(c, filepath.Base(k.path), 14)
			ui.Text(c, filepath.Base(k.path)).Bold().SingleLine().Shrink(0)
			if dir := filepath.Dir(relPath(a.root, k.path)); dir != "." {
				ui.Text(c, dir).FontSize(11).TextColor(colors(c).dim).SingleLine().Shrink(1)
			}
			ui.Box(c).Grow(1)
			ui.Text(c, fmt.Sprint(len(file.Matches))).FontSize(11).TextColor(colors(c).muted).Padding(0, 6).Radius(8).Background(colors(c).pill)
		})
		return
	}
	m := file.Matches[k.match]
	row := ui.Row(c).Grow(1).Gap(0).AlignItems(ui.Center)
	row.Children(func() {
		// A text drops the spaces at its ends: the joins get them as room.
		gap := func(n int) {
			if n > 0 {
				ui.Box(c).Width(float32(n) * 3.5).Shrink(0)
			}
		}
		pre, post := m.Preview[:m.PreStart], m.Preview[m.PreEnd:]
		trimmed := strings.TrimRight(pre, " \t")
		ui.Text(c, trimmed).FontSize(12).TextColor(colors(c).muted).SingleLine().Shrink(1)
		gap(len(pre) - len(trimmed))
		ui.Text(c, m.Preview[m.PreStart:m.PreEnd]).FontSize(12).Bold().Background(colors(c).match).Radius(3).SingleLine().Shrink(0)
		trimmed = strings.TrimLeft(post, " \t")
		gap(len(post) - len(trimmed))
		ui.Text(c, trimmed).FontSize(12).TextColor(colors(c).muted).SingleLine().Shrink(1)
	})
	row.Tooltip(fmt.Sprintf("Line %d", m.Line+1))
	row.OnClick(func() {
		for i := 0; i < a.find.tree.Rows(); i++ {
			if a.find.tree.Item(i) == k {
				a.find.row = i
				break
			}
		}
		a.openMatch(k)
	})
}
