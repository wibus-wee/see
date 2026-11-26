package app

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"
	"see/internal/lsp"
	"see/locales"
)

// symbolPath is the symbols around symbol i, outermost first and i last,
// as indexes of the flattened list.
func symbolPath(symbols []lsp.DocumentSymbol, i int) []int {
	if i < 0 || i >= len(symbols) {
		return nil
	}
	path := []int{i}
	for j := i - 1; j >= 0 && symbols[path[0]].Depth > 0; j-- {
		if symbols[j].Depth < symbols[path[0]].Depth {
			path = append([]int{j}, path...)
		}
	}
	return path
}

// symbolChildren is the symbols directly inside symbol parent, or the top
// level ones for -1.
func symbolChildren(symbols []lsp.DocumentSymbol, parent int) []int {
	var out []int
	depth := 0
	if parent >= 0 {
		depth = symbols[parent].Depth + 1
	}
	for j := parent + 1; j < len(symbols) && symbols[j].Depth >= depth; j++ {
		if symbols[j].Depth == depth {
			out = append(out, j)
		}
	}
	return out
}

// jumpSymbolsShown reports whether the outline holds the symbols of g's
// document, as the jump bar shows them only for the focused editor.
func (a *App) jumpSymbolsShown(g *group, p string) bool {
	o := &a.outline
	return g == a.focus && g.eds[g.active] != nil && !isMarkdownTab(g.active) && o.shown.path == p && o.shown.doc == a.docs[p] && len(o.symbols) > 0
}

// jumpCrumb is a segment of the jump bar: a button whose menu lists the
// items beside it, as Xcode's.
func jumpCrumb(c *ui.Context, key any, label string, open *bool, shrink, grow float32, children func(), build func(m *ui.Menu)) ui.Element {
	return jumpCrumbWithFill(c, key, label, open, shrink, grow, grow > 0, children, build)
}

func jumpCrumbWithFill(c *ui.Context, key any, label string, open *bool, shrink, grow float32, fillContent bool, children func(), build func(m *ui.Menu)) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Label(label).Padding(1, 3).Radius(4).Shrink(shrink).MinWidth(0)
	if grow > 0 {
		b.Grow(grow).Justify(ui.Start)
	}
	if b.Hovered() || b.Pressed() || open != nil && *open {
		b.Background(colors(c).Pill)
	}
	b.Children(func() {
		row := ui.Row(c).Gap(4).AlignItems(ui.Center).MinWidth(0)
		if fillContent {
			row.FillWidth()
		}
		row.Children(children)
	})
	if open == nil {
		return b.PopoverMenuButton(build, ui.PopoverMenuButtonOptions{Panel: popoverMenuPanel})
	}
	return b.PopoverMenuButton(build, ui.PopoverMenuButtonOptions{Open: open, Panel: popoverMenuPanel})
}

// jumpFilesMenu lists the entries of dir, current checked and folders as
// submenus of their own entries one level down.
func (a *App) jumpFilesMenu(m *ui.Menu, g *group, dir, current string, nested bool) {
	d := a.dirs[dir]
	if d == nil || (!d.loaded && !d.loading && d.err == nil) {
		a.loadDir(dir)
		d = a.dirs[dir]
	}
	if !d.loaded {
		m.Item(locales.MsgLoading()).Disabled(true)
		return
	}
	for _, p := range a.entryPaths(d) {
		e, _ := a.entry(p)
		content := func(x ui.MenuItemContent) {
			ui.Row(x.Context).Gap(6).AlignItems(ui.Center).Children(func() {
				if e.Dir {
					folderGlyph(x.Context, false, 13)
				} else {
					fileIcon(x.Context, e.Name, 13)
				}
				ui.Text(x.Context, e.Name).FontSize(13).TextColor(x.Text).SingleLine().Ellipsis("…")
			})
		}
		if e.Dir && !nested {
			// MyGo draws no check mark on a submenu, so the folder holding
			// the file shows it in its submenu instead.
			m.Submenu(e.Name, func(m *ui.Menu) { a.jumpFilesMenu(m, g, p, current, true) }).ID(p).Content(content)
			continue
		}
		it := m.Item(e.Name).ID(p).Checked(p == current || strings.HasPrefix(current, p+string(filepath.Separator))).Content(content)
		if !it.Chosen() {
			continue
		}
		g.jumpFilesDirect = false
		if e.Dir {
			a.sidebarOff, a.navigator = false, navFiles
			a.revealInTree(filepath.Join(p, "x"))
			a.reveal = p
		} else if p != current {
			a.jumpToFile(g, p)
		}
	}
}

func (a *App) jumpToFile(g *group, p string) {
	a.focus = g
	if here, ok := a.here(); ok {
		a.hist.Push(here)
	}
	a.cancelNav()
	a.openIn(g, p, goTo{})
	if st := a.activeEditor(); st != nil {
		a.focusEditor = st
	}
}

func jumpBarOverflowButton(c *ui.Context, open *bool, build func(m *ui.Menu)) {
	b := ui.ButtonBase(c.Key("jumpbar-overflow")).Label("Jump Bar overflow").Size(26, 20).Radius(10).Center().Background(colors(c).Pill)
	b.Children(func() {
		ui.Text(c, "…").FontSize(13).Bold().TextColor(colors(c).Muted).SingleLine()
	})
	options := ui.PopoverMenuButtonOptions{Panel: popoverMenuPanel}
	if open != nil {
		options.Open = open
	}
	b.PopoverMenuButton(build, options)
}

func (a *App) jumpBarOverflowMenu(m *ui.Menu, g *group, p string, parts []string, filesMenu, showSymbols bool) {
	if filesMenu {
		for i, part := range parts {
			item := filepath.Join(append([]string{a.root}, parts[:i+1]...)...)
			m.Submenu(part, func(m *ui.Menu) { a.jumpFilesMenu(m, g, filepath.Dir(item), p, false) }).ID(item)
		}
	}
	if !showSymbols {
		return
	}
	path := symbolPath(a.outline.symbols, a.currentSymbol())
	if len(path) < 2 {
		return
	}
	if filesMenu && len(parts) > 0 {
		m.Separator()
	}
	for k, i := range path[:len(path)-1] {
		parent := -1
		if k > 0 {
			parent = path[k-1]
		}
		s := a.outline.symbols[i]
		m.Submenu(s.Name, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, parent, i) }).ID(fmt.Sprintf("symbol-%d:%s", i, s.Name))
	}
}

// jumpSymbolsMenu lists the symbols inside parent, current checked.
func (a *App) jumpSymbolsMenu(m *ui.Menu, g *group, parent, current int) {
	o := &a.outline
	for _, j := range symbolChildren(o.symbols, parent) {
		s := o.symbols[j]
		it := m.Item(s.Name).ID(fmt.Sprint(j, ":", s.Name)).Checked(j == current).Content(func(x ui.MenuItemContent) {
			ui.Row(x.Context).Gap(6).AlignItems(ui.Center).Children(func() { symbolCrumbContent(x.Context, s, x.Text, 13, false) })
		})
		if it.Chosen() {
			a.focus = g
			a.openOutlineSymbol(j)
			if st := a.activeEditor(); st != nil {
				a.focusEditor = st
			}
		}
	}
}

func symbolCrumbContent(c *ui.Context, s lsp.DocumentSymbol, text ui.Color, size float32, grow bool) {
	kind, color := symbolKind(c, s.Kind)
	ui.Box(c).Width(size + 1).Height(size + 1).Radius(3).Shrink(0).Background(color.Alpha(0.16)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		// The kind letter's line box leaves room under it for a descent
		// that letters don't have; a small bottom margin puts the glyph
		// back in the square's middle.
		ui.Text(c, kind).FontSize(size-4).Bold().TextColor(color).SingleLine().Margin(0, 0, 2, 0)
	})
	name := ui.Text(c, s.Name).FontSize(size).TextColor(text).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0)
	if grow {
		name.Grow(1)
	}
}

// jumpSymbolAncestors lists the symbol path before the current symbol.
func (a *App) jumpSymbolAncestors(c *ui.Context, g *group) {
	o := &a.outline
	path := symbolPath(o.symbols, a.currentSymbol())
	if len(path) < 2 {
		return
	}
	symbolIcon(c, icChevron, 9, colors(c).Dim)
	for k, i := range path[:len(path)-1] {
		if k > 0 {
			symbolIcon(c, icChevron, 9, colors(c).Dim)
		}
		parent := -1
		if k > 0 {
			parent = path[k-1]
		}
		s := o.symbols[i]
		jumpCrumb(c, fmt.Sprint("symbol-", k, ":", s.Name), s.Name, nil, 0, 0, func() {
			symbolCrumbContent(c, s, colors(c).Text, 12, false)
		}, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, parent, i) })
	}
}

// jumpCurrentSymbol keeps the current symbol at the end of the path, after
// the centered overflow pill, so it stays visible as the path gets narrow.
func (a *App) jumpCurrentSymbol(c *ui.Context, g *group, width float32, capped bool) {
	o := &a.outline
	current := a.currentSymbol()
	path := symbolPath(o.symbols, current)
	currentCrumb := ui.Row(c).Gap(2).Shrink(0).MinWidth(0)
	if capped {
		currentCrumb.MaxWidth(width)
	}
	currentCrumb.Children(func() {
		symbolIcon(c, icChevron, 9, colors(c).Dim)
		if len(path) == 0 {
			jumpCrumbWithFill(c, "symbol-none", outlineText("jumpNoSelection"), &g.jumpSymbols, 1, 0, true, func() {
				ui.Text(c, outlineText("jumpNoSelection")).FontSize(12).TextColor(colors(c).Muted).SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Grow(1)
			}, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, -1, -1) })
			return
		}
		i := path[len(path)-1]
		parent := -1
		if len(path) > 1 {
			parent = path[len(path)-2]
		}
		s := o.symbols[i]
		jumpCrumbWithFill(c, fmt.Sprint("symbol-", len(path)-1, ":", s.Name), s.Name, &g.jumpSymbols, 1, 0, true, func() {
			symbolCrumbContent(c, s, colors(c).Text, 12, true)
		}, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, parent, i) })
	})
}
