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
func jumpCrumb(c *ui.Context, key any, label string, open *bool, children func(), build func(m *ui.Menu)) ui.Element {
	b := ui.ButtonBase(c.Key(key)).Label(label).Padding(1, 3).Radius(4).Shrink(0)
	if b.Hovered() || b.Pressed() || open != nil && *open {
		b.Background(colors(c).Pill)
	}
	b.Children(func() {
		ui.Row(c).Gap(4).AlignItems(ui.Center).Children(children)
	})
	if open == nil {
		return b.PopoverMenuButton(build)
	}
	return b.PopoverMenuButton(build, ui.PopoverMenuButtonOptions{Open: open})
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
				ui.Text(x.Context, e.Name).FontSize(13).TextColor(x.Text).SingleLine()
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

// jumpSymbolsMenu lists the symbols inside parent, current checked.
func (a *App) jumpSymbolsMenu(m *ui.Menu, g *group, parent, current int) {
	o := &a.outline
	for _, j := range symbolChildren(o.symbols, parent) {
		s := o.symbols[j]
		it := m.Item(s.Name).ID(fmt.Sprint(j, ":", s.Name)).Checked(j == current).Content(func(x ui.MenuItemContent) {
			ui.Row(x.Context).Gap(6).AlignItems(ui.Center).Children(func() { symbolCrumbContent(x.Context, s, x.Text, 13) })
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

func symbolCrumbContent(c *ui.Context, s lsp.DocumentSymbol, text ui.Color, size float32) {
	kind, color := symbolKind(c, s.Kind)
	ui.Box(c).Width(size + 1).Height(size + 1).Radius(3).Shrink(0).Background(color.Alpha(0.16)).AlignItems(ui.Center).Justify(ui.Center).Children(func() {
		ui.Text(c, kind).FontSize(size - 4).Bold().TextColor(color).SingleLine()
	})
	ui.Text(c, s.Name).FontSize(size).TextColor(text).SingleLine()
}

// jumpSymbols is the jump bar's path from the file down to the symbol
// around the cursor, each segment listing its siblings, or No Selection
// listing the top level when the cursor is outside every symbol.
func (a *App) jumpSymbols(c *ui.Context, g *group) {
	o := &a.outline
	current := a.currentSymbol()
	path := symbolPath(o.symbols, current)
	symbolIcon(c, icChevron, 9, colors(c).Dim)
	if len(path) == 0 {
		jumpCrumb(c, "symbol-none", outlineText("jumpNoSelection"), &g.jumpSymbols, func() {
			ui.Text(c, outlineText("jumpNoSelection")).FontSize(12).TextColor(colors(c).Muted).SingleLine()
		}, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, -1, -1) })
		return
	}
	for k, i := range path {
		if k > 0 {
			symbolIcon(c, icChevron, 9, colors(c).Dim)
		}
		parent := -1
		if k > 0 {
			parent = path[k-1]
		}
		var open *bool
		if k == len(path)-1 {
			open = &g.jumpSymbols
		}
		s := o.symbols[i]
		jumpCrumb(c, fmt.Sprint("symbol-", k, ":", s.Name), s.Name, open, func() {
			symbolCrumbContent(c, s, colors(c).Text, 12)
		}, func(m *ui.Menu) { a.jumpSymbolsMenu(m, g, parent, i) })
	}
}
