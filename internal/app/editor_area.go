package app

import (
	"fmt"
	"math"
	"path/filepath"
	"see/locales"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/git"
	"see/internal/workspace"
)

// tabDrag is a tab, or a file of the tree, being dragged: onto a tab bar
// it goes among the tabs, onto an editor's edge it splits the editor.
type tabDrag struct {
	from   *group // nil for a file of the tree
	path   string
	change *git.Change
	staged bool
	root   string
}

func (a *App) editorArea(c *ui.Context) {
	if target := a.focusGroupTarget; target != nil && (a.layout.find(target) == nil || target.focusHandle.FocusWithin(c)) {
		a.focusGroupTarget = nil
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(1).FillHeight().Children(func() {
			a.editorsAndPanel(c, func() {
				ui.Column(c.Key("regex-tool-editor-workspace")).Fill().Bind(&a.regexToolWindow.workspaceHandle).Children(func() {
					if g := a.maximized; g != nil && a.layout.find(g) != nil && len(a.groups()) > 1 {
						a.groupView(c, g)
					} else {
						a.layoutView(c, a.layout)
					}
					if p := a.regexPlayground; a.regexToolSourceVisible(p) {
						a.regexToolWindowView(c, p, a.regexToolWindow.workspaceHandle.Bounds(c), p.group.focusHandle.Bounds(c))
					}
				})
			})
		})
		if a.inspectorOpen() {
			ui.Column(c).Width(a.inspectorW()).FillHeight().Shrink(0).Children(func() {
				a.inspectorDivider(c)
				a.inspectorView(c)
			})
		}
	})
}

func (a *App) layoutView(c *ui.Context, n *layout) {
	if n == nil {
		return
	}
	if n.g != nil {
		a.groupView(c, n.g)
		return
	}
	// A drop in the first child can collapse n before the second is built.
	left, right := n.a, n.b
	first, second := func() { a.layoutView(c, left) }, func() { a.layoutView(c, right) }
	if n.vertical {
		if ui.SplitVerticalPercent(c.Key(n), &n.pct, first, second).Grow(1).FillWidth().Changed() {
			a.saveConfigSoon()
		}
	} else {
		if ui.SplitPercent(c.Key(n), &n.pct, first, second).Grow(1).FillHeight().Changed() {
			a.saveConfigSoon()
		}
	}
}

func (a *App) groupView(c *ui.Context, g *group) {
	focused := g == a.focus && len(a.groups()) > 1
	col := ui.Column(c.Key(g)).Grow(1).Fill().FocusRing(false).Bind(&g.focusHandle).Label(fmt.Sprintf("%s %d", locales.Catalog.Format(i18n.Locale(), "editorGroup", nil), g.id))
	if !a.groupHasFocusTarget(g) {
		col.Focusable()
		if a.focusGroupTarget == g {
			col.Focus()
		}
	}
	if focused {
		accent := c.Theme().Accent
		col.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, accent, 0)
		})
	}
	col.Children(func() {
		if len(g.order) > 1 || len(a.groups()) > 1 {
			a.tabBar(c, g)
		}
		if len(g.order) > 0 {
			if isDiffTab(g.active) {
				a.diffJumpBar(c, g)
			} else {
				a.jumpBar(c, g)
			}
		}
		if g == a.focus {
			if a.trustAsk && g.eds[g.active] != nil {
				a.trustBanner(c)
			}
		}
		a.dropArea(c, g, func() { a.groupContent(c, g) })
		if v := g.diffs[g.active]; v != nil && v.branch != nil {
			a.branchDiffStatusBar(c, g, v.branch)
		} else if v != nil && v.search != nil {
			a.searchEditStatusBar(c, g, v.search)
		}
		if st := g.eds[g.active]; st != nil {
			d := a.docs[g.active]
			if d != nil {
				a.detectDocumentLanguage(d)
			}
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			column := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			ui.Row(c).FillWidth().Height(24).Padding(0, 10).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(colors(c).Separator).Background(colors(c).Editor).Children(func() {
				a.blameStatus(c, a.docs[g.active], st)
				if d := a.docs[g.active]; d != nil {
					if _, found := g.regexAtCaret(d, st); found {
						button := ui.Button(c.Key("regex-context-action"), locales.CommandTestRegex()).FontSize(10).Height(20).Padding(1, 5).Shrink(0).Label(locales.CommandTestRegex())
						button.OnClick(func() { a.openRegexPlaygroundFor(g, d, st) })
					}
				}
				ui.Box(c).Grow(1)
				if d != nil && d.buf != nil {
					a.documentLanguageMenu(c, d)
				}
				ui.Text(c, fmt.Sprintf(locales.MsgLineDColumnD(), pos.Line+1, column)).FontSize(11).TextColor(colors(c).Muted).SingleLine()
			})
		}
	})
}

func (a *App) documentLanguageMenu(c *ui.Context, d *doc) {
	current := documentLanguage(d)
	name := locales.InspectorTypeText()
	if current != nil {
		name = current.Name
	}
	label := locales.Language() + ": " + name
	button := ui.ButtonBase(c.Key("document-language")).Label(label).Height(20).Padding(0, 5).Shrink(0).KeepFocus().Disabled(d.plain)
	button.Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(colors(c).Muted).SingleLine()
	})
	dark := c.Theme().Dark
	button.PopoverMenuButton(func(m *ui.Menu) {
		if m.Item(locales.InspectorTypeText()).Icon(fileMenuIcon("file-text-duo", dark)).Radio(current == nil).Chosen() {
			a.selectDocumentLanguage(d, nil)
		}
		for _, lang := range documentLanguages() {
			if m.Item(lang.Name).Icon(fileMenuIcon(languageIconName(lang), dark)).Radio(current == lang).Chosen() {
				a.selectDocumentLanguage(d, lang)
			}
		}
	}, ui.PopoverMenuButtonOptions{
		Panel: func(panel ui.Element) { popoverMenuPanel(panel); panel.MaxHeightPercent(70) },
	})
}

// dropArea takes tabs and files dropped onto an editor: on its edges they
// open in a new editor there, in its middle in this one.
func (a *App) dropArea(c *ui.Context, g *group, content func()) {
	area := ui.Column(c).Grow(1).FillWidth()
	_, over := ui.DragOver[*tabDrag](area)
	fileOver := area.FileDragOver()
	zone := sideNone
	if x, y, in := area.PointerPosition(); in || over || fileOver {
		b := area.LayoutBounds()
		zone = zoneAt(x, y, b.W, b.H)
	}
	if v, ok := ui.Drop[*tabDrag](area); ok {
		a.dropTab(v, g, zone, len(g.order))
	}
	if paths := area.DroppedFiles(); paths != nil {
		a.openDropped(g, zone, paths)
	}
	area.Children(func() {
		content()
		if over || fileOver {
			b := area.LayoutBounds()
			hl := ui.Box(c).Absolute().Background(colors(c).DropZone).Radius(6).Border(2, c.Theme().Accent).PassThrough()
			switch zone {
			case sideLeft:
				hl.Left(4).Top(4).Bottom(4).Width(b.W/2 - 8)
			case sideRight:
				hl.Right(4).Top(4).Bottom(4).Width(b.W/2 - 8)
			case sideTop:
				hl.Left(4).Right(4).Top(4).Height(b.H/2 - 8)
			case sideBottom:
				hl.Left(4).Right(4).Bottom(4).Height(b.H/2 - 8)
			default:
				hl.Left(4).Right(4).Top(4).Bottom(4)
			}
		}
	})
}

// zoneAt says where over an editor of w×h a drop at x, y goes: within a
// quarter of an edge, beside it; elsewhere into the editor.
func zoneAt(x, y, w, h float32) side {
	if w <= 0 || h <= 0 {
		return sideNone
	}
	fx, fy := x/w, y/h
	edge := min(fx, 1-fx, fy, 1-fy)
	if edge > 0.25 {
		return sideNone
	}
	switch edge {
	case fx:
		return sideLeft
	case 1 - fx:
		return sideRight
	case fy:
		return sideTop
	}
	return sideBottom
}

func (a *App) dropTab(v *tabDrag, g *group, s side, at int) {
	a.cancelNav()
	if v.change != nil {
		root := v.root
		if root == "" {
			root = a.git.root
		}
		if workingCopy := a.git.workingCopies[root]; a.cfg.GitAutoDiscover && workingCopy != nil {
			a.showGitWorkingCopyDiff(root, *v.change, v.staged, g, s, at, workingCopy)
		} else {
			a.showGitDiffInRepository(root, *v.change, v.staged, g, s, at, nil)
		}
		return
	}
	if v.from == nil || !v.from.has(v.path) {
		if a.isTreeDir(v.path) {
			return
		}
		// A file from the tree.
		target := g
		if s != sideNone {
			target = a.splitBeside(g, s)
		}
		a.openIn(target, v.path, goTo{})
		return
	}
	a.moveTab(v.from, v.path, g, s, at)
}

func (a *App) groupContent(c *ui.Context, g *group) {
	if v := g.diffs[g.active]; v != nil && v.commit != nil {
		a.commitContent(c, g, v.commit)
		return
	} else if v != nil && v.search != nil {
		a.searchEditContent(c, g, v.search)
		return
	} else if v != nil && v.branch != nil {
		a.branchDiffContent(c, g, v.branch)
		return
	} else if v != nil && v.log != nil {
		a.gitLogContent(c, g, v.log)
		return
	} else if v != nil && v.revision != nil {
		a.revisionContent(c, g, v)
	} else if v != nil && v.localHistory != nil {
		a.localHistoryContent(c, g, v)
		return
	} else if v != nil && v.conflict != nil {
		a.gitConflictContent(c, g, v)
		return
	} else if v != nil && v.state == nil {
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.MsgReading()+v.name+"…").TextColor(colors(c).Muted)
		})
		return
	} else if v != nil && v.change && v.patch.OldName == "" && v.patch.NewName == "" {
		ui.Row(c).Grow(1).FillWidth().Center().Children(func() {
			ui.Text(c, locales.MsgNoChanges()).TextColor(colors(c).Muted)
		})
		return
	} else if v != nil {
		if a.diffChangesHidden(v.state) {
			a.hiddenWhitespaceDiff(c)
			return
		}
		if v.rev != "" {
			// A commit's file: the diff over a bar with its permalink,
			// as the revision comparison's.
			ui.Column(c).Fill().Children(func() {
				view := editor.DiffView(c, v.state).Grow(1).FillWidth()
				if a.focusGroupTarget == g {
					view.Focus()
				}
				if a.focusGroupTarget == nil && view.FocusWithin() {
					a.focus = g
				}
				a.commitFileBar(c, v)
			})
			return
		}
		view := editor.DiffView(c, v.state).Grow(1).FillWidth()
		if a.focusGroupTarget == g {
			view.Focus()
		}
		if a.focusGroupTarget == nil && view.FocusWithin() {
			a.focus = g
		}
		return
	}
	d := a.docs[tabPath(g.active)]
	if d == nil {
		ui.Column(c).Grow(1).FillWidth().Center().Gap(6).Children(func() {
			ui.Text(c, locales.MsgNoEditor()).FontSize(20).TextColor(colors(c).Dim)
			ui.Text(c, locales.MsgChooseAFileInTheNavigatorOrPress()+keyLabel("Cmd+Shift+", "O")+locales.MsgToOpenOneQuickly()).FontSize(12).TextColor(colors(c).Dim)
		})
		return
	}
	a.document(c, g, d)
}

func (a *App) document(c *ui.Context, g *group, d *doc) {
	if cs := a.completionFor(g); cs.path != "" && cs.path != d.path {
		a.cancelCompletions(g, g.eds[cs.path])
	}
	st := g.eds[d.path]
	switch {
	case d.err != nil:
		a.message(c, locales.MsgCouldNotOpen()+d.name, describeErr(d.err), locales.Retry(), func() { a.reloadDoc(d) })
	case d.loading && st == nil:
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, locales.MsgLoading2()+d.name+"…").TextColor(colors(c).Muted)
		})
	case !g.hexPreview[d.path] && d.video:
		a.videoPreview(c, g, d)
	case g.hexPreview[d.path] || d.kind == workspace.Binary && d.image == nil:
		a.hexView(c, g, d)
	case d.image != nil:
		a.imagePreview(c, g, d)
	case d.kind == workspace.TooLarge:
		a.message(c, locales.TooLargeTitle(d.name),
			locales.TooLargeDetail(size(d.size), size(workspace.MaxSize)), "", nil)
	case d.kind == workspace.BadEncoding:
		a.message(c, locales.BadEncodingTitle(d.name),
			locales.BadEncodingDetail(), locales.ShowReplacement(), func() {
				d.lossy = true
				a.reloadDoc(d)
			})
	case st != nil:
		if v := d.buf.Version(); v != d.lspVersion {
			a.invalidateHover()
			d.lspVersion = v
			a.captureEditedLocalHistory(d)
			if a.root == "" {
				// Make a saved file's recovery record reachable from the shared
				// standalone session if the process crashes.
				a.saveConfigSoon()
			}
			if d.untitled {
				a.cacheDraft(d)
			} else if !d.lossy {
				a.cacheRecovery(d)
			}
			if m := a.serverFor(d.path); m != nil {
				m.Changed(d.uri, d.buf.Snapshot())
			}
			if d.buf.Modified() && g.preview == d.path {
				g.preview = ""
			}
		}
		if d.conflict {
			a.banner(c, colors(c).WarnBg, locales.FileChangedBanner(), []action{{locales.Overwrite(), func() { a.saveDoc(d, true) }}})
		}
		if d.gone {
			a.banner(c, colors(c).WarnBg, locales.FileDeletedBanner(), []action{
				{locales.Reload(), func() { a.reloadDoc(d) }},
				{locales.Close(), func() { a.closeTab(g, g.active) }},
			})
		}
		if status := a.scopeEditStatus(st); status != "" {
			ui.Text(c, status).FontSize(11).TextColor(colors(c).Muted).Padding(6, 12).FillWidth().Background(colors(c).TabBar)
		}
		if d.plain || d.lossy {
			msg := "Large file: syntax highlighting is off."
			if d.lossy {
				msg = "Bytes that are not UTF-8 show as �. This file is read-only to protect its encoding."
			}
			ui.Text(c, msg).FontSize(11).TextColor(colors(c).Muted).Padding(3, 12).FillWidth().Background(colors(c).TabBar)
		}
		if isMarkdownTab(g.active) || isMarkdown(d.path) && g.markdownPreview[d.path] {
			a.markdownView(c, g, d)
			return
		}
		if p := g.colorPreview; p != nil && (g != a.focus || g.active != d.path || p.path != d.path || d.buf == nil || p.version != d.buf.Version()) {
			g.dismissColorPreview()
		}
		// Reserve the Git gutter even before a base is available. Comparing
		// the buffer with itself gives an empty diff without moving line numbers.
		base := st.DiffBase
		if base == nil {
			st.DiffBase = st.Buffer
		}
		a.syncBlame(d)
		a.syncInlayHints(d, st)
		a.syncDocumentLinks(d, st)
		a.syncFoldingRanges(d, st)
		textBackgrounds := st.TextBackgrounds
		if s := &a.symbolHighlights; s.target.editor == st && len(s.backgrounds) > 0 {
			st.TextBackgrounds = append(append([]editor.TextBackground(nil), s.backgrounds...), textBackgrounds...)
		}
		a.linkBackgrounds(st, d)
		st.SetInlays(a.editorInlays(d, st))
		if g.colorPreview != nil {
			g.colorPreview.seen = false
		}
		st.SetReplacements(mergeReaderReplacements(a.colorReplacements(g, d, st), a.readerReplacements(c, d, st)))
		backgrounds := st.LineBackgrounds
		st.LineBackgrounds = a.diagnosticLineBackgrounds(d, st)
		st.LineBackgrounds = append(st.LineBackgrounds, bookmarkLineBackgrounds(a.bookmarkedLines(d.path), st.Theme)...)
		gutterMarks := st.GutterMarks
		st.GutterMarks = a.bookmarkGutterMarks(d.path)
		st.OnGutterClick = func(line int) { a.bookmarks.Toggle(d.path, line); a.saveConfigSoon() }
		st.WrapGuides = st.WrapGuides[:0]
		if a.cfg.EditorShowPageGuide {
			st.WrapGuides = append(st.WrapGuides, a.cfg.PageGuideColumn())
		}
		var ed ui.Element
		minimap := a.showsMinimap(g, d)
		ui.Row(c).Grow(1).FillWidth().Children(func() {
			cs := a.completionFor(g)
			if g != a.focus && (cs.visible || cs.cancel != nil || cs.resolvePending) {
				a.cancelCompletions(g, st)
			}
			wasVisible := cs.visible
			cs.visible = false
			st.RenderCompletions = func(c *ui.Context, panel ui.Element, menu *editor.CompletionMenu) {
				cs.visible = g == a.focus
				if cs.visible {
					cs.displayedSeq = cs.resultSeq
				}
				a.renderCompletions(c, panel, menu, g, st)
			}
			if !minimap {
				st.RenderFindBar = nil
			}
			ed = editor.View(c.Key(d.path), st).Grow(1).FillHeight()
			refreshCompletionsAfterDeletion(st, cs)
			if wasVisible && !cs.visible && cs.seq == cs.displayedSeq {
				a.cancelCompletions(g, st)
			}
			ed.Children(func() { a.stickyScopeHeaders(c, g, d, st) })
			if minimap {
				a.minimap(c, g, d, st)
			}
		})
		if p := g.colorPreview; p != nil && p.path == d.path && p.open && !p.seen {
			g.dismissColorPreview()
		}
		st.LineBackgrounds = backgrounds
		st.TextBackgrounds = textBackgrounds
		st.GutterMarks = gutterMarks
		st.OnGutterClick = nil
		if row := ed.LayoutBounds().W; row > 0 {
			m := g.minimapOf(d.path)
			m.room = row
			if minimap {
				m.room += minimapWidth
			}
		}
		st.DiffBase = base
		x, y, over := ed.PointerPosition()
		if over {
			wx, wy := ed.LocalToWindow(x, y)
			g.pointerAnchor = ui.Point{X: wx, Y: wy}
			g.pointerInEditor = true
		}
		bounds := ed.LayoutBounds()
		pos := st.CursorPos()
		fontSize := st.Font.Size
		if fontSize <= 0 {
			fontSize = 13
		}
		lineHeight := st.LineHeight
		if lineHeight <= 0 {
			lineHeight = 1.5
		}
		charWidth, _ := c.MeasureText(0, ui.Span{Text: "0", Font: st.Font.Family, Size: fontSize})
		gutter := float32(8)
		if st.LineNumbers != editor.LineNumbersOff {
			gutter += float32(max(3, len(fmt.Sprint(d.buf.LineCount())))+2) * charWidth
		}
		if !st.NoFolding {
			gutter += fontSize * lineHeight * 0.8
		}
		gutter += 6
		g.changeBars = g.changeBars[:0]
		if d.gitChanges != nil {
			// The editor paints its change bars immediately before the fold
			// gutter; a press on one opens its change.
			barGutter := float32(10) // 6 for the change bar and 4 for padding
			if st.LineNumbers != editor.LineNumbersOff {
				barGutter += (float32(max(3, len(fmt.Sprint(d.buf.LineCount())))) + 1.5) * charWidth
			}
			if !st.NoFolding {
				barGutter += fontSize * lineHeight * 0.8
			}
			barX := bounds.X + float32(math.Ceil(float64(barGutter))) - 7
			if !st.NoFolding {
				barX -= fontSize * lineHeight * 0.8
			}
			first, last := st.VisibleLines()
			for _, h := range d.gitChanges.Hunks() {
				line := min(h.NewStart, d.buf.LineCount()-1)
				end := max(h.NewEnd, line+1)
				if end <= first || line > last {
					continue
				}
				top := max(bounds.Y, bounds.Y+float32(st.LineTop(line)-st.ScrollY()))
				bottom := min(bounds.Y+bounds.H, bounds.Y+float32(st.LineTop(min(end, d.buf.LineCount()))-st.ScrollY()))
				if bottom <= top {
					continue
				}
				bar := changeBar{r: ui.Rect{X: barX - 3, Y: top, W: 9, H: bottom - top}, line: line}
				g.changeBars = append(g.changeBars, bar)
				hovered := over && bar.r.Contains(g.pointerAnchor.X, g.pointerAnchor.Y)
				if hovered {
					ed.Cursor(ui.CursorPointer)
				}
				ui.Overlay(c, func() {
					key := struct {
						group *group
						line  int
					}{g, h.NewStart}
					// The bar's face, for the eye and assistive technology:
					// the pointer passes through to the editor, which keeps
					// the wheel and takes the press (changeBarInput).
					b := ui.ButtonBase(c.Key(key)).Absolute().Left(bar.r.X).Top(bar.r.Y).Width(bar.r.W).Height(bar.r.H).Radius(3).
						PassThrough().Label(fmt.Sprintf(locales.MsgShowGitChangeAtLineD(), line+1))
					if hovered {
						b.Background(ui.RGBA(128, 128, 128, 0.25))
					}
					b.OnClick(func() { a.showGitPeek(g, d, st, line) })
				})
			}
		}
		origin := ui.Point{X: bounds.X, Y: bounds.Y}
		st.OnInput = func(ev ui.InputEvent) bool {
			if g.colorPreview != nil && (ev.Kind == ui.InputScroll || ev.Kind == ui.InputZoom) {
				g.dismissColorPreview()
			}
			if a.changeBarInput(g, d, st, origin, ev) {
				return true
			}
			if cs := a.completionFor(g); completionInputEdits(ev) {
				hasSourceResults := cs.incomplete || (cs.visible && len(cs.originals) > 0)
				backspace := completionInputBackspace(ev)
				if hasSourceResults || (backspace && cs.cancel != nil) {
					var refresh *completionEditRefresh
					if backspace && cs.resultSeq != 0 && st.Buffer != nil {
						refresh = &completionEditRefresh{start: cs.resultStart, version: st.Buffer.Version()}
					}
					a.cancelCompletions(g, st)
					cs.refreshAfterEdit = refresh
				}
			}
			if cs := a.completionFor(g); cs.visible && completionNavigationInput(ev) {
				a.cancelCompletionResolve(cs)
				cs.hasResolveSelected = false
			}
			if cs := a.completionFor(g); cs.visible && completionAcceptInput(ev) {
				a.cancelCompletionResolve(cs)
				cs.hasResolveSelected = false
			}
			if ev.Kind == ui.InputKeyDown && ev.Key == ui.KeyEscape {
				cs := a.completionFor(g)
				if cs.visible || cs.cancel != nil {
					a.cancelCompletions(g, st)
				}
			}
			if a.signatureInput(g, st, ev) {
				return true
			}
			return a.quickHelpEditorInput(g, d, st, ev)
		}
		if peek := a.git.peek; peek != nil && peek.doc == d && peek.group == g {
			diff, visible := a.gitPeekDiff(peek, st)
			height := float32(36)
			if visible {
				height = 264
			}
			act := &peekAction{label: commandLabel("change.discard"), destructive: true, do: func() { a.discardChangeAt(d, st, peek.line) }}
			a.editorPeekAction(c, bounds, st, peek.line, d.name+" — Git changes", "Close Change Preview", height, visible, act, func() { a.git.peek = nil }, func() {
				if !visible {
					a.hiddenWhitespaceDiff(c)
					return
				}
				ui.Scroll(c).MinHeight(0).FillWidth().TrackScroll(&peek.scroll).Label(locales.MsgGitChangePreview()).Children(func() {
					editor.DiffView(c, diff).FillWidth()
				})
			})
		}
		if p := a.peek; p != nil && p.group == g && p.doc == d {
			a.editorPeek(c, bounds, st, p.from.Line, a.peekTitle(p.t), locales.PeekDefinitionClose(), peekContentHeight(st, p), func() { a.peek = nil }, func() {
				a.peekView(c, p)
			})
		}
		if ch := a.choices; ch != nil && ch.g == g && ch.from.Path == d.path {
			closeLabel := locales.MsgCloseReferencesPreview()
			if ch.title == "definitions" {
				closeLabel = locales.MsgCloseDefinitionsPreview()
			}
			if ch.navigation != "" {
				closeLabel = navigationText(ch.navigation, "Close")
			}
			var pin *peekAction
			if ch.title == "references" {
				pin = &peekAction{icon: icPin, label: commandLabel("references.pin"), tip: commandLabel("references.pin"), do: func() { a.pinReferences(ch) }}
			}
			a.editorPeekAction(c, bounds, st, ch.from.Line, fmt.Sprintf("%d %s", len(ch.locs), referenceTitle(ch.title)), closeLabel, 264, false,
				pin, func() { a.choices = nil }, func() { a.relatedView(c, ch) })
		}
		line := d.buf.Line(pos.Line)
		prefix := strings.ReplaceAll(line[:min(pos.Col, len(line))], "\t", "    ")
		width, _ := c.MeasureText(0, ui.Span{Text: prefix, Font: st.Font.Family, Size: fontSize})
		caretX := min(bounds.X+bounds.W-4, max(bounds.X+gutter, bounds.X+gutter+width))
		g.caretAnchor = ui.Point{X: caretX, Y: min(bounds.Y+bounds.H-4, max(bounds.Y, bounds.Y+float32(st.LineTop(pos.Line)-st.ScrollY())+fontSize*lineHeight))}
		a.syncSignature(g, d, st)
		a.signatureStrip(c, bounds, st, g, d, gutter)
		a.renameStrip(c, bounds, st, d, gutter)
		a.codeActionLightbulb(c, g, d, st, bounds, gutter, fontSize, lineHeight)
		switch {
		case a.focusEditor == st || a.focusGroupTarget == g:
			// A navigation went here: the keys read on from it.
			ed.Focus()
			a.focusEditor = nil
		case g == a.focus && !a.term.open:
			ed.AutoFocus()
		}
		if a.focusGroupTarget == nil && ed.FocusWithin() && a.focus != g {
			a.focus = g
		}
	}
}

func diffHunkOffset(st *editor.State, d *editor.DiffState, index int) float32 {
	if index <= 0 {
		return 0
	}
	fontSize, lineHeight := st.Font.Size, st.LineHeight
	if fontSize <= 0 {
		fontSize = 13
	}
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	context := d.ContextLines
	if context <= 0 {
		context = 3
	}
	var height float32
	shown := 0
	for i, h := range d.Hunks() {
		from := max(shown, h.NewStart-context)
		if from > shown {
			height += 32
		}
		if i == index {
			return max(0, height-float32(context)*fontSize*lineHeight)
		}
		after := min(d.New.LineCount(), h.NewEnd+context)
		height += float32(h.NewStart-from+h.OldEnd-h.OldStart+h.NewEnd-h.NewStart+after-h.NewEnd) * fontSize * lineHeight
		shown = after
	}
	return height
}

// changeBar is a Git change bar of an editor, in the window, and the line
// its change starts at.
type changeBar struct {
	r    ui.Rect
	line int
}

// changeBarInput opens the change of a bar pressed in the editor of d,
// whose top left is origin in the window, taking the press and its
// release from the editor. The bars take nothing else: the wheel over
// them scrolls the editor as anywhere in it.
func (a *App) changeBarInput(g *group, d *doc, st *editor.State, origin ui.Point, ev ui.InputEvent) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		if ev.Button != 0 {
			return false
		}
		for _, bar := range g.changeBars {
			if bar.r.Contains(origin.X+ev.X, origin.Y+ev.Y) {
				g.changeBarPress = true
				a.showGitPeek(g, d, st, bar.line)
				return true
			}
		}
	case ui.InputPointerMove, ui.InputPointerUp:
		if g.changeBarPress {
			g.changeBarPress = ev.Kind == ui.InputPointerMove
			return true
		}
	}
	return false
}

func (a *App) relatedView(c *ui.Context, ch *defChoices) {
	if ch.collapsed == nil {
		ch.collapsed = make(map[string]bool)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(2).FillHeight().Clip().Children(func() {
			if ch.previewEditor == nil {
				message := "Loading code preview…"
				if ch.buffers != nil {
					message = "Code preview unavailable"
				}
				ui.Text(c, message).FontSize(12).TextColor(colors(c).Muted).Padding(12)
			} else {
				editor.View(c.Key("reference-code"), ch.previewEditor).Grow(1).FillWidth()
			}
		})
		ui.Column(c).Grow(1).FillHeight().BorderWidth(0, 0, 0, 1).BorderColor(colors(c).Separator).Children(func() {
			ui.Scroll(c).Grow(1).FillWidth().Label(locales.MsgNavigationResults()).Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					a.relatedList(c, ch, ch.collapsed, "", nil,
						func(i int) { a.selectRelated(ch, i) },
						func(i int) { a.jump(ch.g, ch.from, ch.locs[i], ch.enc) })
				})
			})
		})
	})
}

// peekAction is a second button of a peek's header, beside its close
// button, as the references peek's pin.
type peekAction struct {
	destructive bool
	icon        *ui.SVG
	label       string
	tip         string
	do          func()
}

// editorPeek shows content in a popover at a line of the editor, as
// Xcode's change and reference previews: rounded, lifted off the code by a
// soft shadow, below the line, or above it where there is no room below.
// A title "name — detail" shows the detail muted. It is as tall as
// contentHeight, the height content would take, up to a third of a
// screenful; content scrolls past that.
func (a *App) editorPeek(c *ui.Context, bounds ui.Rect, st *editor.State, line int, title, closeLabel string, contentHeight float32, close, content func()) {
	a.editorPeekAction(c, bounds, st, line, title, closeLabel, contentHeight, false, nil, close, content)
}

// editorPeekAction is editorPeek with a second header action before the
// close button, or none for a nil action.
func (a *App) editorPeekAction(c *ui.Context, bounds ui.Rect, st *editor.State, line int, title, closeLabel string, contentHeight float32, fitContent bool, act *peekAction, close, content func()) {
	a.editorPeekSizedContent(c, bounds, st, line, title, closeLabel, contentHeight, 300, fitContent, act, close, content)
}

func (a *App) editorPeekSized(c *ui.Context, bounds ui.Rect, st *editor.State, line int, title, closeLabel string, contentHeight, maxHeight float32, act *peekAction, close, content func()) {
	a.editorPeekSizedContent(c, bounds, st, line, title, closeLabel, contentHeight, maxHeight, false, act, close, content)
}

func (a *App) editorPeekSizedContent(c *ui.Context, bounds ui.Rect, st *editor.State, line int, title, closeLabel string, contentHeight, maxHeight float32, fitContent bool, act *peekAction, close, content func()) {
	const inset, gap = 12, 4
	fontSize := st.Font.Size
	if fontSize <= 0 {
		fontSize = 13
	}
	lineHeight := st.LineHeight
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	const header = 36
	height := min(header+contentHeight, maxHeight, max(120, bounds.H-2*inset))
	top := bounds.Y + float32(st.LineTop(line)-st.ScrollY())
	y := top + fontSize*lineHeight + gap
	if y+height > bounds.Y+bounds.H-inset && top-gap-height >= bounds.Y+inset {
		y = top - gap - height
	}
	y = min(max(y, bounds.Y+inset), max(bounds.Y+inset, bounds.Y+bounds.H-inset-height))
	pal := colors(c)
	near, far := ui.RGBA(0, 0, 0, 0.08), ui.RGBA(0, 0, 0, 0.16)
	if c.Theme().Dark {
		near, far = ui.RGBA(0, 0, 0, 0.30), ui.RGBA(0, 0, 0, 0.45)
	}
	name, detail, _ := strings.Cut(title, " — ")
	ui.Overlay(c, func() {
		panel := ui.Column(c).Absolute().Left(bounds.X + inset).Width(max(200, bounds.W-2*inset))
		if fitContent {
			panel.MaxHeight(height).Role(ui.RoleGroup).Label(title)
			if y < top {
				_, viewHeight := c.Size()
				panel.Bottom(viewHeight - (top - gap))
			} else {
				panel.Top(y)
			}
		} else {
			panel.Top(y).Height(height)
		}
		panel.
			Radius(12).Clip().Background(pal.Popover).Border(1, pal.PopoverEdge).
			Shadow(0, 1, 3, 0, near).Shadow(0, 12, 32, 0, far).Children(func() {
			ui.Row(c).FillWidth().Height(header).Shrink(0).Padding(0, 8, 0, 14).Gap(6).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.Separator).Children(func() {
				ui.Text(c, name).FontSize(12).FontWeight(600).SingleLine().Shrink(1)
				if detail != "" {
					ui.Text(c, detail).FontSize(12).TextColor(pal.Muted).SingleLine().Shrink(2)
				}
				ui.Box(c).Grow(1)
				if act != nil {
					if act.destructive {
						button := ui.ButtonBase(c).Label(act.label).Padding(3, 6).Radius(4).Cursor(ui.CursorPointer)
						if button.Hovered() {
							button.Background(pal.Pill)
						}
						button.OnClick(act.do).Children(func() { ui.Text(c, act.label).FontSize(11).TextColor(pal.Err.Alpha(0.75)) })
					} else if glyphButton(c, act.icon, act.label, act.tip, false, false).Clicked() {
						act.do()
					}
				}
				if glyphButton(c, icClose, closeLabel, locales.MsgClose(), false, false).Clicked() {
					close()
				}
			})
			body := ui.Column(c).MinHeight(0).FillWidth().Clip()
			if fitContent {
				body.Padding(0, 0, 6, 0)
			} else {
				body.Grow(1)
			}
			body.Children(content)
		})
	})
}

type action struct {
	label string
	fn    func()
}

func (a *App) banner(c *ui.Context, bg ui.Color, msg string, actions []action) {
	ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(bg).Children(func() {
		ui.Text(c, msg).FontSize(12).Grow(1).Shrink(1)
		for _, ac := range actions {
			ui.Button(c, ac.label).OnClick(ac.fn)
		}
	})
}

func (a *App) message(c *ui.Context, title, detail, button string, fn func()) {
	ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(16).Bold()
		ui.Text(c, detail).TextColor(colors(c).Muted).MaxWidth(520).TextAlign(ui.Center)
		if button != "" {
			ui.Button(c, button).OnClick(fn)
		}
	})
}

func (a *App) trustBanner(c *ui.Context) {
	// The decision is the lead; what servers may do reads as the detail
	// under it, or the wall of text buried the buttons the banner exists
	// for.
	ui.Row(c).FillWidth().Padding(8, 12).Gap(12).AlignItems(ui.Center).Background(colors(c).InfoBg).Children(func() {
		ui.Column(c).Grow(1).Shrink(1).Gap(2).Children(func() {
			ui.Text(c, locales.TrustBannerLead(filepath.Base(a.root))).FontSize(12).FontWeight(600).FillWidth()
			ui.Text(c, locales.TrustBannerDetail()).FontSize(11).TextColor(colors(c).Muted).FillWidth()
		})
		ui.Button(c, locales.TrustFolder()).OnClick(a.trust)
		ui.Button(c, locales.NotNow()).OnClick(func() { a.trustAsk, a.afterTrust = false, nil; a.trustDismissed = true })
	})
}
