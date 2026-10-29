package main

import (
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"

	"see/internal/workspace"
)

// tabDrag is a tab, or a file of the tree, being dragged: onto a tab bar
// it goes among the tabs, onto an editor's edge it splits the editor.
type tabDrag struct {
	from *group // nil for a file of the tree
	path string
}

func (a *app) editorArea(c *ui.Context) {
	ui.Column(c).Fill().Background(colors(c).editor).Children(func() {
		a.editorsAndPanel(c, func() {
			if a.git.diff != nil {
				a.gitDiffView(c)
			} else {
				ui.Column(c).Fill().Children(func() { a.layoutView(c, a.layout) })
			}
		})
	})
}

func (a *app) layoutView(c *ui.Context, n *layout) {
	if n.g != nil {
		a.groupView(c, n.g)
		return
	}
	first, second := func() { a.layoutView(c, n.a) }, func() { a.layoutView(c, n.b) }
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

func (a *app) groupView(c *ui.Context, g *group) {
	focused := g == a.focus && len(a.groups()) > 1
	col := ui.Column(c.Key(g)).Grow(1).Fill()
	if focused {
		accent := c.Theme().Accent
		col.DrawOver(func(p *ui.Painter, r ui.Rect) {
			p.Fill(ui.Rect{X: r.X, Y: r.Y, W: r.W, H: 2}, accent, 0)
		})
	}
	col.Children(func() {
		if len(g.order) > 0 {
			if len(g.order) > 1 {
				a.tabBar(c, g)
			}
			a.jumpBar(c, g)
		}
		if g == a.focus {
			if a.trustAsk {
				a.trustBanner(c)
			}
			if a.choices != nil && a.choices.g == g && a.choices.title == "" {
				a.choicesPanel(c)
			}
		}
		a.dropArea(c, g, func() { a.groupContent(c, g) })
		if st := g.eds[g.active]; st != nil {
			pos := st.CursorPos()
			line := st.Buffer.Line(pos.Line)
			column := utf8.RuneCountInString(line[:min(pos.Col, len(line))]) + 1
			ui.Row(c).FillWidth().Height(24).Padding(0, 10).AlignItems(ui.Center).
				BorderWidth(1, 0, 0, 0).BorderColor(colors(c).separator).Background(colors(c).editor).Children(func() {
				ui.Box(c).Grow(1)
				ui.Text(c, fmt.Sprintf("Line %d  ·  Column %d", pos.Line+1, column)).FontSize(11).TextColor(colors(c).muted).SingleLine()
			})
		}
	})
}

// dropArea takes tabs and files dropped onto an editor: on its edges they
// open in a new editor there, in its middle in this one.
func (a *app) dropArea(c *ui.Context, g *group, content func()) {
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
			hl := ui.Box(c).Absolute().Background(colors(c).dropZone).Radius(6).Border(2, c.Theme().Accent).PassThrough()
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

func (a *app) dropTab(v *tabDrag, g *group, s side, at int) {
	a.cancelNav()
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

func (a *app) groupContent(c *ui.Context, g *group) {
	d := a.docs[g.active]
	if d == nil {
		ui.Column(c).Grow(1).FillWidth().Center().Gap(6).Children(func() {
			ui.Text(c, "No Editor").FontSize(20).TextColor(colors(c).dim)
			ui.Text(c, "Choose a file in the navigator, or press "+keyLabel("Cmd+Shift+", "O")+" to open one quickly.").FontSize(12).TextColor(colors(c).dim)
		})
		return
	}
	a.document(c, g, d)
}

func (a *app) document(c *ui.Context, g *group, d *doc) {
	st := g.eds[d.path]
	switch {
	case d.err != nil:
		a.message(c, "Could not open "+d.name, describeErr(d.err), "Retry", func() { a.reloadDoc(d) })
	case d.loading && st == nil:
		ui.Row(c).Grow(1).FillWidth().Center().Gap(8).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "Loading "+d.name+"…").TextColor(colors(c).muted)
		})
	case d.image != nil:
		a.imagePreview(c, g, d)
	case d.kind == workspace.Binary:
		a.message(c, d.name+" is a binary file", fmt.Sprintf("It is not shown as source (%s).", size(d.size)), "", nil)
	case d.kind == workspace.TooLarge:
		a.message(c, d.name+" is too large to show",
			fmt.Sprintf("It is %s; files up to %s open here.", size(d.size), size(workspace.MaxSize)), "", nil)
	case d.kind == workspace.BadEncoding:
		a.message(c, d.name+" is not UTF-8 text",
			"Its text uses another encoding, or it is not text.", "Show with Replacement Characters", func() {
				d.lossy = true
				a.reloadDoc(d)
			})
	case st != nil:
		if v := d.buf.Version(); v != d.lspVersion {
			a.invalidateHover()
			d.lspVersion = v
			if d.untitled {
				a.cacheDraft(d)
			}
			if m := a.serverFor(d.path); m != nil {
				m.Changed(d.uri, d.buf.Snapshot())
			}
			if d.buf.Modified() && g.preview == d.path {
				g.preview = ""
			}
		}
		if d.conflict {
			a.banner(c, colors(c).warnBg, "This file changed on disk. Your edits are preserved.", []action{{"Overwrite", func() { a.saveDoc(d, true) }}})
		}
		if d.gone {
			a.banner(c, colors(c).warnBg, "This file was deleted or moved on disk. The text below is how it was.", []action{
				{"Reload", func() { a.reloadDoc(d) }},
				{"Close", func() { a.closeTab(g, d.path) }},
			})
		}
		if d.plain || d.lossy {
			msg := "Large file: syntax highlighting is off."
			if d.lossy {
				msg = "Bytes that are not UTF-8 show as �. This file is read-only to protect its encoding."
			}
			ui.Text(c, msg).FontSize(11).TextColor(colors(c).muted).Padding(3, 12).FillWidth().Background(colors(c).tabBar)
		}
		if isMarkdown(d.path) && g.markdownPreview[d.path] {
			a.markdownView(c, g, d)
			return
		}
		ed := editor.View(c.Key(d.path), st).Grow(1).FillWidth()
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
		if st.DiffBase != nil {
			gutter += 6
		}
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
						PassThrough().Label(fmt.Sprintf("Show Git change at line %d", line+1))
					if hovered {
						b.Background(ui.RGBA(128, 128, 128, 0.25))
					}
					b.OnClick(func() { a.showGitPeek(g, d, st, line) })
				})
			}
		}
		origin := ui.Point{X: bounds.X, Y: bounds.Y}
		st.OnInput = func(ev ui.InputEvent) bool { return a.changeBarInput(g, d, st, origin, ev) }
		if peek := a.git.peek; peek != nil && peek.doc == d && peek.group == g {
			a.editorPeek(c, bounds, st, peek.line, d.name+" — Git changes", "Close Change Preview", diffHeight(st, peek.diff), func() { a.git.peek = nil }, func() {
				editor.DiffView(c, peek.diff).Grow(1).FillWidth()
			})
		}
		if ch := a.choices; ch != nil && ch.title != "" && ch.g == g && ch.from.Path == d.path {
			a.editorPeek(c, bounds, st, ch.from.Line, fmt.Sprintf("%d %s", len(ch.locs), ch.title), "Close References Preview", 264, func() { a.choices = nil }, func() { a.relatedView(c, ch) })
		}
		line := d.buf.Line(pos.Line)
		prefix := strings.ReplaceAll(line[:min(pos.Col, len(line))], "\t", "    ")
		width, _ := c.MeasureText(0, ui.Span{Text: prefix, Font: st.Font.Family, Size: fontSize})
		g.caretAnchor = ui.Point{X: min(bounds.X+bounds.W-4, bounds.X+gutter+width), Y: min(bounds.Y+bounds.H-4, max(bounds.Y, bounds.Y+float32(st.LineTop(pos.Line)-st.ScrollY())+fontSize*lineHeight))}
		switch {
		case a.focusEditor == st:
			// A navigation went here: the keys read on from it.
			ed.Focus()
			a.focusEditor = nil
		case g == a.focus && !a.term.open:
			ed.AutoFocus()
		}
		if ed.FocusWithin() && a.focus != g {
			a.focus = g // the editor clicked in is the one keys go to
		}
	}
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
func (a *app) changeBarInput(g *group, d *doc, st *editor.State, origin ui.Point, ev ui.InputEvent) bool {
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

// diffHeight is the height of a diff view of d in the editor's font:
// each hunk's lines and the context around them, as far as the text
// goes, and a bar for each run of lines folded away.
func diffHeight(st *editor.State, d *editor.DiffState) float32 {
	fontSize, lineHeight := st.Font.Size, st.LineHeight
	if fontSize <= 0 {
		fontSize = 13
	}
	if lineHeight <= 0 {
		lineHeight = 1.5
	}
	context := max(d.ContextLines, 0)
	if d.ContextLines == 0 {
		context = 3
	}
	n := d.New.LineCount()
	rows, shown := 0, 0 // shown: the new text's lines up to here shown or folded
	for _, h := range d.Hunks() {
		from := max(shown, h.NewStart-context)
		if from > shown {
			rows++ // the bar of the lines folded before
		}
		after := min(n, h.NewEnd+context)
		rows += h.NewStart - from + h.OldEnd - h.OldStart + h.NewEnd - h.NewStart + after - h.NewEnd
		shown = after
	}
	if shown < n {
		rows++
	}
	return float32(rows)*fontSize*lineHeight + 12
}

func (a *app) relatedView(c *ui.Context, ch *defChoices) {
	if ch.collapsed == nil {
		ch.collapsed = make(map[string]bool)
	}
	paths := make([]string, 0)
	groups := make(map[string][]int)
	for i, t := range ch.locs {
		if _, ok := groups[t.path]; !ok {
			paths = append(paths, t.path)
		}
		groups[t.path] = append(groups[t.path], i)
	}
	ui.Row(c).Fill().AlignItems(ui.Stretch).Children(func() {
		ui.Column(c).Grow(2).FillHeight().Clip().Children(func() {
			if ch.previewEditor == nil {
				ui.Text(c, "Loading code preview…").FontSize(12).TextColor(colors(c).muted).Padding(12)
			} else {
				editor.View(c.Key("reference-code"), ch.previewEditor).Grow(1).FillWidth()
			}
		})
		ui.Column(c).Grow(1).FillHeight().BorderWidth(0, 0, 0, 1).BorderColor(colors(c).separator).Children(func() {
			ui.Scroll(c).Grow(1).FillWidth().Label("Reference results").Children(func() {
				ui.Column(c).FillWidth().Children(func() {
					for _, path := range paths {
						indices := groups[path]
						file := ui.ButtonBase(c.Key(path)).FillWidth().Padding(5, 8).Gap(6).Label("Toggle references in " + relPath(a.root, path))
						if file.Hovered() {
							file.Background(colors(c).pill)
						}
						file.Children(func() {
							arrow := "▾"
							if ch.collapsed[path] {
								arrow = "▸"
							}
							ui.Text(c, arrow).TextColor(colors(c).muted)
							ui.Text(c, relPath(a.root, path)).FontSize(12).SingleLine().Grow(1).Shrink(1)
							ui.Text(c, fmt.Sprint(len(indices))).FontSize(11).TextColor(colors(c).muted)
						})
						file.OnClick(func() { ch.collapsed[path] = !ch.collapsed[path] })
						if ch.collapsed[path] {
							continue
						}
						for _, i := range indices {
							t := ch.locs[i]
							label := fmt.Sprintf("%s:%d", relPath(a.root, t.path), t.pos.Line+1)
							text := t.name
							if n := t.pos.Line - t.previewStart; n >= 0 && n < len(t.preview) {
								text = strings.TrimSpace(t.preview[n])
							}
							if text == "" {
								text = label
							}
							row := ui.ButtonBase(c.Key(i)).FillWidth().Padding(4, 8, 4, 25).Radius(4).Label(label).Tooltip("Select to preview; double-click to open")
							if i == ch.selected {
								row.Background(colors(c).selectedRow)
							} else if row.Hovered() {
								row.Background(colors(c).pill)
							}
							row.Children(func() { ui.Text(c, text).FontSize(12).SingleLine().Shrink(1) })
							if row.DoubleClicked() {
								a.jump(ch.g, ch.from, t, ch.enc)
							} else {
								row.OnClick(func() { a.selectRelated(ch, i) })
							}
						}
					}
				})
			})
		})
	})
}

// editorPeek shows content in a popover at a line of the editor, as
// Xcode's change and reference previews: rounded, lifted off the code by a
// soft shadow, below the line, or above it where there is no room below.
// A title "name — detail" shows the detail muted. It is as tall as
// contentHeight, the height content would take, up to a third of a
// screenful; content scrolls past that.
func (a *app) editorPeek(c *ui.Context, bounds ui.Rect, st *editor.State, line int, title, closeLabel string, contentHeight float32, close, content func()) {
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
	height := min(header+contentHeight, 300, max(120, bounds.H-2*inset))
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
		ui.Column(c).Absolute().Left(bounds.X+inset).Top(y).Width(max(200, bounds.W-2*inset)).Height(height).
			Radius(12).Clip().Background(pal.popover).Border(1, pal.popoverEdge).
			Shadow(0, 1, 3, 0, near).Shadow(0, 12, 32, 0, far).Children(func() {
			ui.Row(c).FillWidth().Height(header).Shrink(0).Padding(0, 8, 0, 14).Gap(6).AlignItems(ui.Center).
				BorderWidth(0, 0, 1, 0).BorderColor(pal.separator).Children(func() {
				ui.Text(c, name).FontSize(12).FontWeight(600).SingleLine().Shrink(1)
				if detail != "" {
					ui.Text(c, detail).FontSize(12).TextColor(pal.muted).SingleLine().Shrink(2)
				}
				ui.Box(c).Grow(1)
				if glyphButton(c, icClose, closeLabel, "Close", false, false).Clicked() {
					close()
				}
			})
			ui.Column(c).Grow(1).FillWidth().Clip().Children(content)
		})
	})
}

type action struct {
	label string
	fn    func()
}

func (a *app) banner(c *ui.Context, bg ui.Color, msg string, actions []action) {
	ui.Row(c).FillWidth().Padding(6, 12).Gap(8).AlignItems(ui.Center).Background(bg).Children(func() {
		ui.Text(c, msg).FontSize(12).Grow(1).Shrink(1)
		for _, ac := range actions {
			ui.Button(c, ac.label).OnClick(ac.fn)
		}
	})
}

func (a *app) message(c *ui.Context, title, detail, button string, fn func()) {
	ui.Column(c).Grow(1).FillWidth().Center().Gap(8).Padding(24).Children(func() {
		ui.Text(c, title).FontSize(16).Bold()
		ui.Text(c, detail).TextColor(colors(c).muted).MaxWidth(520).TextAlign(ui.Center)
		if button != "" {
			ui.Button(c, button).OnClick(fn)
		}
	})
}

func (a *app) trustBanner(c *ui.Context) {
	a.banner(c, colors(c).infoBg,
		"Go to Definition and symbol help run language servers in “"+filepath.Base(a.root)+"”. Servers may run project tools and download dependencies or toolchains. Reading works without them.",
		[]action{{"Trust Folder", a.trust}, {"Not Now", func() { a.trustAsk, a.afterTrust = false, nil; a.trustDismissed = true }}})
}

func (a *app) choicesPanel(c *ui.Context) {
	ch := a.choices
	ui.Column(c).FillWidth().Padding(6, 12).Gap(2).Background(colors(c).tabBar).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).separator).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Children(func() {
			title := ch.title
			if title == "" {
				title = "definitions"
			}
			if len(ch.locs) == 1 {
				title = strings.TrimSuffix(title, "s")
			}
			ui.Text(c, fmt.Sprintf("%d %s", len(ch.locs), title)).Bold().Grow(1)
			ui.Button(c, "Cancel").OnClick(func() { a.choices = nil })
		})
		ui.Scroll(c).MaxHeight(180).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i, t := range ch.locs {
					label := fmt.Sprintf("%s:%d", relPath(a.root, t.path), t.pos.Line+1)
					b := ui.Button(c.Key(i), label).FillWidth()
					if i == 0 {
						b.AutoFocus()
					}
					b.OnClick(func() { a.jump(ch.g, ch.from, t, ch.enc) })
				}
			})
		})
	})
}
