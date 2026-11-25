package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"see/locales"
	"slices"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/plugins/glass"
	"github.com/egoist/mygo/ui"

	"see/internal/search"
	"see/internal/workspace"
)

// navigatorPane is the sidebar, over the window's material where there
// is one: the navigator selector bar, then the navigator chosen.
func (a *App) navigatorPane(c *ui.Context, vibrant bool) {
	pane := ui.Column(c).Fill().Background(sidebarColor(c, vibrant))
	pane.Children(func() {
		a.navigatorBar(c)
		if a.navigator == navGit {
			a.gitNavigator(c)
		} else if a.navigator == navProblems {
			a.problemsNavigator(c)
		} else if a.navigator == navOutline {
			a.outlineNavigator(c)
		} else if a.navigator == navBookmarks {
			a.bookmarksNavigator(c)
		} else if a.navigator == navFind {
			a.findNavigator(c)
		} else {
			a.projectNavigator(c)
		}
	})
}

func sidebarColor(c *ui.Context, vibrant bool) ui.Color {
	if vibrant {
		return colors(c).Sidebar.Alpha(0.78)
	}
	return colors(c).Sidebar
}

// thumbSpring moves the navigator bar's thumb: quick, with a little
// bounce at the end.
var thumbSpring = ui.Animation{Spring: ui.SpringOptions{Duration: 260 * time.Millisecond, Bounce: 0.2}}

// navigatorBar switches between sidebar navigators.
func (a *App) navigatorBar(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).FillWidth().Padding(0, 10, 10, 10).Children(func() {
		const segW, segGap = 38, 2
		segments := []struct {
			icon  *ui.SVG
			label string
		}{
			{icFolder, locales.MsgProjectNavigator()},
			{icSearch, locales.MsgFindNavigator()},
			{icGit, locales.MsgGitNavigator()},
			{icProblems, problemText("problemsList")},
			{icOutline, outlineText("outlineTitle")},
			{icBookmark, bookmarkText("bookmarksTitle")},
		}
		pill := ui.Row(c).FillWidth().Height(30).Radius(15).Clip().Background(t.Text.Alpha(0.06))
		if c.Preferences().HighContrast {
			pill.Border(1, t.Border)
		}
		pill.Children(func() {
			ui.ScrollHorizontal(c.Key("navigator-scroll")).Fill().Scrollbars(false).Children(func() {
				parts := ui.SegmentedBase(c.Key("navigator"), &a.navigator, len(segments))
				parts.Track.Height(30).Padding(3).Gap(segGap).Label(locales.MsgNavigator())
				at := parts.Track.AnimateTo("thumb", float32(a.navigator), thumbSpring)
				parts.Track.Children(func() {
					ui.Box(c.Key("navigator-thumb")).Absolute().Start(3 + at*(segW+segGap)).Top(3).Width(segW).Height(24).
						Radius(12).Material(glass.Glass{Interactive: true}).PassThrough().ZIndex(-1)
					for i, s := range segments {
						i, s := i, s
						seg := parts.Segment(i).Width(segW).Height(24).Radius(12).Label(s.label).Tooltip(s.label)
						seg.Children(func() {
							color := colors(c).Muted
							if i == a.navigator {
								color = colors(c).Text
							}
							symbolIcon(c, s.icon, 14, color)
						})
					}
				})
				if parts.Track.Changed() {
					switch a.navigator {
					case navGit:
						a.refreshGit()
					case navProblems:
						a.startProjectDiagnostics()
					}
				}
			})
		})
	})
}

func (a *App) projectNavigator(c *ui.Context) {
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

func (a *App) projectNavigatorContent(c *ui.Context) {
	a.syncFilter()
	a.treeHeading(c)
	switch {
	case a.filtering():
		switch {
		case a.quick.err != nil:
			ui.Column(c).Grow(1).Padding(8, 12).Gap(6).Children(func() {
				ui.Text(c, locales.MsgCouldNotListTheFiles()+describeErr(a.quick.err)).TextColor(colors(c).Err).FontSize(12)
				if ui.Button(c, locales.Retry()).Clicked() {
					a.retryFiles()
				}
			})
		case a.quick.files == nil:
			ui.Row(c).Padding(8, 12).Gap(8).Children(func() {
				ui.Spinner(c)
				ui.Text(c, locales.MsgListingFiles()).TextColor(colors(c).Muted)
			})
		case a.filter.matches == 0:
			ui.Column(c).Grow(1).FillWidth().AlignItems(ui.Center).Justify(ui.Center).Children(func() {
				ui.Text(c, locales.MsgNoResults()).TextColor(colors(c).Muted)
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
				ui.Text(c, locales.MsgLoading()).TextColor(colors(c).Muted)
			})
		case !rootDir.loaded:
			a.dirError(c, a.root, rootDir.err)
		case len(a.entryPaths(rootDir)) == 0 && a.createEntry == nil:
			ui.Text(c, locales.MsgThisFolderIsEmpty()).TextColor(colors(c).Muted).Padding(8, 12)
		default:
			a.fileTree(c, a.withEntryPrompt(a.root, a.entryPaths(rootDir)))
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
func (a *App) treeHeading(c *ui.Context) {
	ui.Row(c).FillWidth().Padding(4, 12, 4, sidebarInset+6).Gap(6).AlignItems(ui.Center).Children(func() {
		folderGlyph(c, true, 15)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if a.filtering() {
			detail := fmt.Sprintf("%d of %d", a.filter.matches, len(a.quick.files))
			if a.quick.files == nil {
				detail = locales.MsgFiltering()
			}
			ui.Text(c, detail).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		ui.Box(c).Grow(1)
		if glyphButton(c, icAdd, commandLabel("file.new"), locales.MsgNewFileInProject(), false, false).Clicked() {
			a.beginEntry(a.entryTarget(), false)
		}
		if glyphButton(c, icNewFolder, locales.MsgNewFolder(), locales.MsgNewFolderInProject(), false, false).Clicked() {
			a.beginEntry(a.entryTarget(), true)
		}
		if glyphButton(c, icCollapse, locales.MsgCollapseAllFolders(), locales.MsgCollapseAllFolders(), false, a.filtering()).Clicked() {
			a.tree.Open.Clear()
			a.reveal = ""
			a.treeRow = -1
		}
		if glyphButton(c, icRefresh, locales.MsgRefresh(), locales.MsgReadTheFoldersAgain(), false, false).Clicked() {
			a.refreshAll()
		}
	})
}

// glyphButton is an icon button of a heading: smaller than the toolbar's,
// as the actions beside a source list's title.
func glyphButton(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool) ui.Element {
	b := ui.ButtonBase(c).Label(label).Tooltip(tip).Padding(3).Radius(4).Shrink(0).KeepFocus().Disabled(disabled)
	color := colors(c).Muted
	if on {
		color = c.Theme().Accent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colors(c).Pill)
	}
	b.Children(func() { symbolIcon(c, icon, 13, color) })
	return b
}

// glyphToggle is glyphButton for a control that stays on or off, which
// assistive technology sees as a toggle button pressed while on.
func glyphToggle(c *ui.Context, icon *ui.SVG, label, tip string, on, disabled bool) ui.Element {
	b := ui.ToggleBase(c, &on).Label(label).Tooltip(tip).Padding(3).Radius(4).Shrink(0).KeepFocus().Disabled(disabled)
	color := colors(c).Muted
	if on {
		color = c.Theme().Accent
	}
	if !disabled && (b.Hovered() || b.Pressed()) {
		b.Background(colors(c).Pill)
	}
	b.Children(func() { symbolIcon(c, icon, 13, color) })
	return b
}

// filterBar is the field at the bottom of the navigator, as Xcode's and
// Peek's: it filters the tree, Down moves into it, and Escape clears it.
func (a *App) filterBar(c *ui.Context) {
	ui.Row(c).Shrink(0).FillWidth().Padding(6, sidebarInset, 10, sidebarInset).Children(func() {
		// A capsule filled with the label color, borderless, as macOS
		// 27's search fields in a sidebar.
		field := ui.SearchField(c, &a.filter.query).FillWidth().Height(28).Radius(14).Background(colors(c).Pill).BorderWidth(0).
			Label(commandLabel("files.filter")).Bind(&a.filter.field)
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
func (a *App) retryFiles() {
	a.quick.files, a.quick.err, a.quick.listing = nil, nil, false
	a.filter.forQuery = ""
	a.ensureFiles()
}

// sidebarInset is how far a source list's rows, and their selection,
// stand in from the sidebar's edges, as AppKit's on macOS 26 and later.
const sidebarInset = 10

func (a *App) activeProjectFile() string {
	if a.focus == nil {
		return ""
	}
	return a.tabFile(a.focus, a.focus.active)
}

func (a *App) canRevealInProjectNavigator(path string) bool {
	if a.root == "" || path == "" || path == a.root || !workspace.Within(a.root, path) || a.hiddenPath(path) {
		return false
	}
	d := a.docs[path]
	return d == nil || (!d.untitled && !d.gone)
}

func (a *App) revealInProjectNavigator(path string) {
	if !a.canRevealInProjectNavigator(path) {
		return
	}
	a.sidebarOff, a.navigator = false, navFiles
	a.filter.query, a.filter.focus, a.filter.toList = "", false, false
	a.syncFilter()
	a.revealInTree(path)
}

func (a *App) fileTree(c *ui.Context, roots []string) {
	if a.reveal != "" && !a.filtering() {
		if d := a.dirs[filepath.Dir(a.reveal)]; d != nil {
			for parent, children := range d.nested {
				if slices.Contains(children, a.reveal) {
					a.tree.Open.Add(parent)
					break
				}
			}
		}
	}
	a.tree.Indent = 12
	tree := ui.Outline(c, &a.tree, roots, a.children, func(p string) { a.treeItem(c, p) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label(locales.Files())
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
				a.tree.List.Choose(i)
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
func (a *App) openFromTree(path string, preview bool) {
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
func (a *App) isTreeDir(p string) bool {
	if a.filtering() {
		return a.filter.tree[p] != nil
	}
	e, ok := a.entry(p)
	return ok && e.Dir
}

func (a *App) toggleDir(p string) {
	if a.tree.Open.Has(p) {
		a.tree.Open.Remove(p)
	} else {
		a.tree.Open.Add(p)
	}
}

func (a *App) treeItem(c *ui.Context, p string) {
	switch {
	case strings.HasSuffix(p, entrySuffix):
		if a.createEntry != nil && p == a.createEntry.dir+entrySuffix {
			a.entryPanel(c)
		}
		return
	case strings.HasSuffix(p, loadingSuffix):
		ui.Text(c, locales.MsgLoading()).TextColor(colors(c).Muted).FontSize(12)
		return
	case strings.HasSuffix(p, errorSuffix):
		dir := strings.TrimSuffix(p, errorSuffix)
		msg := "Could not read this folder"
		if d := a.dirs[dir]; d != nil && d.err != nil {
			msg += ": " + describeErr(d.err)
		}
		ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
			ui.Text(c, msg).TextColor(colors(c).Err).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			if ui.Button(c, locales.Retry()).Padding(0, 6).Clicked() {
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
	dim := a.gitIgnores(p)
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
		t := ui.Text(c, name).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
		if dim {
			t.TextColor(colors(c).Dim)
		}
		if e.Link {
			t.Italic()
			symbolIcon(c, icLink, 11, colors(c).Dim)
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
		if m.Item(locales.MsgNewFile()).Chosen() {
			a.beginEntry(dir, false)
		}
		if m.Item(locales.MsgNewFolderEllipsis()).Chosen() {
			a.beginEntry(dir, true)
		}
		m.Separator()
		if m.Item(locales.MsgRenameEllipsis()).Chosen() {
			a.beginRename(p)
		}
		if m.Item(locales.Copy()).Chosen() {
			a.copyFile(p)
		}
		if m.Item(locales.MsgCopyPath()).Chosen() {
			services.WriteClipboard(p)
		}
		if runtime.GOOS == "darwin" && !e.Dir && m.Item(locales.QuickLook()).Chosen() {
			if err := a.host.PreviewItem(p); err != nil {
				a.note(locales.MsgCouldNotPreviewFile()+err.Error(), true)
			}
		}
		if m.Item(showInFolderLabel()).Chosen() {
			mygo.Shell.ShowItemInFolder(p)
		}
		if !e.Dir && m.Item(locales.MsgOpenWithExternalEditor()).Chosen() {
			_ = mygo.Shell.OpenPath(p)
		}
		if !e.Dir {
			if m.Item(commandLabel("history.file")).Chosen() {
				a.showFileHistory(p)
			}
			m.Separator()
			if m.Item(locales.MsgOpen()).Chosen() {
				a.openFromTree(p, false)
			}
		}
		if e.Dir {
			m.Separator()
			if m.Item(locales.MsgNewTerminalHere()).Chosen() {
				a.newTerminalTabAt(p)
			}
			if m.Item(locales.MsgOpenInExternalTerminalHere()).Disabled(!a.terminalDirOK(p)).Chosen() {
				a.openExternalTerminalAt(p)
			}
		}
		m.Separator()
		if m.Item(locales.MoveToTrash()).Chosen() {
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
			row.Background(colors(c).DropZone)
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

func (a *App) dirError(c *ui.Context, dir string, err error) {
	ui.Column(c).Padding(8, 12).Gap(6).Children(func() {
		ui.Text(c, locales.MsgCouldNotReadThisFolder()+describeErr(err)).TextColor(colors(c).Err)
		if ui.Button(c, locales.Retry()).Clicked() {
			a.retryDir(dir)
		}
	})
}

// findNavigator is Xcode's Find navigator: the query, its options, and
// the matches by file, found with ripgrep.
func (a *App) findNavigator(c *ui.Context) {
	f := &a.find
	ui.Column(c).FillWidth().Padding(8, 10).Gap(6).Children(func() {
		ui.Row(c).FillWidth().Gap(4).AlignItems(ui.Center).Children(func() {
			field := ui.SearchField(c, &f.opts.Query).Label(locales.MsgFindInWorkspace()).Grow(1).MinWidth(0).Bind(&f.field)
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
			a.savedFindMenu(c)
		})
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
			changed := false
			changed = ui.Toggle(c, &f.opts.CaseSensitive, "Aa").Label(locales.MsgMatchCase()).Tooltip(locales.MsgMatchCase()).KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.WholeWord, "W").Label(locales.MsgWholeWords()).Tooltip(locales.MsgMatchWholeWords()).KeepFocus().Changed() || changed
			changed = ui.Toggle(c, &f.opts.Regexp, ".*").Label(locales.MsgRegularExpression()).Tooltip(locales.MsgRegularExpression()).KeepFocus().Changed() || changed
			ui.Box(c).Grow(1)
			if ui.Toggle(c, &f.showReplace, locales.WorkspaceReplace()).KeepFocus().Changed() {
				f.confirm = false
			}
			ui.Toggle(c, &f.showFilters, locales.MsgFilters()).Tooltip(locales.MsgFilesToIncludeAndExclude()).KeepFocus()
			if changed {
				a.runFind()
			}
		})
		if f.showFilters {
			include, exclude := locales.MsgIncludeGoCmd(), locales.MsgExcludeTestGo()
			if ui.TextInput(c, &f.opts.Include).Label(include).Placeholder(include).FillWidth().Changed() {
				a.findSoon()
			}
			if ui.TextInput(c, &f.opts.Exclude).Label(exclude).Placeholder(exclude).FillWidth().Changed() {
				a.findSoon()
			}
		}
		if f.showReplace {
			a.findReplaceControls(c)
		}
		ui.Row(c).FillWidth().Gap(6).AlignItems(ui.Center).Children(func() {
			a.findSummary(c)
			ui.Box(c).Grow(1)
			if len(f.results) > 0 {
				b := ui.Button(c, searchEditText("searchEditOpen")).FontSize(11).Height(20).Padding(1, 7).Shrink(0).KeepFocus().
					Disabled(f.running).Tooltip(searchEditText("searchEditOpenHint"))
				b.OnClick(func() { a.openSearchEdit() })
			}
		})
	})
	if len(f.results) == 0 {
		return
	}
	roots := make([]findKey, len(f.results))
	for i, r := range f.results {
		roots[i] = findKey{path: r.Path, match: -1}
	}
	f.tree.Indent = 6
	list := ui.Outline(c, &f.tree, roots, a.findChildren, func(k findKey) { a.findRow(c, k) }).Grow(1).FillWidth().Padding(0, sidebarInset).Label(locales.MsgFindResults())
	if list.Submitted() && f.row >= 0 && f.row < f.tree.Rows() {
		a.openMatch(f.tree.Item(f.row))
	}
}

func (a *App) savedFindMenu(c *ui.Context) {
	f := &a.find
	current := f.currentQuery()
	queries := a.cfg.SavedFindQueries[a.root]
	saved := slices.Contains(queries, current)
	label := locales.MsgSavedFindQueries()
	glyphButton(c.Key("saved-find-queries"), icBookmark, label, label, saved, a.root == "" || f.replacing).
		Size(24, 24).Center().PopoverMenuButton(func(m *ui.Menu) {
		m.MaxWidth(320)
		action := locales.MsgSaveFindQuery()
		if saved {
			action = locales.MsgRemoveSavedFindQuery()
		}
		if m.Item(action).Disabled(strings.TrimSpace(current.Query) == "").Chosen() {
			a.setSavedFindQuery(current, !saved)
			return
		}
		m.Separator()
		if len(queries) == 0 {
			m.Item(locales.MsgNoSavedFindQueries()).Disabled(true)
		}
		for _, query := range queries {
			var details []string
			if query.CaseSensitive {
				details = append(details, locales.MsgMatchCase())
			}
			if query.WholeWord {
				details = append(details, locales.MsgWholeWords())
			}
			if query.Regexp {
				details = append(details, locales.MsgRegularExpression())
			}
			if query.Include != "" {
				details = append(details, locales.MsgSavedFindInclude(query.Include))
			}
			if query.Exclude != "" {
				details = append(details, locales.MsgSavedFindExclude(query.Exclude))
			}
			item := m.Item(query.Query).Checked(query == current)
			if len(details) > 0 {
				subtitle := strings.Join(details, " · ")
				item.Subtitle(subtitle).Tooltip(query.Query + "\n" + subtitle)
			} else {
				item.Tooltip(query.Query)
			}
			if item.Chosen() {
				a.rerunSavedFindQuery(query)
			}
		}
	}, ui.PopoverMenuButtonOptions{At: ui.AnchorBottomEnd, Self: ui.AnchorTopEnd, Panel: popoverMenuPanel})
}

func (a *App) findSummary(c *ui.Context) {
	f := &a.find
	var msg string
	color := colors(c).Muted
	switch {
	case f.err != nil && f.err == search.ErrNoRipgrep:
		msg, color = "Finding needs ripgrep: install it in Settings, or "+search.InstallHint()+".", colors(c).Err
	case f.err != nil:
		msg, color = f.err.Error(), colors(c).Err
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

func (a *App) findRow(c *ui.Context, k findKey) {
	file := a.findFile(k.path)
	if file == nil {
		return
	}
	if k.match < 0 {
		ui.Row(c).Grow(1).Gap(6).AlignItems(ui.Center).Children(func() {
			fileIcon(c, filepath.Base(k.path), 14)
			ui.Text(c, filepath.Base(k.path)).Bold().SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(0)
			if dir := filepath.Dir(workspace.RelPath(a.root, k.path)); dir != "." {
				ui.Text(c, dir).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			}
			ui.Box(c).Grow(1)
			ui.Text(c, fmt.Sprint(len(file.Matches))).FontSize(11).TextColor(colors(c).Muted).Padding(0, 6).Radius(8).Background(colors(c).Pill)
		})
		return
	}
	m := file.Matches[k.match]
	ui.Row(c).Grow(1).Gap(3).AlignItems(ui.Center).Children(func() {
		a.findMatchText(c, k, m)
		if a.find.showReplace {
			label := locales.MsgReplaceThisMatch()
			tip := label
			protected := a.replaceProtected(k.path)
			if protected {
				tip = locales.MsgReplaceProtected(filepath.Base(k.path))
			}
			b := ui.ButtonBase(c).Label(label).Tooltip(tip).Size(23, 22).Radius(4).Center().Shrink(0).
				Disabled(!a.find.canReplace() || a.find.confirm || protected).OnClick(func() { a.replaceFind(&k) })
			if b.Hovered() {
				b.Background(colors(c).Pill)
			}
			b.Children(func() { symbolIcon(c, icReplaceMatch, 14, colors(c).Muted) })
		}
	})
}

func (a *App) findMatchText(c *ui.Context, k findKey, m search.Match) {
	row := ui.Row(c).Grow(1).Shrink(1).Gap(0).AlignItems(ui.Center)
	before := m.Preview[m.PreStart:m.PreEnd]
	replacing := a.find.showReplace && a.find.ran == a.find.opts
	replacement := a.find.replacement
	row.Children(func() {
		// A text drops the spaces at its ends: the joins get them as room.
		gap := func(n int) {
			if n > 0 {
				ui.Box(c).Width(float32(n) * 3.5).Shrink(0)
			}
		}
		pre, post := m.Preview[:m.PreStart], m.Preview[m.PreEnd:]
		trimmed := strings.TrimRight(pre, " \t")
		ui.Text(c, trimmed).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		gap(len(pre) - len(trimmed))
		match := ui.Text(c, before).FontSize(12).Bold().Background(colors(c).Match).Radius(3).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		if replacing && before != replacement {
			match.Strikethrough()
			ui.Text(c, " → ").FontSize(11).Padding(0, 3).Shrink(0)
			label := replacement
			if label == "" {
				label = locales.MsgReplaceDeleteMatch()
			}
			ui.Text(c, label).FontSize(12).Bold().SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
		}
		trimmed = strings.TrimLeft(post, " \t")
		gap(len(post) - len(trimmed))
		ui.Text(c, trimmed).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
	})
	tip := fmt.Sprintf(locales.MsgLineD(), m.Line+1)
	if replacing {
		tip += "\n" + m.Preview + "\n→ " + m.Preview[:m.PreStart] + replacement + m.Preview[m.PreEnd:]
	}
	row.Tooltip(tip)
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
