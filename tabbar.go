package main

import (
	"path/filepath"
	"slices"
	"strings"

	"github.com/egoist/mygo/ui"

	"see/internal/workspace"
)

func (a *app) tabBar(c *ui.Context, g *group) {
	// Names shown by more than one tab get their folder too.
	count := map[string]int{}
	seen := map[string]bool{}
	for _, p := range g.order {
		path := tabPath(p)
		if !seen[path] {
			count[filepath.Base(path)]++
			seen[path] = true
		}
	}
	g.tabSel = slices.Index(g.order, g.active)
	tabs := ui.TabsBase(c, &g.tabSel, len(g.order))
	tabs.List.Background(colors(c).tabBar).FillWidth().Height(30).AlignItems(ui.Stretch).BorderWidth(0, 0, 1, 0).BorderColor(colors(c).hairline).Label("Open files")
	if v, ok := ui.Drop[*tabDrag](tabs.List); ok {
		a.dropTab(v, g, sideNone, len(g.order))
	}
	tabs.List.Children(func() {
		for i, p := range g.order {
			path := tabPath(p)
			d := a.docs[path]
			diff := g.diffs[p]
			if diff != nil {
				path = filepath.Join(a.git.root, diff.name)
				d = &doc{name: filepath.Base(diff.name)}
			}
			if d == nil {
				continue
			}
			active := p == g.active
			tab := tabs.Tab(i).Grow(1).Basis(0).MaxWidth(260).MinWidth(90).Padding(0, 8).Gap(6).
				AlignItems(ui.Center).Justify(ui.Center).BorderWidth(0, 1, 0, 0).BorderColor(colors(c).hairline)
			if tab.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
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
				tab.Background(colors(c).tabActive)
			case hovered:
				tab.Background(colors(c).tabHover)
			}
			if d.untitled {
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
				if diff != nil {
					if diff.staged {
						nameLabel += " (Staged)"
					} else {
						nameLabel += " (Working Tree)"
					}
				}
				cl := ui.ButtonBase(c).Label("Close "+nameLabel).Size(16, 16).Radius(4).Center().KeepFocus()
				if hovered || active {
					cl.Children(func() { symbolIcon(c, icClose, 10, colors(c).muted) })
					if cl.Hovered() {
						cl.Background(colors(c).pill)
					}
				}
				cl.OnClick(func() { a.closeTab(g, p) })
				fileIcon(c, d.name, 13)
				if d.buf != nil && d.buf.Modified() {
					ui.Text(c, "●").FontSize(10).TextColor(c.Theme().Accent)
				}
				name := ui.Text(c, nameLabel).FontSize(12).SingleLine().Shrink(1)
				if g.preview == p {
					name.Italic()
				}
				switch {
				case d.gone:
					name.Strikethrough().TextColor(colors(c).muted)
				case !active:
					name.TextColor(colors(c).muted)
				}
				if count[d.name] > 1 {
					ui.Text(c, filepath.Base(filepath.Dir(path))).FontSize(11).TextColor(colors(c).dim).SingleLine().Shrink(2)
				}
				if d.loading {
					ui.Spinner(c).FontSize(10)
				}
				ui.Box(c).Size(16, 16).Shrink(0) // balances the close button
			})
		}
	})
	if tabs.List.Changed() && g.tabSel >= 0 && g.tabSel < len(g.order) {
		a.userActivateIn(g, g.order[g.tabSel])
	}
}

func (a *app) diffJumpBar(c *ui.Context, g *group) {
	v := g.diffs[g.active]
	if v == nil {
		return
	}
	ui.Row(c).FillWidth().Height(26).Padding(0, 6).Gap(5).AlignItems(ui.Center).Background(colors(c).editor).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).separator).Children(func() {
		folderGlyph(c, false, 13)
		ui.Text(c, filepath.Base(a.root)).FontSize(12).TextColor(colors(c).muted).SingleLine()
		symbolIcon(c, icChevron, 9, colors(c).dim)
		fileIcon(c, filepath.Base(v.name), 12)
		ui.Text(c, v.name).FontSize(12).SingleLine().Shrink(1)
		label := "Working Tree"
		if v.staged {
			label = "Staged"
		}
		ui.Text(c, label).FontSize(12).TextColor(colors(c).muted).SingleLine()
		ui.Box(c).Grow(1)
		if len(g.order) == 1 && len(a.groups()) == 1 {
			if glyphButton(c, icClose, "Close "+filepath.Base(v.name)+" ("+label+")", "Close Diff", false, false).Clicked() {
				a.closeTab(g, g.active)
			}
		}
	})
}

// jumpBar is Xcode's jump bar: back and forward, then the path of the
// document, whose folders reveal themselves in the navigator.
func (a *app) jumpBar(c *ui.Context, g *group) {
	tab := g.active
	p := tabPath(tab)
	if p == "" {
		return
	}
	ui.Row(c).FillWidth().Height(26).Padding(0, 6).Gap(2).AlignItems(ui.Center).Background(colors(c).editor).
		BorderWidth(0, 0, 1, 0).BorderColor(colors(c).separator).Children(func() {
		g.relatedBounds = g.relatedHandle.Bounds(c)
		related := ui.ButtonBase(c.Key("related-items")).Label("Related Items").Tooltip("Related Items").Size(22, 22).Radius(4).Center().Bind(&g.relatedHandle)
		if related.Hovered() || related.Pressed() {
			related.Background(colors(c).pill)
		}
		related.Children(func() { symbolIcon(c, icRelated, 14, colors(c).muted) })
		related.Menu(func(menu *ui.Menu) {
			if menu.Item("Callers").Chosen() {
				a.focus = g
				a.findUsagesAt(true, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
			if menu.Item("References").Chosen() {
				a.focus = g
				a.findUsagesAt(false, ui.Point{X: g.relatedBounds.X, Y: g.relatedBounds.Y + g.relatedBounds.H}, true)
			}
		})
		outside := !workspace.Within(a.root, p)
		if d := a.docs[p]; d != nil && d.untitled {
			outside = false
		}
		folderGlyph(c, false, 13)
		if outside {
			ui.Text(c, tildePath(filepath.Dir(p))).FontSize(12).TextColor(colors(c).muted).SingleLine().Shrink(1)
		} else {
			ui.Text(c, filepath.Base(a.root)).FontSize(12).TextColor(colors(c).muted).SingleLine()
		}
		symbolIcon(c, icChevron, 9, colors(c).dim)
		parts := strings.Split(workspace.RelPath(a.root, p), "/")
		if d := a.docs[p]; d != nil && d.untitled {
			parts = []string{d.name}
		} else if outside {
			parts = []string{filepath.Base(p)}
		}
		for i, part := range parts {
			if i > 0 {
				symbolIcon(c, icChevron, 9, colors(c).dim)
			}
			last := i == len(parts)-1
			crumb := ui.Row(c.Key(i)).Gap(4).AlignItems(ui.Center).Padding(1, 3).Radius(4)
			crumb.Children(func() {
				if last {
					fileIcon(c, part, 12)
				} else {
					folderGlyph(c, false, 12)
				}
				t := ui.Text(c, part).FontSize(12).SingleLine()
				if !last {
					t.TextColor(colors(c).muted)
				} else {
					if g.preview == p {
						t.Italic()
					}
					if d := a.docs[p]; d != nil && d.buf != nil && d.buf.Modified() {
						ui.Text(c, "●").FontSize(9).TextColor(c.Theme().Accent)
					}
				}
			})
			if last && crumb.DoubleClicked() && g.preview == p {
				g.preview = ""
			}
			if !last {
				dir := filepath.Join(append([]string{a.root}, parts[:i+1]...)...)
				if crumb.Hovered() {
					crumb.Background(colors(c).pill)
				}
				crumb.OnClick(func() {
					a.sidebarOff, a.navigator = false, navFiles
					a.revealInTree(filepath.Join(dir, "x"))
					a.reveal = dir
				})
			}
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
				name += " Preview"
			}
			if glyphButton(c, icClose, "Close "+name, "Close File", false, false).Clicked() {
				a.closeTab(g, tab)
			}
		}
		if !isMarkdownTab(tab) && a.canViewBytes(p) {
			label := "Show File Bytes"
			if g.hexPreview[p] {
				label = "Show File"
			}
			if iconButton(c, icHex, label, label, g.hexPreview[p]).Clicked() {
				if g.hexPreview == nil {
					g.hexPreview = map[string]bool{}
				}
				g.hexPreview[p] = !g.hexPreview[p]
			}
		}
		if isMarkdown(p) {
			label := "Open Markdown Preview"
			if g.markdownPreview[p] || isMarkdownTab(tab) {
				label = "Show Markdown Source"
			}
			if iconButton(c, icMarkdown, label, label+" ("+keyLabel("Cmd+Shift+", "V")+")", g.markdownPreview[p] || isMarkdownTab(tab)).Clicked() {
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
