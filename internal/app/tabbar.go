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
			a.fail(locales.MsgCouldNotPreviewFile() + err.Error())
		}
	}
	if m.Item(showInFolderLabel()).Chosen() {
		mygo.Shell.ShowItemInFolder(path)
	}
	m.Separator()
}

func (a *App) tabBar(c *ui.Context, g *group) {
	// Names shown by more than one tab get their folder too.
	type tabNameParent struct {
		name   string
		parent string
	}
	count := map[string]int{}
	parentCount := map[tabNameParent]int{}
	seen := map[string]bool{}
	for _, p := range g.order {
		path := tabPath(p)
		if v := g.diffs[p]; v != nil && v.localHistory != nil {
			path = v.localHistory.path
		}
		if !seen[path] {
			name := filepath.Base(path)
			parent := filepath.Base(filepath.Dir(path))
			count[name]++
			parentCount[tabNameParent{name: name, parent: parent}]++
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
			if tab.Clicked() && p == g.active {
				a.userActivateIn(g, p)
			}
			if tab.DoubleClicked() && g.preview == p {
				a.pinPreview(g, p)
			}
			tab.ContextMenu(func(m *ui.Menu) {
				a.tabFileMenu(c, m, p, d, diff)
				if m.Item(commandLabel("tab.close")).Chosen() {
					a.closeTab(g, p)
				}
				if g.preview == p && m.Item(commandLabel("tab.keepPreview")).Chosen() {
					a.pinPreview(g, p)
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
				tab.Tooltip(a.workspacePath(path))
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
					parent := filepath.Base(filepath.Dir(path))
					if parentCount[tabNameParent{name: filepath.Base(path), parent: parent}] > 1 {
						parent = a.workspacePath(filepath.Dir(path))
					}
					ui.Text(c, parent).FontSize(11).TextColor(colors(c).Dim).SingleLine().Ellipsis("…").TruncateMiddle().MinWidth(0).Shrink(2)
				}
				if d.loadingVisible {
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
		repositoryLabel := a.workspaceTitle()
		if a.git.root != "" {
			repositoryLabel = a.gitRepositoryLabel(a.git.root)
		}
		ui.Text(c, repositoryLabel).FontSize(12).TextColor(colors(c).Muted).SingleLine()
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
		if v.commit == nil || len(v.commit.files) > 0 {
			label, tip := diffWhitespaceText("msgWhitespaceChanges"), diffWhitespaceText("msgHideWhitespaceChanges")
			if a.cfg.HideWhitespaceChanges {
				tip = diffWhitespaceText("msgShowWhitespaceChanges")
			}
			if glyphToggle(c.Key("diff-whitespace"), icDiffWhitespace, label, tip, a.cfg.HideWhitespaceChanges, false).Clicked() {
				a.toggleDiffWhitespace()
			}
		}
		if v.state != nil {
			unchangedTip := locales.MsgShowUnchangedDiffLines()
			if a.cfg.DiffShowUnchanged {
				unchangedTip = locales.MsgCollapseUnchangedDiffLines()
			}
			if glyphToggle(c.Key("diff-unchanged"), icDiffUnchanged, locales.MsgShowUnchangedDiffLines(), unchangedTip, a.cfg.DiffShowUnchanged, false).Clicked() {
				a.toggleDiffUnchanged()
			}
			hunks := v.state.Hunks()
			if v.partial != nil && len(hunks) > 0 {
				i, _ := v.state.CurrentHunk()
				ui.Text(c, fmt.Sprintf("%d / %d", max(0, i)+1, len(hunks))).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			}
			if glyphButton(c, icChangeUp, "Previous Change", "Previous Change", false, len(hunks) == 0).Clicked() {
				a.setFocusedGroup(g)
				a.goToChange(-1)
			}
			if glyphButton(c, icChangeDown, "Next Change", "Next Change", false, len(hunks) == 0).Clicked() {
				a.setFocusedGroup(g)
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

func jumpBarTextWidth(c *ui.Context, text string) float32 {
	w, _ := c.MeasureText(0, ui.Span{Text: text, Size: 12})
	return w
}

func jumpBarFileCrumbWidth(c *ui.Context, text string, modified bool) float32 {
	w := jumpBarTextWidth(c, text) + 23
	if modified {
		w += 13
	}
	return w
}

func jumpBarFileCrumbMinWidth(modified bool) float32 {
	w := float32(23 + 32)
	if modified {
		w += 13
	}
	return w
}

func jumpBarSymbolCrumbWidth(c *ui.Context, text string) float32 {
	return jumpBarTextWidth(c, text) + 23
}

type jumpBarInlinePlan struct {
	rootVisible    bool
	fileVisible    bool
	firstFile      int
	symbolsVisible bool
	overflow       bool
	pathWidth      float32
	currentWidth   float32
	currentCapped  bool
}

func jumpBarInlineLayout(c *ui.Context, width float32, root string, parts []string, modified bool, filesMenu bool, symbols []string, current string, currentIcon, showSymbols bool) jumpBarInlinePlan {
	const separator = float32(13)
	rootWidth := float32(15) + min(jumpBarTextWidth(c, root), 160)
	partWidths := make([]float32, len(parts))
	partsWidth := float32(0)
	for i, part := range parts {
		partWidths[i] = jumpBarFileCrumbWidth(c, part, modified && i == len(parts)-1)
		partsWidth += partWidths[i]
		if i > 0 {
			partsWidth += separator
		}
	}
	symbolWidths := make([]float32, len(symbols))
	symbolsWidth := float32(0)
	for i, symbol := range symbols {
		symbolWidths[i] = jumpBarSymbolCrumbWidth(c, symbol)
		symbolsWidth += symbolWidths[i]
		if i > 0 {
			symbolsWidth += separator
		}
	}
	if len(parts) > 0 && len(symbols) > 0 {
		symbolsWidth += separator
	}
	currentNaturalWidth := float32(0)
	if showSymbols {
		currentNaturalWidth = jumpBarTextWidth(c, current) + 21
		if currentIcon {
			currentNaturalWidth += 13
		}
	}
	fullPathWidth := rootWidth + partsWidth + symbolsWidth
	if len(parts) > 0 {
		fullPathWidth += separator
	}
	plan := jumpBarInlinePlan{fileVisible: len(parts) > 0, symbolsVisible: true, currentWidth: currentNaturalWidth}
	if fullPathWidth+currentNaturalWidth <= width {
		plan.rootVisible = true
		plan.pathWidth = fullPathWidth
		return plan
	}
	withoutRootWidth := partsWidth + symbolsWidth
	if withoutRootWidth+currentNaturalWidth <= width {
		plan.pathWidth = withoutRootWidth
		return plan
	}
	canOverflow := filesMenu && len(parts) > 1 || len(symbols) > 0
	if !canOverflow {
		plan.rootVisible = false
		plan.currentWidth = min(currentNaturalWidth, width)
		plan.currentCapped = plan.currentWidth < currentNaturalWidth
		plan.pathWidth = min(withoutRootWidth, max(float32(0), width-plan.currentWidth))
		if len(parts) > 0 && plan.pathWidth < jumpBarFileCrumbMinWidth(modified) {
			plan.fileVisible = false
			plan.pathWidth = 0
			if filesMenu {
				if showSymbols && plan.currentWidth+26 > width {
					plan.currentWidth = max(float32(0), width-26)
					plan.currentCapped = true
				}
				plan.overflow = true
			}
		}
		return plan
	}
	if showSymbols && currentNaturalWidth+26 > width {
		plan.currentWidth = max(float32(0), width-26)
		plan.currentCapped = true
	}
	pathBudget := max(float32(0), width-plan.currentWidth-26)
	plan.rootVisible = false
	plan.symbolsVisible = false
	if len(parts) == 0 {
		plan.overflow = len(symbols) > 0
		return plan
	}
	if pathBudget < jumpBarFileCrumbMinWidth(modified) {
		plan.fileVisible = false
		plan.overflow = filesMenu || len(symbols) > 0
		return plan
	}
	plan.firstFile = len(parts) - 1
	remaining := pathBudget
	lastWidth := partWidths[plan.firstFile]
	if rootWidth+separator+lastWidth <= remaining {
		plan.rootVisible = true
		remaining -= rootWidth + separator + lastWidth
	} else {
		remaining -= min(lastWidth, remaining)
	}
	if filesMenu {
		for i := len(parts) - 2; i >= 0; i-- {
			cost := separator + partWidths[i]
			if plan.rootVisible {
				cost += separator
			}
			if cost > remaining {
				break
			}
			plan.firstFile = i
			remaining -= cost
		}
	}
	hiddenFiles := filesMenu && (!plan.fileVisible || plan.firstFile > 0)
	hiddenSymbols := len(symbols) > 0
	plan.overflow = hiddenFiles || hiddenSymbols
	if plan.rootVisible {
		plan.pathWidth += rootWidth
	}
	for i := plan.firstFile; i < len(parts); i++ {
		if plan.rootVisible || i > plan.firstFile {
			plan.pathWidth += separator
		}
		plan.pathWidth += partWidths[i]
	}
	plan.pathWidth = min(plan.pathWidth, pathBudget)
	return plan
}

// jumpBar is Xcode's jump bar: back and forward, then the path of the
// document, whose folders reveal themselves in the navigator.
func (a *App) jumpBar(c *ui.Context, g *group) {
	tab := g.active
	p := tabPath(tab)
	if p == "" {
		g.jumpFiles, g.jumpSymbols, g.jumpFilesDirect = false, false, false
		return
	}
	if g == a.focus {
		a.refreshOutline()
	}
	showSymbols := a.jumpSymbolsShown(g, p)
	rootPath := a.workspaceRootForPath(p)
	outside := rootPath == ""
	if d := a.docs[p]; d != nil && d.untitled {
		outside = false
	}
	parts := strings.Split(workspace.RelPath(rootPath, p), "/")
	root := filepath.Base(rootPath)
	if len(a.workspaceFolders) > 1 && rootPath != "" {
		root = a.workspaceFolderLabel(rootPath)
	}
	if d := a.docs[p]; d != nil && d.untitled {
		parts = []string{d.name}
	} else if outside {
		parts = []string{filepath.Base(p)}
	}
	filesMenu := !outside && !(a.docs[p] != nil && a.docs[p].untitled)
	if outside {
		root = tildePath(filepath.Dir(p))
	} else if rootPath == "" {
		root = a.workspaceTitle()
	}
	showClose := len(g.order) == 1 && len(a.groups()) == 1
	showBytes := !isMarkdownTab(tab) && a.canViewBytes(p)
	showMarkdown := isMarkdown(p)
	symbols := []string(nil)
	current := outlineText("jumpNoSelection")
	currentIcon := false
	if showSymbols {
		path := symbolPath(a.outline.symbols, a.currentSymbol())
		if len(path) > 0 {
			current = a.outline.symbols[path[len(path)-1]].Name
			currentIcon = true
			for _, i := range path[:len(path)-1] {
				symbols = append(symbols, a.outline.symbols[i].Name)
			}
		}
	}
	paneWidth := g.focusHandle.Bounds(c).W
	if paneWidth <= 0 {
		paneWidth, _ = c.Size()
		paneWidth = min(paneWidth, float32(640))
	}
	rightControls := 0
	if showClose {
		rightControls++
	}
	if showBytes {
		rightControls++
	}
	if showMarkdown {
		rightControls++
	}
	pendingLoading := g.pendingPreview != nil && g.pendingPreview.doc.loadingVisible
	gapCount := rightControls + 2
	if showSymbols {
		gapCount++
	}
	pendingLoadingWidth := float32(0)
	if pendingLoading {
		gapCount++
		pendingLoadingWidth = 140
	}
	bodyWidth := paneWidth - 12 - 22 - float32(rightControls*19) - float32(gapCount*2) - pendingLoadingWidth
	modified := false
	if d := a.docs[p]; d != nil && d.buf != nil {
		modified = d.buf.Modified()
	}
	layout := jumpBarInlineLayout(c, max(float32(0), bodyWidth), root, parts, modified, filesMenu, symbols, current, currentIcon, showSymbols)
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
				a.setFocusedGroup(g)
				a.findUsagesAt(true, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
			if menu.Item(locales.MsgReferences()).Chosen() {
				a.setFocusedGroup(g)
				a.findUsagesAt(false, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
			if menu.Item(locales.CallHierarchy()).Chosen() {
				a.setFocusedGroup(g)
				a.showCallHierarchyAt(g)
			}
			if a.navigationEnabled(g.active, "typeHierarchy") && menu.Item(navigationText("typeHierarchy", "")).Chosen() {
				a.setFocusedGroup(g)
				a.showTypeHierarchyAt(g)
			}
		}, ui.PopoverMenuButtonOptions{Open: &g.relatedOpen, Panel: popoverMenuPanel})
		if !g.relatedOpen {
			g.counterpartMenu = nil
		}
		if !filesMenu {
			g.jumpFiles = false
		}
		if !g.jumpFiles {
			g.jumpFilesDirect = false
		}
		ui.Row(c).Gap(2).Padding(0).Grow(1).Shrink(1).MinWidth(0).Children(func() {
			ui.Row(c).Gap(2).Padding(0).Shrink(1).MinWidth(0).MaxWidth(layout.pathWidth).Children(func() {
				if layout.rootVisible {
					ui.Row(c).Gap(2).AlignItems(ui.Center).Shrink(0).Children(func() {
						folderGlyph(c, false, 13)
						ui.Text(c, root).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").TruncateMiddle().MaxWidth(160).MinWidth(0).Shrink(1)
					})
				}
				if layout.fileVisible {
					for i := layout.firstFile; i < len(parts); i++ {
						part := parts[i]
						if layout.rootVisible || i > layout.firstFile {
							symbolIcon(c, icChevron, 9, colors(c).Dim)
						}
						last := i == len(parts)-1
						children := func() {
							if last {
								fileIcon(c, part, 13)
							} else {
								folderGlyph(c, false, 13)
							}
							t := ui.Text(c, part).FontSize(12).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
							if !last {
								t.TextColor(colors(c).Muted)
							} else {
								if g.preview == p {
									t.Italic()
								}
								if modified {
									ui.Text(c, "●").FontSize(9).TextColor(c.Theme().Accent)
								}
							}
						}
						if !filesMenu {
							ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Padding(1, 3).Shrink(1).MinWidth(0).Children(children)
							continue
						}
						item := filepath.Join(append([]string{rootPath}, parts[:i+1]...)...)
						var open *bool
						if last {
							open = &g.jumpFiles
						}
						shrink := float32(0)
						if last {
							shrink = 1
						}
						crumb := jumpCrumb(c, i, part, open, shrink, 0, children, func(m *ui.Menu) { a.jumpFilesMenu(m, g, filepath.Dir(item), p, false) })
						if last && crumb.DoubleClicked() && g.preview == p {
							g.preview = ""
						}
					}
				}
				if showSymbols && layout.symbolsVisible {
					a.jumpSymbolAncestors(c, g)
				}
			})
			middle := ui.Box(c.Key("jumpbar-overflow-area")).Grow(1).Shrink(1).MinWidth(0).AlignItems(ui.Center).Justify(ui.Center)
			if layout.overflow {
				var open *bool
				if filesMenu && len(parts) > 0 && !layout.fileVisible {
					open = &g.jumpFiles
				}
				middle.MinWidth(26).Label("Jump Bar overflow area").Children(func() {
					jumpBarOverflowButton(c, open, func(m *ui.Menu) {
						if g.jumpFilesDirect && filesMenu && len(parts) > 0 {
							a.jumpFilesMenu(m, g, filepath.Dir(p), p, false)
							return
						}
						a.jumpBarOverflowMenu(m, g, p, parts, filesMenu, showSymbols)
					})
				})
			}
			if pendingLoading {
				loading := locales.MsgLoading2() + g.pendingPreview.doc.name + "…"
				ui.Row(c.Key("pending-preview-loading")).Gap(4).Shrink(1).MinWidth(0).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c).FontSize(10)
					ui.Text(c, loading).FontSize(11).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").MaxWidth(120).MinWidth(0).Shrink(1)
				})
			}
			if showSymbols {
				a.jumpCurrentSymbol(c, g, layout.currentWidth, layout.currentCapped)
			} else {
				g.jumpSymbols = false
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
			if showBytes {
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
			if showMarkdown {
				label, tip := locales.MsgMarkdownPreview(), commandLabel("markdown.preview")
				if g.markdownPreview[p] || isMarkdownTab(tab) {
					label, tip = locales.MsgMarkdownSource(), locales.MsgShowMarkdownSource()
				}
				if glyphToggle(c, icMarkdown, label, tip+" ("+keyLabel("Cmd+Shift+", "V")+")", g.markdownPreview[p] || isMarkdownTab(tab), false).Clicked() {
					a.setFocusedGroup(g)
					if isMarkdownTab(tab) || g.markdownPreview[p] {
						delete(g.markdownPreview, p)
						a.openIn(g, p, goTo{})
					} else {
						a.openMarkdownTab(g)
					}
				}
			}
		})
	})
}
