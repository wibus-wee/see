package app

import (
	"fmt"
	"path/filepath"
	"runtime"
	"see/locales"
	"slices"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/workspace"
)

// tabFileMenu adds to a file tab's context menu the items the
// navigator's rows offer for a file. Diff, log and other tabs without a
// file on disk get none of them.
func (a *App) tabFileMenu(c *ui.Context, m *ui.Menu, tab string, d *doc, diff *gitDiffTab) {
	if diff != nil || isDiffTab(tab) || d.untitled {
		return
	}
	path := tabPath(tab)
	services := c.Services()
	if m.Item(commandLabel("navigator.reveal")).Disabled(!a.canRevealInProjectNavigator(path)).Chosen() {
		a.revealInProjectNavigator(path)
	}
	if m.Item(commandLabel("history.file")).Disabled(a.git.root == "").Chosen() {
		a.showFileHistory(path)
	}
	if m.Item(locales.MsgCopyPath()).Chosen() {
		services.WriteClipboard(path)
	}
	if runtime.GOOS == "darwin" && m.Item(locales.QuickLook()).Chosen() {
		if err := a.host.PreviewItem(path); err != nil {
			a.note(locales.MsgCouldNotPreviewFile()+err.Error(), true)
		}
	}
	if m.Item(showInFolderLabel()).Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	m.Separator()
}

func (a *App) tabBar(c *ui.Context, g *group) {
	// Names shown by more than one tab get their folder too.
	count := map[string]int{}
	seen := map[string]bool{}
	for _, p := range g.order {
		path := tabPath(p)
		if v := g.diffs[p]; v != nil && v.localHistory != nil {
			path = v.localHistory.path
		}
		if !seen[path] {
			count[filepath.Base(path)]++
			seen[path] = true
		}
	}
	g.tabSel = slices.Index(g.order, g.active)
	tabs := ui.TabsBase(c, &g.tabSel, len(g.order))
	tabs.List.Background(colors(c).TabBar).FillWidth().Height(30).AlignItems(ui.Stretch).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Hairline).Label(locales.MsgOpenFiles())
	if v, ok := ui.Drop[*tabDrag](tabs.List); ok {
		a.dropTab(v, g, sideNone, len(g.order))
	}
	tabs.List.Children(func() {
		for i, p := range g.order {
			path := tabPath(p)
			d := a.docs[path]
			diff := g.diffs[p]
			if diff != nil {
				if diff.revision != nil {
					path = diff.revision.path
					d = &doc{name: localHistoryText("revisionTitle") + " · " + filepath.Base(path)}
				} else if diff.localHistory != nil {
					path = diff.localHistory.path
					d = &doc{name: diff.localHistory.name}
				} else if diff.branch != nil || diff.search != nil {
					path = ""
					d = &doc{name: diff.name}
				} else {
					d = &doc{name: filepath.Base(diff.name)}
					if !diff.cli && a.git.root != "" {
						path = filepath.Join(a.git.root, diff.name)
					}
				}
			}
			if d == nil {
				continue
			}
			active := p == g.active
			tab := tabs.Tab(i).Grow(1).Basis(0).MaxWidth(260).MinWidth(90).Padding(0, 8).Gap(6).
				AlignItems(ui.Center).Justify(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(colors(c).Hairline)
			if tab.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
			tab.ContextMenu(func(m *ui.Menu) {
				a.tabFileMenu(c, m, p, d, diff)
				if m.Item(commandLabel("tab.close")).Chosen() {
					a.closeTab(g, p)
				}
				if g.preview == p && m.Item(commandLabel("tab.keepPreview")).Chosen() {
					g.preview = ""
				}
				if m.Item(commandLabel("tab.closeOthers")).Chosen() {
					a.closeTabs(g, p, closeOtherTabs)
				}
				if m.Item(commandLabel("tab.closeRight")).Chosen() {
					a.closeTabs(g, p, closeRightTabs)
				}
				if m.Item(commandLabel("tab.closeSaved")).Chosen() {
					a.closeTabs(g, p, closeSavedTabs)
				}
			})
			tab.Drag(&tabDrag{from: g, path: p})
			if v, over := ui.DragOver[*tabDrag](tab); over && v.path != p {
				tab.DrawOver(func(pt *ui.Painter, r ui.Rect) {
					pt.Fill(ui.Rect{X: r.X, Y: r.Y, W: 2, H: r.H}, c.Theme().Accent, 0)
				})
			}
			if v, ok := ui.Drop[*tabDrag](tab); ok {
				a.dropTab(v, g, sideNone, i)
			}
			hovered := tab.Hovered()
			switch {
			case active:
				tab.Background(colors(c).TabActive)
			case hovered:
				tab.Background(colors(c).TabHover)
			}
			if diff != nil && (diff.branch != nil || diff.search != nil) {
				tab.Tooltip(diff.name)
			} else if d.untitled {
				tab.Tooltip(d.name)
			} else {
				tab.Tooltip(workspace.RelPath(a.root, path))
			}
			tab.Children(func() {
				// The close button shows on the left, as Xcode's, while the
				// tab is hovered.
				nameLabel := d.name
				if isMarkdownTab(p) {
					nameLabel += " Preview"
				}
				if diff != nil && !diff.page() && diff.revision == nil {
					nameLabel += " (" + diff.label() + ")"
				}
				cl := ui.ButtonBase(c).Label(locales.MsgClose()+nameLabel).Tooltip(locales.MsgClose()+nameLabel).Size(16, 16).Radius(4).Center().KeepFocus()
				if hovered || active {
					cl.Children(func() { symbolIcon(c, icClose, 10, colors(c).Muted) })
					if cl.Hovered() {
						cl.Background(colors(c).Pill)
					}
				}
				cl.OnClick(func() { a.closeTab(g, p) })
				if diff != nil && diff.search != nil {
					symbolIcon(c, icSearch, 13, colors(c).Muted)
				} else if diff != nil && diff.page() {
					symbolIcon(c, icGit, 13, colors(c).Muted)
				} else {
					fileIcon(c, d.name, 13)
				}
				if d.buf != nil && d.buf.Modified() || diff != nil && diff.search != nil && diff.search.dirty() {
					ui.Text(c, "●").FontSize(10).TextColor(c.Theme().Accent)
				}
				name := ui.Text(c, nameLabel).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
				if g.preview == p {
					name.Italic()
				}
				switch {
				case d.gone:
					name.Strikethrough().TextColor(colors(c).Muted)
				case !active:
					name.TextColor(colors(c).Muted)
				}
				if count[d.name] > 1 {
					ui.Text(c, filepath.Base(filepath.Dir(path))).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(2)
				}
				if d.loading {
					ui.Spinner(c).FontSize(10)
				}
				ui.Box(c).Size(16, 16).Shrink(0) // balances the close button
			})
		}
		if a.maximized == g {
			ui.Box(c).Grow(1)
			label := locales.Catalog.Format(i18n.Locale(), "restoreEditorLayout", nil)
			button := ui.ButtonBase(c).Size(28, 28).Shrink(0).Radius(4).Center().KeepFocus().Label(label).Tooltip(label)
			if button.Hovered() {
				button.Background(colors(c).Pill)
			}
			button.Children(func() { symbolIcon(c, icSplitH, 14, colors(c).Muted) })
			button.OnClick(func() { a.maximized = nil })
		}
	})
	if tabs.List.Changed() && g.tabSel >= 0 && g.tabSel < len(g.order) {
		a.userActivateIn(g, g.order[g.tabSel])
	}
}

func (a *App) diffJumpBar(c *ui.Context, g *group) {
	v := g.diffs[g.active]
	if v == nil {
		return
	}
	ui.Row(c).FillWidth().Height(26).Padding(0, 6).Gap(5).AlignItems(ui.Center).Background(colors(c).Editor).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		folderGlyph(c, false, 13)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		symbolIcon(c, icChevron, 9, colors(c).Dim)
		label := v.label()
		if v.search != nil {
			symbolIcon(c, icSearch, 12, colors(c).Muted)
			ui.Text(c, "“"+v.search.opts.Query+"”").FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			ui.Text(c, v.search.summary()).FontSize(12).TextColor(colors(c).Muted).SingleLine()
			label = ""
		} else if v.branch != nil {
			symbolIcon(c, icGit, 12, colors(c).Muted)
			ui.Text(c, branchDiffText("gitSince")+" "+v.branch.baseName).FontSize(12).SingleLine().Ellipsis("…").MinWidth(0).Shrink(1)
			label = ""
		} else if v.log != nil {
			symbolIcon(c, icGit, 12, colors(c).Muted)
			ui.Text(c, v.name).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			label = ""
		} else if v.commit != nil {
			symbolIcon(c, icGit, 12, colors(c).Muted)
			ui.Text(c, v.rev).FontSize(12).SingleLine()
			ui.Text(c, v.name).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
			label = ""
		} else {
			fileIcon(c, filepath.Base(v.name), 12)
			ui.Text(c, v.name).FontSize(12).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
		}
		if label != "" {
			ui.Text(c, label).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		}
		ui.Box(c).Grow(1)
		if v.commit == nil {
			label, tip := diffWhitespaceText("msgWhitespaceChanges"), diffWhitespaceText("msgHideWhitespaceChanges")
			if a.cfg.HideWhitespaceChanges {
				tip = diffWhitespaceText("msgShowWhitespaceChanges")
			}
			if glyphToggle(c.Key("diff-whitespace"), icDiffWhitespace, label, tip, a.cfg.HideWhitespaceChanges, false).Clicked() {
				a.toggleDiffWhitespace()
			}
		}
		if v.state != nil {
			hunks := v.state.Hunks()
			if v.partial != nil && len(hunks) > 0 {
				i, _ := v.state.CurrentHunk()
				ui.Text(c, fmt.Sprintf("%d / %d", max(0, i)+1, len(hunks))).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			}
			if glyphButton(c, icChangeUp, "Previous Change", "Previous Change", false, len(hunks) == 0).Clicked() {
				a.focus = g
				a.goToChange(-1)
			}
			if glyphButton(c, icChangeDown, "Next Change", "Next Change", false, len(hunks) == 0).Clicked() {
				a.focus = g
				a.goToChange(1)
			}
			if v.partial != nil && len(hunks) > 0 {
				a.gitDiffPartButton(c, v, hunks)
			}
			icon, label, tip := icDiffInline, locales.MsgInlineDiff(), locales.MsgShowChangesInline()
			if a.cfg.DiffUnified {
				icon, label, tip = icDiffSplit, locales.MsgSideBySide(), locales.MsgShowChangesSideBySide()
			}
			if glyphButton(c, icon, label, tip, false, false).Clicked() {
				a.toggleDiffLayout()
			}
		}
		if len(g.order) == 1 && len(a.groups()) == 1 {
			label := locales.MsgClose() + filepath.Base(v.name) + " (" + v.label() + ")"
			if v.branch != nil {
				label = locales.MsgClose() + v.name
			}
			if glyphButton(c, icClose, label, locales.MsgCloseDiff(), false, false).Clicked() {
				a.closeTab(g, g.active)
			}
		}
	})
}

func (a *App) gitDiffPartButton(c *ui.Context, v *gitDiffTab, hunks []editor.DiffHunk) {
	part, canStageLines := gitSelectedPart(v.state, hunks)
	_, selected := v.state.SelectedLines()
	label := gitPartialText("gitStageSelectedLines")
	if v.staged {
		label = gitPartialText("gitUnstageSelectedLines")
	}
	if !selected {
		i, _ := v.state.CurrentHunk()
		part.hunk = hunks[max(0, i)]
		label = gitPartialText("gitStageHunk")
		if v.staged {
			label = gitPartialText("gitUnstageHunk")
		}
	}
	button := ui.Button(c.Key("git-diff-part"), label).FontSize(11).Height(20).Padding(0, 7).Shrink(0).KeepFocus().
		Disabled(a.git.busy || selected && !canStageLines).Tooltip(gitPartialText("gitPartialSelectionHint"))
	if selected {
		button.TextColor(c.Theme().Accent)
	}
	button.OnClick(func() { a.applyGitDiffPart(v, part) })
}

// jumpBar is Xcode's jump bar: back and forward, then the path of the
// document, whose folders reveal themselves in the navigator.
func (a *App) jumpBar(c *ui.Context, g *group) {
	tab := g.active
	p := tabPath(tab)
	if p == "" {
		g.jumpFiles, g.jumpSymbols = false, false
		return
	}
	if g == a.focus {
		a.refreshOutline()
	}
	ui.Row(c).FillWidth().Height(26).Padding(0, 6).Gap(2).AlignItems(ui.Center).Background(colors(c).Editor).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).Separator).Children(func() {
		g.relatedBounds = g.relatedHandle.Bounds(c)
		related := ui.ButtonBase(c.Key("related-items")).Label(locales.MsgRelatedItems()).Tooltip(locales.MsgRelatedItems()).Size(22, 22).Radius(4).Center().Bind(&g.relatedHandle)
		if related.Hovered() || related.Pressed() || g.relatedOpen {
			related.Background(colors(c).Pill)
		}
		related.Children(func() { symbolIcon(c, icRelated, 14, colors(c).Muted) })
		related.PopoverMenuButton(func(menu *ui.Menu) {
			a.addCounterpartsMenu(menu, g)
			menu.Separator()
			if menu.Item(locales.InspectorCallers()).Chosen() {
				a.focus = g
				a.findUsagesAt(true, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
			if menu.Item(locales.MsgReferences()).Chosen() {
				a.focus = g
				a.findUsagesAt(false, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
			if menu.Item(locales.CallHierarchy()).Chosen() {
				a.focus = g
				a.showCallHierarchyAt(g)
			}
			if a.navigationEnabled(g.active, "typeHierarchy") && menu.Item(navigationText("typeHierarchy", "")).Chosen() {
				a.focus = g
				a.showTypeHierarchyAt(g)
			}
		}, ui.PopoverMenuButtonOptions{Open: &g.relatedOpen, Panel: popoverMenuPanel})
		if !g.relatedOpen {
			g.counterpartMenu = nil
		}
		outside := !workspace.Within(a.root, p)
		if d := a.docs[p]; d != nil && d.untitled {
			outside = false
		}
		folderGlyph(c, false, 13)
		if outside {
			ui.Text(c, tildePath(filepath.Dir(p))).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(1)
		} else {
			ui.Text(c, filepath.Base(a.root)).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		}
		symbolIcon(c, icChevron, 9, colors(c).Dim)
		parts := strings.Split(workspace.RelPath(a.root, p), "/")
		if d := a.docs[p]; d != nil && d.untitled {
			parts = []string{d.name}
		} else if outside {
			parts = []string{filepath.Base(p)}
		}
		filesMenu := !outside && !(a.docs[p] != nil && a.docs[p].untitled)
		for i, part := range parts {
			if i > 0 {
				symbolIcon(c, icChevron, 9, colors(c).Dim)
			}
			last := i == len(parts)-1
			children := func() {
				if last {
					fileIcon(c, part, 13)
				} else {
					folderGlyph(c, false, 13)
				}
				t := ui.Text(c, part).FontSize(12).SingleLine().Ellipsis("…")
				if !last {
					t.TextColor(colors(c).Muted)
				} else {
					if g.preview == p {
						t.Italic()
					}
					if d := a.docs[p]; d != nil && d.buf != nil && d.buf.Modified() {
						ui.Text(c, "●").FontSize(9).TextColor(c.Theme().Accent)
					}
				}
			}
			if !filesMenu {
				ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Padding(1, 3).Children(children)
				continue
			}
			item := filepath.Join(append([]string{a.root}, parts[:i+1]...)...)
			var open *bool
			if last {
				open = &g.jumpFiles
			}
			crumb := jumpCrumb(c, i, part, open, children, func(m *ui.Menu) { a.jumpFilesMenu(m, g, filepath.Dir(item), p, false) })
			if last && crumb.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
		}
		if !filesMenu {
			g.jumpFiles = false
		}
		if a.jumpSymbolsShown(g, p) {
			a.jumpSymbols(c, g)
		} else {
			g.jumpSymbols = false
		}
		showClose := len(g.order) == 1 && len(a.groups()) == 1
		if showClose || isMarkdown(p) || a.canViewBytes(p) {
			ui.Box(c).Grow(1)
		}
		if showClose {
			name := filepath.Base(p)
			if d := a.docs[p]; d != nil {
				name = d.name
			}
			if isMarkdownTab(tab) {
				name += " " + locales.MsgPreview()
			}
			if glyphButton(c, icClose, locales.MsgClose()+name, locales.MsgCloseFile(), false, false).Clicked() {
				a.closeTab(g, tab)
			}
		}
		if !isMarkdownTab(tab) && a.canViewBytes(p) {
			label, tip := locales.MsgFileBytes(), locales.MsgShowFileBytes()
			if g.hexPreview[p] {
				label, tip = locales.MsgFileText(), locales.MsgShowFile()
			}
			if glyphToggle(c, icHex, label, tip, g.hexPreview[p], false).Clicked() {
				if g.hexPreview == nil {
					g.hexPreview = map[string]bool{}
				}
				g.hexPreview[p] = !g.hexPreview[p]
			}
		}
		if isMarkdown(p) {
			label, tip := locales.MsgMarkdownPreview(), commandLabel("markdown.preview")
			if g.markdownPreview[p] || isMarkdownTab(tab) {
				label, tip = locales.MsgMarkdownSource(), locales.MsgShowMarkdownSource()
			}
			if glyphToggle(c, icMarkdown, label, tip+" ("+keyLabel("Cmd+Shift+", "V")+")", g.markdownPreview[p] || isMarkdownTab(tab), false).Clicked() {
				a.focus = g
				if isMarkdownTab(tab) || g.markdownPreview[p] {
					delete(g.markdownPreview, p)
					a.openIn(g, p, goTo{})
				} else {
					a.openMarkdownTab(g)
				}
			}
		}
	})
}
